package server

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"fever/internal/apitypes"
	"fever/internal/content"
	"fever/internal/feeds"
	"fever/internal/opml"
	"fever/internal/store"
)

const (
	// addFeedTimeout bounds finding a new feed and storing its items. It is
	// shorter than the time nginx and the server wait for an answer.
	addFeedTimeout = 40 * time.Second
	// OPML files are small: 2 MB is thousands of feeds (nginx's limit too).
	maxOPMLBytes   = 2 << 20
	maxImportFeeds = 5000
)

func (s *Server) putPreferences(w http.ResponseWriter, r *http.Request) {
	var req apitypes.Preferences
	if !decodeJSON(w, r, &req) {
		return
	}
	err := s.q.SetPreferences(r.Context(), store.SetPreferencesParams{
		OldestFirst: req.OldestFirst,
		ShowImages:  req.ShowImages,
		ShowRead:    req.ShowRead,
		AutoRead:    req.AutoRead,
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.prefs.Store(&apitypes.Defaults{Preferences: req, PageSize: pageSize})
	w.WriteHeader(http.StatusNoContent)
}

// preferences are the reading defaults: the stored preferences, and the
// page size, which is not one. They are read from the database once.
func (s *Server) preferences(ctx context.Context) (apitypes.Defaults, error) {
	if d := s.prefs.Load(); d != nil {
		return *d, nil
	}
	p, err := s.q.GetPreferences(ctx)
	if err != nil {
		return apitypes.Defaults{}, err
	}
	d := &apitypes.Defaults{
		Preferences: apitypes.Preferences{
			OldestFirst: p.OldestFirst,
			ShowImages:  p.ShowImages,
			ShowRead:    p.ShowRead,
			AutoRead:    p.AutoRead,
		},
		PageSize: pageSize,
	}
	// Unless a change was saved meanwhile: this read may predate it.
	s.prefs.CompareAndSwap(nil, d)
	return *s.prefs.Load(), nil
}

// addFeed subscribes to a feed, given its address or a web page that links
// to it, and stores its items before answering, so they can be shown right
// away.
func (s *Server) addFeed(w http.ResponseWriter, r *http.Request) {
	var req apitypes.AddFeedRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), addFeedTimeout)
	defer cancel()
	username, password := login(req.Username, req.Password)

	// Already subscribed: no need to download anything.
	if addr, ok := feedAddress(req.URL); ok && s.answerExisting(w, r, addr) {
		return
	}
	found, choices, err := s.refresher.Discover(ctx, req.URL, username, password)
	if r.Context().Err() != nil {
		return // the client went away
	}
	if err != nil {
		writeError(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	if len(choices) > 0 {
		res := apitypes.AddFeedResponse{Choices: make([]apitypes.FeedChoice, len(choices))}
		for i, c := range choices {
			res.Choices[i] = apitypes.FeedChoice{URL: c.URL, Title: c.Title}
		}
		writeJSON(w, http.StatusOK, res)
		return
	}
	if s.answerExisting(w, r, found.URL) { // the page linked to a known feed
		return
	}

	// Not the login given if the page linked to a feed on another site:
	// Discover did not send it there, and neither will the refresher.
	username, password = found.Login()
	var id int64
	err = s.inTx(ctx, func(q *store.Queries) error {
		var err error
		id, err = q.CreateFeed(ctx, store.CreateFeedParams{
			Url:          found.URL,
			AuthUsername: username,
			AuthPassword: password,
			// Fetched here: the refresher should not take it meanwhile.
			NextFetchAt: time.Now().Add(time.Hour),
		})
		if err != nil {
			return err
		}
		return q.SetFeedGroups(ctx, store.SetFeedGroupsParams{FeedID: id, GroupIds: nonNil(req.GroupIDs)})
	})
	if errors.Is(err, pgx.ErrNoRows) { // added by another request meanwhile
		s.answerExisting(w, r, found.URL)
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	s.refresher.Subscribed(ctx, id, found)
	writeJSON(w, http.StatusCreated, apitypes.AddFeedResponse{FeedID: &id})
}

// answerExisting answers an addFeed for a feed already subscribed to, and
// reports whether it did.
func (s *Server) answerExisting(w http.ResponseWriter, r *http.Request, url string) bool {
	id, err := s.q.FeedIDByURL(r.Context(), url)
	if errors.Is(err, pgx.ErrNoRows) {
		return false
	}
	if err != nil {
		s.serverError(w, r, err)
		return true
	}
	writeJSON(w, http.StatusOK, apitypes.AddFeedResponse{FeedID: &id, Existing: true})
	return true
}

func (s *Server) getFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "feed")
	if !ok {
		return
	}
	f, err := s.q.GetFeedSettings(r.Context(), id)
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Unknown feed.")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, apitypes.FeedSettings{
		ID:       f.ID,
		Title:    f.Title,
		URL:      f.Url,
		GroupIDs: f.GroupIds,
		DisplayOptions: apitypes.DisplayOptions{
			ShowImages:      f.ShowImages,
			OldestFirst:     f.OldestFirst,
			ShowUnreadCount: f.ShowUnreadCount,
		},
		Username:    f.AuthUsername,
		HasPassword: f.HasPassword,
		ItemCount:   f.ItemCount,
		SavedCount:  f.SavedCount,
	})
}

func (s *Server) updateFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "feed")
	if !ok {
		return
	}
	var req apitypes.FeedUpdate
	if !decodeJSON(w, r, &req) {
		return
	}
	addr, ok := feedAddress(req.URL)
	if !ok {
		writeError(w, http.StatusBadRequest, "Enter the feed's address, starting with http:// or https://.")
		return
	}
	title := content.Text(req.Title, 500)
	ctx := r.Context()
	var refetch bool
	err := s.inTx(ctx, func(q *store.Queries) error {
		cur, err := q.FeedForUpdate(ctx, id)
		if err != nil {
			return err
		}
		password := req.Password
		if password == nil {
			password = cur.AuthPassword // unchanged
		}
		username, password := login(req.Username, password)
		err = q.UpdateFeed(ctx, store.UpdateFeedParams{
			ID:              id,
			Title:           title,
			Url:             addr,
			ShowImages:      req.ShowImages,
			OldestFirst:     req.OldestFirst,
			ShowUnreadCount: req.ShowUnreadCount,
			AuthUsername:    username,
			AuthPassword:    password,
		})
		if err != nil {
			return err
		}
		if err := q.SetFeedGroups(ctx, store.SetFeedGroupsParams{FeedID: id, GroupIds: nonNil(req.GroupIDs)}); err != nil {
			return err
		}
		// A new address or login is tried right away; an empty title is
		// filled in from the feed.
		fresh := addr != cur.Url || !sameString(username, cur.AuthUsername) || !sameString(password, cur.AuthPassword)
		refetch = fresh || title == ""
		if refetch {
			return q.RefetchFeed(ctx, store.RefetchFeedParams{ID: id, FreshStart: fresh})
		}
		return nil
	})
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		writeError(w, http.StatusNotFound, "Unknown feed.")
		return
	case isUniqueViolation(err):
		writeError(w, http.StatusConflict, "Another feed has this address already.")
		return
	case err != nil:
		s.serverError(w, r, err)
		return
	}
	if refetch {
		s.refresher.Wake()
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteFeed unsubscribes from a feed. Its items go with it, saved ones
// included, as in Fever.
func (s *Server) deleteFeed(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "feed")
	if !ok {
		return
	}
	n, err := s.q.DeleteFeed(r.Context(), id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "Unknown feed.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) createGroup(w http.ResponseWriter, r *http.Request) {
	var req apitypes.GroupUpdate
	if !decodeJSON(w, r, &req) {
		return
	}
	title, ok := groupTitle(w, req.Title)
	if !ok {
		return
	}
	ctx := r.Context()
	var id int64
	err := s.inTx(ctx, func(q *store.Queries) error {
		var err error
		id, err = q.CreateGroup(ctx, store.CreateGroupParams{
			Title:           title,
			ShowImages:      req.ShowImages,
			OldestFirst:     req.OldestFirst,
			ShowUnreadCount: req.ShowUnreadCount,
		})
		if err != nil {
			return err
		}
		return q.SetGroupFeeds(ctx, store.SetGroupFeedsParams{GroupID: id, FeedIds: nonNil(req.FeedIDs)})
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	writeJSON(w, http.StatusCreated, apitypes.Created{ID: id})
}

func (s *Server) updateGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "group")
	if !ok {
		return
	}
	var req apitypes.GroupUpdate
	if !decodeJSON(w, r, &req) {
		return
	}
	title, ok := groupTitle(w, req.Title)
	if !ok {
		return
	}
	ctx := r.Context()
	err := s.inTx(ctx, func(q *store.Queries) error {
		n, err := q.UpdateGroup(ctx, store.UpdateGroupParams{
			ID:              id,
			Title:           title,
			ShowImages:      req.ShowImages,
			OldestFirst:     req.OldestFirst,
			ShowUnreadCount: req.ShowUnreadCount,
		})
		if err != nil {
			return err
		}
		if n == 0 {
			return pgx.ErrNoRows
		}
		return q.SetGroupFeeds(ctx, store.SetGroupFeedsParams{GroupID: id, FeedIds: nonNil(req.FeedIDs)})
	})
	if errors.Is(err, pgx.ErrNoRows) {
		writeError(w, http.StatusNotFound, "Unknown group.")
		return
	}
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// deleteGroup deletes a group; its feeds stay.
func (s *Server) deleteGroup(w http.ResponseWriter, r *http.Request) {
	id, ok := pathID(w, r, "group")
	if !ok {
		return
	}
	n, err := s.q.DeleteGroup(r.Context(), id)
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if n == 0 {
		writeError(w, http.StatusNotFound, "Unknown group.")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) exportOPML(w http.ResponseWriter, r *http.Request) {
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
	inGroup := make(map[int64][]opml.Entry, len(groups))
	var ungrouped []opml.Entry
	for _, f := range subscribed {
		e := opml.Entry{Title: f.DisplayTitle, URL: f.Url, SiteURL: f.SiteUrl}
		for _, gid := range f.GroupIds {
			inGroup[gid] = append(inGroup[gid], e)
		}
		if len(f.GroupIds) == 0 {
			ungrouped = append(ungrouped, e)
		}
	}
	out := make([]opml.Group, 0, len(groups))
	for _, g := range groups {
		out = append(out, opml.Group{Title: g.Title, Feeds: inGroup[g.ID]})
	}
	h := w.Header()
	h.Set("Content-Type", "text/x-opml; charset=utf-8")
	h.Set("Content-Disposition", `attachment; filename="fever-subscriptions.opml"`)
	if err := opml.Write(w, "Fever subscriptions", time.Now(), out, ungrouped); err != nil {
		s.log.Warn("write OPML", "err", err)
	}
}

// importOPML adds the feeds of an OPML file (the request body) that are not
// subscribed to yet, and puts every feed listed in the file's groups,
// creating the groups that do not exist (matched by name, ignoring case).
// New feeds are fetched in the background.
func (s *Server) importOPML(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxOPMLBytes)
	list, err := opml.Parse(r.Body)
	if err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			writeError(w, http.StatusRequestEntityTooLarge, "The file is too large.")
			return
		}
		writeError(w, http.StatusBadRequest, err.Error())
		return
	}
	if len(list.Feeds) > maxImportFeeds {
		writeError(w, http.StatusBadRequest, "The file lists too many feeds.")
		return
	}
	ctx := r.Context()
	var res apitypes.OPMLImport
	err = s.inTx(ctx, func(q *store.Queries) error {
		existing, err := q.ListGroups(ctx)
		if err != nil {
			return err
		}
		groupIDs := make(map[string]int64, len(existing))
		for _, g := range existing {
			groupIDs[strings.ToLower(g.Title)] = g.ID
		}
		// The group with this name, created if there is none.
		group := func(name string) (int64, error) {
			if id, ok := groupIDs[strings.ToLower(name)]; ok {
				return id, nil
			}
			id, err := q.CreateGroup(ctx, store.CreateGroupParams{Title: name})
			if err != nil {
				return 0, err
			}
			groupIDs[strings.ToLower(name)] = id
			res.GroupsAdded++
			return id, nil
		}
		for _, name := range list.Groups { // empty ones included
			if name := content.Text(name, 200); name != "" {
				if _, err := group(name); err != nil {
					return err
				}
			}
		}
		seen := make(map[string]bool)
		for _, e := range list.Feeds {
			addr, ok := feedAddress(e.URL)
			if !ok {
				res.Skipped++
				continue
			}
			site := ""
			if u := content.ParseHTTP(e.SiteURL); u != nil {
				site = u.String()
			}
			id, err := q.ImportFeed(ctx, store.ImportFeedParams{Url: addr, Title: importedTitle(e.Title, addr), SiteUrl: site})
			inserted := err == nil
			if errors.Is(err, pgx.ErrNoRows) { // subscribed already
				id, err = q.FeedIDByURL(ctx, addr)
			}
			if err != nil {
				return err
			}
			if !seen[addr] { // a feed may be listed in several groups
				seen[addr] = true
				if inserted {
					res.FeedsAdded++
				} else {
					res.FeedsKnown++
				}
			}
			name := content.Text(e.Group, 200)
			if name == "" {
				continue
			}
			gid, err := group(name)
			if err != nil {
				return err
			}
			if err := q.AddFeedToGroup(ctx, store.AddFeedToGroupParams{FeedID: id, GroupID: gid}); err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		s.serverError(w, r, err)
		return
	}
	if res.FeedsAdded > 0 {
		s.refresher.Wake()
	}
	writeJSON(w, http.StatusOK, res)
}

// inTx runs fn in a transaction, committed if fn succeeds.
func (s *Server) inTx(ctx context.Context, fn func(q *store.Queries) error) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.WithoutCancel(ctx))
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func pathID(w http.ResponseWriter, r *http.Request, what string) (int64, bool) {
	id, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil {
		writeError(w, http.StatusNotFound, "Unknown "+what+".")
		return 0, false
	}
	return id, true
}

// feedAddress checks a feed address and returns it as stored: written as
// the importer writes addresses, so the same address always compares equal.
func feedAddress(raw string) (string, bool) {
	addr := feeds.NormalizeURL(raw)
	if content.ParseHTTP(addr) == nil {
		return "", false
	}
	return addr, true
}

// importedTitle is the title to store for a feed from an OPML file. Readers
// (this one included) write the feed's host for feeds without a title:
// stored, it would stay instead of the feed's own title.
func importedTitle(raw, addr string) string {
	title := content.Text(raw, 500)
	if u := content.ParseHTTP(addr); u != nil &&
		(strings.EqualFold(title, u.Host) || strings.EqualFold(title, u.Hostname()) || title == addr) {
		return ""
	}
	return title
}

func groupTitle(w http.ResponseWriter, raw string) (string, bool) {
	title := content.Text(raw, 200)
	if title == "" {
		writeError(w, http.StatusBadRequest, "Give the group a name.")
		return "", false
	}
	return title, true
}

// login returns a feed's login as stored: none without a username, or with
// neither a username nor a password. The username may be empty: some feeds
// only want a password (a token).
func login(username, password *string) (*string, *string) {
	if username == nil {
		return nil, nil
	}
	u := strings.TrimSpace(*username)
	hasPassword := password != nil && *password != ""
	switch {
	case u == "" && !hasPassword:
		return nil, nil
	case !hasPassword:
		return &u, nil
	}
	return &u, password
}

func sameString(a, b *string) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// nonNil turns a missing list into an empty one: a nil slice would be sent
// to PostgreSQL as NULL, which matches nothing, not even in NOT IN.
func nonNil(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

func isUniqueViolation(err error) bool {
	var pgErr *pgconn.PgError
	return errors.As(err, &pgErr) && pgErr.Code == "23505"
}
