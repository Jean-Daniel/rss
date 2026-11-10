package server

import (
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"

	"fever/internal/apitypes"
	"fever/internal/auth"
	"fever/internal/feeds"
)

func (s *Server) getSession(w http.ResponseWriter, r *http.Request) {
	ok, err := s.sessions.Check(r.Context(), w, r)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	hash, err := s.q.GetPasswordHash(r.Context())
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.Session{
		Authenticated: ok,
		PasswordSet:   hash != nil && *hash != "",
	})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var req apitypes.LoginRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	err := s.sessions.Login(r.Context(), w, r, req.Password, s.clientIP(r))
	switch {
	case err == nil:
		writeJSON(w, http.StatusOK, apitypes.Session{Authenticated: true, PasswordSet: true})
	case errors.Is(err, auth.ErrBadPassword):
		writeError(w, http.StatusUnauthorized, "Wrong password.")
	case errors.Is(err, auth.ErrTooManyAttempts):
		writeError(w, http.StatusTooManyRequests, "Too many failed attempts. Try again in 15 minutes.")
	case errors.Is(err, auth.ErrNoPassword):
		writeError(w, http.StatusConflict, "No password is set yet. Run `fever set-password` on the server.")
	default:
		s.serverError(w, r, err)
	}
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if err := s.sessions.Logout(r.Context(), w, r); err != nil {
		s.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) getSubscriptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	groups, err := s.q.ListGroups(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	subscribed, err := s.q.ListFeeds(ctx)
	if err != nil {
		s.serverError(w, r, err)
		return
	}

	res := apitypes.Subscriptions{
		Groups: make([]apitypes.Group, 0, len(groups)),
		Feeds:  make([]apitypes.Feed, 0, len(subscribed)),
	}
	if res.Defaults, err = s.preferences(ctx); err != nil {
		s.serverError(w, r, err)
		return
	}
	unread := make(map[int64]int64, len(subscribed))
	feedsOfGroup := make(map[int64][]int64)
	res.Refreshing = s.refresher.Busy()
	res.RetentionWeeks = int(feeds.Retention / (7 * 24 * time.Hour))
	now := time.Now()
	for _, f := range subscribed {
		res.SavedCount = f.SavedCount     // the same on every row
		if f.LastRefreshedAt.Unix() > 0 { // epoch means never
			t := f.LastRefreshedAt
			res.LastRefreshedAt = &t
		}
		res.TotalUnread += f.Unread
		unread[f.ID] = f.Unread
		for _, gid := range f.GroupIds {
			feedsOfGroup[gid] = append(feedsOfGroup[gid], f.ID)
		}
		res.Feeds = append(res.Feeds, apitypes.Feed{
			ID:        f.ID,
			Title:     f.DisplayTitle,
			URL:       f.Url,
			SiteURL:   f.SiteUrl,
			FaviconID: f.FaviconID,
			Unread:    f.Unread,
			GroupIDs:  f.GroupIds,
			DisplayOptions: apitypes.DisplayOptions{
				ShowUnreadCount: f.ShowUnreadCount,
				ShowImages:      f.ShowImages,
				OldestFirst:     f.OldestFirst,
			},
			Error:         f.LastError,
			LastUpdatedAt: f.LastNewItemAt,
			Stale:         isStale(f.LastFetchedAt, f.LastNewItemAt, now),
		})
	}
	for _, g := range groups {
		fids := feedsOfGroup[g.ID]
		if fids == nil {
			fids = []int64{}
		}
		var n int64
		for _, id := range fids {
			n += unread[id]
		}
		res.Groups = append(res.Groups, apitypes.Group{
			ID:      g.ID,
			Title:   g.Title,
			Unread:  n,
			FeedIDs: fids,
			DisplayOptions: apitypes.DisplayOptions{
				ShowUnreadCount: g.ShowUnreadCount,
				ShowImages:      g.ShowImages,
				OldestFirst:     g.OldestFirst,
			},
		})
	}
	writeJSON(w, http.StatusOK, res)
}

// isStale reports whether a feed that has been fetched at least once has
// brought no new item for the retention period.
func isStale(lastFetched, lastNewItem *time.Time, now time.Time) bool {
	if lastFetched == nil {
		return false // not fetched yet: nothing to judge
	}
	return lastNewItem == nil || lastNewItem.Before(now.Add(-feeds.Retention))
}

// refresh starts fetching every feed, or one feed, right away. The client
// follows progress through the refreshing flag of GET /api/subscriptions.
func (s *Server) refresh(w http.ResponseWriter, r *http.Request) {
	var req apitypes.RefreshRequest
	if r.ContentLength != 0 && !decodeJSON(w, r, &req) {
		return
	}
	switch {
	case req.FeedID != nil:
		found, err := s.refresher.RefreshFeed(r.Context(), *req.FeedID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "Unknown feed.")
			return
		}
	case req.GroupID != nil:
		found, err := s.refresher.RefreshGroup(r.Context(), *req.GroupID)
		if err != nil {
			s.serverError(w, r, err)
			return
		}
		if !found {
			writeError(w, http.StatusNotFound, "Unknown group.")
			return
		}
	default:
		if err := s.refresher.RefreshAll(r.Context()); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusAccepted)
}

func (s *Server) getFavicon(w http.ResponseWriter, r *http.Request) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "Unknown favicon.")
		return
	}
	icon, err := s.q.GetFavicon(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Unknown favicon.")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	etag := fmt.Sprintf(`"%d-%d"`, id, icon.FetchedAt.Unix())
	h := w.Header()
	h.Set("ETag", etag)
	h.Set("Cache-Control", "private, max-age=604800")
	// Favicons come from third-party sites. If one is an SVG with scripts,
	// this keeps it inert even when opened directly.
	h.Set("Content-Security-Policy", "default-src 'none'; style-src 'unsafe-inline'; sandbox")
	if r.Header.Get("If-None-Match") == etag {
		w.WriteHeader(http.StatusNotModified)
		return
	}
	h.Set("Content-Type", icon.MimeType)
	w.Write(icon.Data)
}
