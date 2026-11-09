package server

import (
	"net/http"
	"strconv"
	"time"

	"fever/internal/apitypes"
	"fever/internal/store"
)

// readingDefaults are used where a feed or group sets nothing. They are the
// settings of the old Fever installation: full articles with images, oldest
// first, unread items only, marked read on scroll, 20 at a time.
var readingDefaults = apitypes.Defaults{
	OldestFirst: true,
	ShowImages:  true,
	ShowRead:    false,
	AutoRead:    true,
	PageSize:    20,
}

const (
	maxPageSize = 100
	maxStateIDs = 1000
)

// maxChanges is how many changes one GET /api/items/changes returns (a
// variable so tests can lower it).
var maxChanges = 2000

// listItems answers GET /api/items?scope=all|saved|group|feed&id=…&unread=1
// &order=oldest|newest&after=<cursor>&limit=…
func (s *Server) listItems(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var sq store.StreamQuery
	switch scope := q.Get("scope"); scope {
	case "", "all":
	case "saved":
		sq.SavedOnly = true
	case "feed", "group":
		id, err := strconv.ParseInt(q.Get("id"), 10, 64)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Missing or invalid id.")
			return
		}
		if scope == "feed" {
			sq.FeedID = &id
		} else {
			sq.GroupID = &id
		}
	default:
		writeError(w, http.StatusBadRequest, "Unknown scope.")
		return
	}
	// Saved items are listed whether read or not.
	sq.UnreadOnly = q.Get("unread") == "1" && !sq.SavedOnly
	sq.OldestFirst = q.Get("order") == "oldest"

	limit := readingDefaults.PageSize
	if v := q.Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 || n > maxPageSize {
			writeError(w, http.StatusBadRequest, "Invalid limit.")
			return
		}
		limit = n
	}
	sq.Limit = limit + 1 // one more, to know whether there is a next page
	if v := q.Get("after"); v != "" {
		c, err := store.ParseCursor(v)
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid cursor.")
			return
		}
		sq.After = &c
	}

	loadedAt := time.Now()
	rows, err := s.q.ItemStream(r.Context(), sq)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	page := apitypes.ItemPage{Items: make([]apitypes.Item, 0, min(len(rows), limit)), LoadedAt: loadedAt}
	if len(rows) > limit {
		rows = rows[:limit]
		last := rows[len(rows)-1]
		next := store.Cursor{Time: last.PublishedAt, ID: last.ID}.String()
		page.Next = &next
	}
	for _, it := range rows {
		page.Items = append(page.Items, apitypes.Item{
			ID:          it.ID,
			FeedID:      it.FeedID,
			Title:       it.Title,
			URL:         it.Url,
			Author:      it.Author,
			PublishedAt: it.PublishedAt,
			Read:        it.Read,
			Saved:       it.Saved,
			Content:     it.Content,
		})
	}
	writeJSON(w, http.StatusOK, page)
}

// setItemState answers POST /api/items/state: marks items read or unread,
// saved or not.
func (s *Server) setItemState(w http.ResponseWriter, r *http.Request) {
	var req apitypes.ItemStateRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if len(req.IDs) == 0 || len(req.IDs) > maxStateIDs {
		writeError(w, http.StatusBadRequest, "Give between 1 and 1000 item ids.")
		return
	}
	ctx := r.Context()
	if req.Read != nil {
		mark := s.q.MarkUnread
		if *req.Read {
			mark = s.q.MarkRead
		}
		if _, err := mark(ctx, req.IDs); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	if req.Saved != nil {
		mark := s.q.MarkUnsaved
		if *req.Saved {
			mark = s.q.MarkSaved
		}
		if _, err := mark(ctx, req.IDs); err != nil {
			s.serverError(w, r, err)
			return
		}
	}
	w.WriteHeader(http.StatusNoContent)
}

// markAllRead answers POST /api/items/read-all.
func (s *Server) markAllRead(w http.ResponseWriter, r *http.Request) {
	var req apitypes.MarkAllReadRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Before.IsZero() {
		writeError(w, http.StatusBadRequest, "Missing before.")
		return
	}
	ctx := r.Context()
	var err error
	switch {
	case req.Scope == "all":
		_, err = s.q.MarkAllRead(ctx, req.Before)
	case req.Scope == "feed" && req.ID != nil:
		_, err = s.q.MarkFeedRead(ctx, store.MarkFeedReadParams{Before: req.Before, FeedID: *req.ID})
	case req.Scope == "group" && req.ID != nil:
		_, err = s.q.MarkGroupRead(ctx, store.MarkGroupReadParams{Before: req.Before, GroupID: *req.ID})
	default:
		writeError(w, http.StatusBadRequest, "Scope must be all, or group or feed with an id.")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// itemChanges answers GET /api/items/changes?since=<RFC 3339 time>, then
// ?after=<cursor> while the answer has a next cursor.
func (s *Server) itemChanges(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	var from store.Cursor
	switch {
	case q.Get("after") != "":
		c, err := store.ParseCursor(q.Get("after"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "Invalid cursor.")
			return
		}
		from = c
	default:
		since, err := time.Parse(time.RFC3339Nano, q.Get("since"))
		if err != nil {
			writeError(w, http.StatusBadRequest, "Missing or invalid since.")
			return
		}
		// A small overlap covers clock differences between this server and
		// the database; a change reported twice is harmless.
		from = store.Cursor{Time: since.Add(-2 * time.Second)}
	}

	loadedAt := time.Now()
	rows, err := s.q.ItemChanges(r.Context(), store.ItemChangesParams{
		AfterTime: from.Time,
		AfterID:   from.ID,
		MaxRows:   int32(maxChanges + 1),
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	res := apitypes.ItemChanges{LoadedAt: loadedAt}
	if len(rows) > maxChanges {
		rows = rows[:maxChanges]
		last := rows[len(rows)-1]
		next := store.Cursor{Time: last.StateChangedAt, ID: last.ID}.String()
		res.Next = &next
	}
	res.Changes = make([]apitypes.ItemChange, 0, len(rows))
	for _, c := range rows {
		res.Changes = append(res.Changes, apitypes.ItemChange{ID: c.ID, Read: c.Read, Saved: c.Saved})
	}
	writeJSON(w, http.StatusOK, res)
}
