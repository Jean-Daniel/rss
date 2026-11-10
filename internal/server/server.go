// Package server is the HTTP layer: the JSON API and the embedded front end.
package server

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"fever/internal/apitypes"
	"fever/internal/auth"
	"fever/internal/config"
	"fever/internal/store"
)

// Refresher is the background feed refresher (feeds.Refresher).
type Refresher interface {
	RefreshAll(ctx context.Context) error
	RefreshFeed(ctx context.Context, id int64) (bool, error)
	Busy() bool
}

type Server struct {
	cfg       config.Config
	pool      *pgxpool.Pool
	q         *store.Queries
	sessions  *auth.Sessions
	refresher Refresher
	log       *slog.Logger
	static    http.Handler
	changing  stateChanges
}

// New builds the server. webFiles holds the built front end; it is nil in
// development builds, where the Vite dev server serves the front end.
func New(cfg config.Config, pool *pgxpool.Pool, log *slog.Logger, refresher Refresher, webFiles fs.FS) *Server {
	q := store.New(pool)
	return &Server{
		cfg:       cfg,
		pool:      pool,
		q:         q,
		sessions:  auth.NewSessions(q, cfg.SecureCookies()),
		refresher: refresher,
		log:       log,
		static:    newStaticHandler(webFiles),
	}
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.healthz)

	mux.HandleFunc("GET /api/session", s.getSession)
	mux.HandleFunc("POST /api/session", s.login)
	mux.HandleFunc("DELETE /api/session", s.logout)
	mux.Handle("GET /api/subscriptions", s.requireAuth(s.getSubscriptions))
	mux.Handle("GET /api/favicons/{id}", s.requireAuth(s.getFavicon))
	mux.Handle("POST /api/refresh", s.requireAuth(s.refresh))
	mux.Handle("GET /api/items", s.requireAuth(s.listItems))
	mux.Handle("POST /api/items/state", s.requireAuth(s.setItemState))
	mux.Handle("POST /api/items/read-all", s.requireAuth(s.markAllRead))
	mux.Handle("GET /api/items/changes", s.requireAuth(s.itemChanges))
	mux.HandleFunc("/api/", func(w http.ResponseWriter, r *http.Request) {
		writeError(w, http.StatusNotFound, "Unknown API endpoint.")
	})

	mux.Handle("/", s.static)
	return s.securityHeaders(s.checkOrigin(mux))
}

// SweepSessions deletes expired sessions and forgotten devices every hour
// until ctx ends.
func (s *Server) SweepSessions(ctx context.Context) {
	t := time.NewTicker(time.Hour)
	defer t.Stop()
	for {
		if sessions, devices, err := s.sessions.SweepExpired(ctx); err != nil && ctx.Err() == nil {
			s.log.Warn("delete expired sessions", "err", err)
		} else if sessions > 0 || devices > 0 {
			s.log.Info("deleted expired sessions", "sessions", sessions, "devices", devices)
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if err := s.pool.Ping(ctx); err != nil {
		http.Error(w, "database unreachable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte("ok\n"))
}

// requireAuth rejects API calls without a valid session.
func (s *Server) requireAuth(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ok, err := s.sessions.Check(r.Context(), w, r)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		if !ok {
			writeError(w, http.StatusUnauthorized, "Not logged in.")
			return
		}
		next(w, r)
	})
}

// checkOrigin blocks cross-site requests that change state (CSRF). The
// session cookie is also SameSite=Lax, so this is a second line of defense.
func (s *Server) checkOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			got, ok := config.NormalizeOrigin(origin)
			if !ok || got != s.expectedOrigin(r) {
				writeError(w, http.StatusForbidden, "Cross-site request refused.")
				return
			}
		} else if site := r.Header.Get("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			writeError(w, http.StatusForbidden, "Cross-site request refused.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) expectedOrigin(r *http.Request) string {
	if s.cfg.PublicOrigin != "" {
		return s.cfg.PublicOrigin
	}
	scheme := "http"
	if r.TLS != nil || (s.cfg.TrustProxy && r.Header.Get("X-Forwarded-Proto") == "https") {
		scheme = "https"
	}
	host := r.Host
	if s.cfg.TrustProxy {
		if fh := r.Header.Get("X-Forwarded-Host"); fh != "" {
			host = fh
		}
	}
	origin, _ := config.NormalizeOrigin(scheme + "://" + host)
	return origin
}

// clientIP returns the caller's address. Behind nginx it is the last entry of
// X-Forwarded-For, the one nginx itself appended.
func (s *Server) clientIP(r *http.Request) string {
	if s.cfg.TrustProxy {
		if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
			parts := strings.Split(xff, ",")
			if ip := strings.TrimSpace(parts[len(parts)-1]); ip != "" {
				return ip
			}
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Referrer-Policy", "no-referrer")
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) serverError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, context.Canceled) {
		return // the client went away
	}
	s.log.Error("request failed", "method", r.Method, "path", r.URL.Path, "err", err)
	writeError(w, http.StatusInternalServerError, "Something went wrong on the server.")
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(v)
}

func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, apitypes.ErrorResponse{Error: msg})
}

// decodeJSON reads a small JSON request body.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if mt, _, _ := mime.ParseMediaType(r.Header.Get("Content-Type")); mt != "application/json" {
		writeError(w, http.StatusUnsupportedMediaType, "Expected a JSON body.")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "Invalid JSON body.")
		return false
	}
	return true
}
