package auth

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"net/http"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"

	"fever/internal/store"
)

const (
	CookieName = "fever_session"
	// DeviceCookieName marks a browser that has logged in before; see the
	// devices table. It is only sent to the login endpoint.
	DeviceCookieName = "fever_device"
	deviceCookiePath = "/api/session"
	// DeviceTTL is how long a browser stays known without logging in again
	// (the longest lifetime browsers accept for a cookie).
	DeviceTTL = 400 * 24 * time.Hour
	// SessionTTL is how long a session lasts without being used. Each use
	// extends it, so a phone that opens the app regularly stays logged in.
	SessionTTL = 90 * 24 * time.Hour
	// touchEvery limits how often a session's expiry is pushed back, to avoid
	// a database write on every request.
	touchEvery = 24 * time.Hour
)

var (
	ErrNoPassword      = errors.New("no password has been set")
	ErrBadPassword     = errors.New("wrong password")
	ErrTooManyAttempts = errors.New("too many failed attempts")
)

type Sessions struct {
	q       *store.Queries
	secure  bool
	limiter *Limiter
}

func NewSessions(q *store.Queries, secureCookies bool) *Sessions {
	return &Sessions{q: q, secure: secureCookies, limiter: NewLimiter()}
}

// Login checks the password and, on success, starts a session cookie.
func (s *Sessions) Login(ctx context.Context, w http.ResponseWriter, r *http.Request, password, clientIP string) error {
	hash, err := s.q.GetPasswordHash(ctx)
	if err != nil {
		return err
	}
	if hash == nil || *hash == "" {
		return ErrNoPassword
	}

	device := ""
	if c, err := r.Cookie(DeviceCookieName); err == nil {
		device = c.Value
	}
	known := false
	if device != "" {
		if known, err = s.q.DeviceExists(ctx, hashToken(device)); err != nil {
			return err
		}
	}

	attempt, allowed := s.limiter.Begin(clientIP, known)
	if !allowed {
		return ErrTooManyAttempts
	}
	defer attempt.Release()

	ok, err := VerifyPassword(password, *hash)
	if err != nil {
		return err
	}
	if !ok {
		attempt.Failed()
		return ErrBadPassword
	}
	attempt.Succeeded()

	token, tokenHash, err := newToken()
	if err != nil {
		return err
	}
	expires := time.Now().Add(SessionTTL)
	err = s.q.CreateSession(ctx, store.CreateSessionParams{
		TokenHash: tokenHash,
		ExpiresAt: expires,
		UserAgent: truncateUTF8(r.UserAgent(), 255),
	})
	if err != nil {
		return err
	}
	s.setCookie(w, token, expires)

	// Remember this browser, so the global login limit never applies to it.
	// This is best effort: the login itself already succeeded.
	if known {
		_ = s.q.TouchDevice(ctx, hashToken(device))
		return nil
	}
	newDevice, deviceHash, err := newToken()
	if err != nil {
		return nil
	}
	if s.q.CreateDevice(ctx, deviceHash) == nil {
		http.SetCookie(w, &http.Cookie{
			Name:     DeviceCookieName,
			Value:    newDevice,
			Path:     deviceCookiePath,
			MaxAge:   int(DeviceTTL / time.Second),
			HttpOnly: true,
			Secure:   s.secure,
			SameSite: http.SameSiteLaxMode,
		})
	}
	return nil
}

// SweepExpired deletes expired sessions and browsers unseen for DeviceTTL.
func (s *Sessions) SweepExpired(ctx context.Context) (sessions, devices int64, err error) {
	if sessions, err = s.q.DeleteExpiredSessions(ctx); err != nil {
		return
	}
	devices, err = s.q.DeleteStaleDevices(ctx, time.Now().Add(-DeviceTTL))
	return
}

// Logout ends the current session, if any, and clears the cookie.
func (s *Sessions) Logout(ctx context.Context, w http.ResponseWriter, r *http.Request) error {
	if c, err := r.Cookie(CookieName); err == nil && c.Value != "" {
		if err := s.q.DeleteSession(ctx, hashToken(c.Value)); err != nil {
			return err
		}
	}
	s.setCookie(w, "", time.Unix(0, 0))
	return nil
}

// Check reports whether the request carries a valid session. Sessions that
// have not been extended for a while get a new expiry and a refreshed cookie.
func (s *Sessions) Check(ctx context.Context, w http.ResponseWriter, r *http.Request) (bool, error) {
	c, err := r.Cookie(CookieName)
	if err != nil || c.Value == "" {
		return false, nil
	}
	tokenHash := hashToken(c.Value)
	sess, err := s.q.GetSession(ctx, tokenHash)
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if time.Since(sess.LastSeenAt) > touchEvery {
		expires := time.Now().Add(SessionTTL)
		if err := s.q.TouchSession(ctx, store.TouchSessionParams{TokenHash: tokenHash, ExpiresAt: expires}); err != nil {
			return false, err
		}
		s.setCookie(w, c.Value, expires)
	}
	return true, nil
}

func (s *Sessions) setCookie(w http.ResponseWriter, value string, expires time.Time) {
	c := &http.Cookie{
		Name:     CookieName,
		Value:    value,
		Path:     "/",
		Expires:  expires,
		HttpOnly: true,
		Secure:   s.secure,
		SameSite: http.SameSiteLaxMode,
	}
	if value == "" {
		c.MaxAge = -1
	}
	http.SetCookie(w, c)
}

func newToken() (token string, tokenHash []byte, err error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", nil, err
	}
	token = base64.RawURLEncoding.EncodeToString(b)
	return token, hashToken(token), nil
}

// truncateUTF8 returns valid UTF-8 of at most max bytes, without splitting a
// character (PostgreSQL rejects invalid UTF-8 in text columns).
func truncateUTF8(v string, max int) string {
	v = strings.ToValidUTF8(v, "")
	if len(v) <= max {
		return v
	}
	for max > 0 && !utf8.RuneStart(v[max]) {
		max--
	}
	return v[:max]
}

func hashToken(token string) []byte {
	sum := sha256.Sum256([]byte(token))
	return sum[:]
}
