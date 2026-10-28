package server

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"

	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// ---- Google Reader API ----
//
// The sync protocol FreshRSS, Inoreader and The Old Reader speak, so phone
// and desktop RSS apps (Reeder, NetNewsWire, ReadKit, FeedMe, Fluent Reader,
// Read You…) work with Pocket Reader. Point them at BASE_URL/api/greader.php
// (choose "FreshRSS" or "Google Reader API"), sign in with your email and a
// personal token from Settings → API & shortcuts as the password.
//
// Only feed posts take part; pages saved to read later show up under the
// label "Read later" (adding that label to a post saves it).

const (
	grReadingList = "user/-/state/com.google/reading-list"
	grRead        = "user/-/state/com.google/read"
	grStarred     = "user/-/state/com.google/starred"
	grKeptUnread  = "user/-/state/com.google/kept-unread"
	grLabel       = "user/-/label/"
	grReadLater   = "Read later"
	grItemPrefix  = "tag:google.com,2005:reader/item/"
)

func (s *Server) greaderRoutes(r chi.Router) {
	r.Post("/accounts/ClientLogin", s.grClientLogin)
	r.Get("/accounts/ClientLogin", s.grClientLogin)
	r.Group(func(r chi.Router) {
		r.Use(s.grAuth)
		r.Get("/reader/api/0/token", s.grToken)
		r.Get("/reader/api/0/user-info", s.grUserInfo)
		r.Get("/reader/api/0/subscription/list", s.grSubscriptions)
		r.Post("/reader/api/0/subscription/edit", s.grEditSubscription)
		r.Post("/reader/api/0/subscription/quickadd", s.grQuickAdd)
		r.Get("/reader/api/0/subscription/export", s.handleExportOPML)
		r.Get("/reader/api/0/tag/list", s.grTags)
		r.Get("/reader/api/0/unread-count", s.grUnreadCount)
		r.Get("/reader/api/0/stream/items/ids", s.grItemIDs)
		r.Get("/reader/api/0/stream/items/contents", s.grItemContents)
		r.Post("/reader/api/0/stream/items/contents", s.grItemContents)
		r.Get("/reader/api/0/stream/contents/*", s.grStreamContents)
		r.Post("/reader/api/0/stream/contents/*", s.grStreamContents)
		r.Get("/reader/api/0/stream/contents", s.grStreamContents)
		r.Post("/reader/api/0/edit-tag", s.grEditTag)
		r.Post("/reader/api/0/mark-all-as-read", s.grMarkAllRead)
		r.Post("/reader/api/0/rename-tag", s.grRenameTag)
		r.Post("/reader/api/0/disable-tag", s.grDisableTag)
	})
}

func grText(w http.ResponseWriter, status int, body string) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.WriteHeader(status)
	_, _ = w.Write([]byte(body))
}

// grClientLogin signs in with an email and a personal token (as the password).
func (s *Server) grClientLogin(w http.ResponseWriter, r *http.Request) {
	_ = r.ParseForm()
	email := strings.ToLower(strings.TrimSpace(r.Form.Get("Email")))
	pass := strings.TrimSpace(r.Form.Get("Passwd"))
	u, err := s.store.GetUserByAPIToken(r.Context(), pass)
	if err != nil || (email != "" && !strings.EqualFold(u.Email, email) && !strings.EqualFold(u.Name, email)) {
		grText(w, http.StatusUnauthorized, "Error=BadAuthentication\n")
		return
	}
	grText(w, http.StatusOK, "SID="+pass+"\nLSID=null\nAuth="+pass+"\n")
}

// grAuth reads "Authorization: GoogleLogin auth=<token>" (or a Bearer token).
func (s *Server) grAuth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		tok := ""
		if i := strings.Index(strings.ToLower(h), "auth="); strings.HasPrefix(strings.ToLower(h), "googlelogin") && i >= 0 {
			tok = strings.TrimSpace(h[i+5:])
		} else if strings.HasPrefix(strings.ToLower(h), "bearer ") {
			tok = strings.TrimSpace(h[7:])
		}
		if tok == "" {
			grText(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		u, err := s.store.GetUserByAPIToken(r.Context(), tok)
		if err != nil {
			grText(w, http.StatusUnauthorized, "Unauthorized")
			return
		}
		ctx := context.WithValue(r.Context(), userKey, u)
		ctx = context.WithValue(ctx, grTokenKey, tok)
		_ = r.ParseForm()
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

const grTokenKey ctxKey = 9

func (s *Server) grToken(w http.ResponseWriter, r *http.Request) {
	tok, _ := r.Context().Value(grTokenKey).(string)
	sum := sha256.Sum256([]byte("pocket-reader:edit:" + tok))
	grText(w, http.StatusOK, strings.ToUpper(hex.EncodeToString(sum[:]))[:57]+"\n")
}

func (s *Server) grUserInfo(w http.ResponseWriter, r *http.Request) {
	u := userFrom(r.Context())
	id := strconv.FormatInt(u.ID, 10)
	writeJSON(w, http.StatusOK, map[string]any{"userId": id, "userName": firstNonEmpty(u.Name, u.Email), "userProfileId": id, "userEmail": u.Email, "isBloggerUser": false, "signupTimeSec": 0, "isMultiLoginEnabled": false})
}

// grFeeds is a person's feeds by id and their folder names.
type grFeeds struct {
	byID    map[int64]store.Feed
	folders map[int64]string
}

func (s *Server) grLoad(ctx context.Context, uid int64) (*grFeeds, []store.Feed) {
	feeds, _ := s.store.ListFeeds(ctx, uid)
	g := &grFeeds{byID: map[int64]store.Feed{}, folders: s.folderNames(ctx, uid)}
	for _, f := range feeds {
		g.byID[f.ID] = f
	}
	return g, feeds
}

func (s *Server) grSubscriptions(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	g, feeds := s.grLoad(ctx, actorID(ctx))
	subs := []map[string]any{}
	for _, f := range feeds {
		cats := []map[string]string{}
		if f.FolderID != nil {
			name := g.folders[*f.FolderID]
			cats = append(cats, map[string]string{"id": grLabel + name, "label": name})
		}
		subs = append(subs, map[string]any{"id": fmt.Sprintf("feed/%d", f.ID), "title": f.Name(), "categories": cats, "url": f.URL, "htmlUrl": f.SiteURL, "iconUrl": f.IconURL, "firstitemmsec": "0", "sortid": fmt.Sprintf("%08X", f.ID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"subscriptions": subs})
}

// grFeedID reads "feed/<id>" or "feed/<address>".
func (s *Server) grFeedID(ctx context.Context, uid int64, stream string) int64 {
	v := strings.TrimPrefix(stream, "feed/")
	if id, err := strconv.ParseInt(v, 10, 64); err == nil {
		if _, err := s.store.GetFeed(ctx, uid, id); err == nil {
			return id
		}
	}
	if f, err := s.store.FeedByURL(ctx, uid, v); err == nil {
		return f.ID
	}
	return 0
}

func (s *Server) grEditSubscription(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	ac := r.Form.Get("ac")
	title := strings.TrimSpace(r.Form.Get("t"))
	add := strings.TrimPrefix(r.Form.Get("a"), grLabel)
	remove := r.Form.Get("r")
	for _, stream := range r.Form["s"] {
		switch ac {
		case "subscribe":
			raw := strings.TrimPrefix(stream, "feed/")
			if _, _, err := s.subscribe(ctx, uid, feedInput{URL: raw, Title: title, Folder: add}, "greader"); err != nil && err != store.ErrConflict {
				grText(w, http.StatusBadRequest, err.Error())
				return
			}
		case "unsubscribe":
			if id := s.grFeedID(ctx, uid, stream); id > 0 {
				_ = s.store.DeleteFeed(ctx, uid, id)
			}
		case "edit":
			id := s.grFeedID(ctx, uid, stream)
			if id == 0 {
				grText(w, http.StatusNotFound, "no such feed")
				return
			}
			f, err := s.store.GetFeed(ctx, uid, id)
			if err != nil {
				grText(w, http.StatusNotFound, "no such feed")
				return
			}
			if title != "" {
				f.CustomTitle = title
			}
			if add != "" {
				if folder, err := s.store.FolderByName(ctx, uid, add, true); err == nil {
					f.FolderID = &folder.ID
				}
			} else if remove != "" {
				f.FolderID = nil
			}
			_ = s.store.UpdateFeed(ctx, uid, f)
		}
	}
	grText(w, http.StatusOK, "OK")
}

func (s *Server) grQuickAdd(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	raw := strings.TrimPrefix(r.Form.Get("quickadd"), "feed/")
	f, _, err := s.subscribe(ctx, actorID(ctx), feedInput{URL: raw}, "greader")
	if err != nil && f == nil {
		writeJSON(w, http.StatusOK, map[string]any{"numResults": 0, "query": raw, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"numResults": 1, "query": raw, "streamId": fmt.Sprintf("feed/%d", f.ID), "streamName": f.Name()})
}

func (s *Server) grTags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	folders, _ := s.store.ListFolders(ctx, actorID(ctx))
	tags := []map[string]any{{"id": grStarred}, {"id": grLabel + grReadLater, "type": "tag"}}
	for _, f := range folders {
		tags = append(tags, map[string]any{"id": grLabel + f.Name, "type": "folder", "sortid": fmt.Sprintf("%08X", f.ID)})
	}
	writeJSON(w, http.StatusOK, map[string]any{"tags": tags})
}

func (s *Server) grUnreadCount(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	g, feeds := s.grLoad(ctx, uid)
	var out []map[string]any
	total := 0
	byFolder := map[int64]int{}
	newest := func(f store.Feed) string {
		if t, err := time.Parse(time.RFC3339, f.NewestAt); err == nil {
			return strconv.FormatInt(t.UnixMicro(), 10)
		}
		return "0"
	}
	for _, f := range feeds {
		total += f.Unread
		if f.FolderID != nil {
			byFolder[*f.FolderID] += f.Unread
		}
		out = append(out, map[string]any{"id": fmt.Sprintf("feed/%d", f.ID), "count": f.Unread, "newestItemTimestampUsec": newest(f)})
	}
	for id, n := range byFolder {
		out = append(out, map[string]any{"id": grLabel + g.folders[id], "count": n, "newestItemTimestampUsec": "0"})
	}
	out = append(out, map[string]any{"id": grReadingList, "count": total, "newestItemTimestampUsec": strconv.FormatInt(time.Now().UnixMicro(), 10)})
	writeJSON(w, http.StatusOK, map[string]any{"max": total, "unreadcounts": out})
}

// grFilter turns a stream and the usual parameters into an article filter.
func (s *Server) grFilter(ctx context.Context, uid int64, stream string, form url.Values) (store.ArticleFilter, bool) {
	f := store.ArticleFilter{View: "", FeedOnly: true}
	switch {
	case stream == "" || stream == grReadingList:
	case stream == grStarred:
		f.View, f.FeedOnly = "starred", false
	case stream == grRead:
		f.ReadOnly = true
	case stream == grLabel+grReadLater:
		f.View, f.FeedOnly = "saved", false
	case strings.HasPrefix(stream, grLabel):
		folder, err := s.store.FolderByName(ctx, uid, strings.TrimPrefix(stream, grLabel), false)
		if err != nil {
			return f, false
		}
		f.FolderID = folder.ID
	case strings.HasPrefix(stream, "feed/"):
		f.FeedID = s.grFeedID(ctx, uid, stream)
		if f.FeedID == 0 {
			return f, false
		}
	default:
		return f, false
	}
	switch form.Get("xt") {
	case grRead:
		f.Unread = true
	case grStarred:
		// rare; ignored
	}
	if form.Get("it") == grStarred {
		f.View, f.FeedOnly = "starred", false
	}
	if ot, err := strconv.ParseInt(form.Get("ot"), 10, 64); err == nil && ot > 0 {
		f.Newer = time.Unix(ot, 0).UTC().Format(time.RFC3339)
	}
	if nt, err := strconv.ParseInt(form.Get("nt"), 10, 64); err == nil && nt > 0 {
		f.Older = time.Unix(nt, 0).UTC().Format(time.RFC3339)
	}
	n, _ := strconv.Atoi(form.Get("n"))
	if n <= 0 {
		n = 20
	}
	if n > 1000 {
		n = 1000
	}
	f.Limit = n
	f.Oldest = form.Get("r") == "o"
	if c := form.Get("c"); c != "" {
		f.Before, f.BeforeID = store.ParseCursor(c)
	}
	return f, true
}

func (s *Server) grItemIDs(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	f, ok := s.grFilter(ctx, uid, r.Form.Get("s"), r.Form)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"itemRefs": []any{}})
		return
	}
	list, next, err := s.store.ListArticles(ctx, uid, f)
	if err != nil {
		s.fail(w, err, "items")
		return
	}
	refs := make([]map[string]any, 0, len(list))
	for _, a := range list {
		refs = append(refs, map[string]any{"id": strconv.FormatInt(a.ID, 10), "directStreamIds": []string{}, "timestampUsec": usec(a.PublishedAt)})
	}
	out := map[string]any{"itemRefs": refs}
	if next != "" {
		out["continuation"] = next
	}
	writeJSON(w, http.StatusOK, out)
}

func usec(ts string) string {
	if t, err := time.Parse(time.RFC3339, ts); err == nil {
		return strconv.FormatInt(t.UnixMicro(), 10)
	}
	if t, err := time.Parse(time.RFC3339Nano, ts); err == nil {
		return strconv.FormatInt(t.UnixMicro(), 10)
	}
	return "0"
}

// grParseID reads an item id in any of its forms.
func grParseID(v string) int64 {
	v = strings.TrimSpace(v)
	if strings.HasPrefix(v, grItemPrefix) {
		n, _ := strconv.ParseInt(strings.TrimPrefix(v, grItemPrefix), 16, 64)
		return n
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil {
		return n
	}
	n, _ := strconv.ParseInt(v, 16, 64)
	return n
}

func (s *Server) grItems(ctx context.Context, uid int64, g *grFeeds, list []store.Article) []map[string]any {
	items := make([]map[string]any, 0, len(list))
	for _, a := range list {
		full, err := s.store.GetArticle(ctx, uid, a.ID)
		if err != nil {
			continue
		}
		cats := []string{grReadingList}
		origin := map[string]any{"streamId": "", "title": full.SiteName, "htmlUrl": ""}
		if full.FeedID != nil {
			if f, ok := g.byID[*full.FeedID]; ok {
				origin = map[string]any{"streamId": fmt.Sprintf("feed/%d", f.ID), "title": f.Name(), "htmlUrl": f.SiteURL}
				if f.FolderID != nil {
					cats = append(cats, grLabel+g.folders[*f.FolderID])
				}
			}
		}
		if full.Read {
			cats = append(cats, grRead)
		}
		if full.Starred {
			cats = append(cats, grStarred)
		}
		if full.Saved && !full.Archived {
			cats = append(cats, grLabel+grReadLater)
		}
		pub, _ := time.Parse(time.RFC3339, full.PublishedAt)
		body := full.Content
		if body == "" {
			body = "<p>" + full.Excerpt + "</p>"
		}
		it := map[string]any{
			"id": fmt.Sprintf("%s%016x", grItemPrefix, full.ID), "crawlTimeMsec": strconv.FormatInt(pub.UnixMilli(), 10), "timestampUsec": strconv.FormatInt(pub.UnixMicro(), 10),
			"published": pub.Unix(), "updated": pub.Unix(), "title": full.Title, "author": full.Author,
			"canonical": []map[string]string{{"href": full.URL}}, "alternate": []map[string]string{{"href": full.URL, "type": "text/html"}},
			"categories": cats, "origin": origin, "summary": map[string]string{"direction": "ltr", "content": body},
		}
		if full.MediaURL != "" {
			it["enclosure"] = []map[string]string{{"href": full.MediaURL, "type": full.MediaType}}
		} else if full.ImageURL != "" {
			it["enclosure"] = []map[string]string{{"href": full.ImageURL, "type": "image/jpeg"}}
		}
		items = append(items, it)
	}
	return items
}

func (s *Server) grItemContents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var ids []int64
	for _, v := range r.Form["i"] {
		if id := grParseID(v); id > 0 {
			ids = append(ids, id)
		}
	}
	g, _ := s.grLoad(ctx, uid)
	var list []store.Article
	if len(ids) > 0 {
		var err error
		list, _, err = s.store.ListArticles(ctx, uid, store.ArticleFilter{View: "", IDs: ids, Limit: 500})
		if err != nil {
			s.fail(w, err, "items")
			return
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{"id": grReadingList, "updated": time.Now().Unix(), "items": s.grItems(ctx, uid, g, list)})
}

func (s *Server) grStreamContents(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	stream := chi.URLParam(r, "*")
	if stream == "" {
		stream = r.Form.Get("s")
	}
	if u, err := url.PathUnescape(stream); err == nil {
		stream = u
	}
	f, ok := s.grFilter(ctx, uid, stream, r.Form)
	if !ok {
		writeJSON(w, http.StatusOK, map[string]any{"id": stream, "items": []any{}})
		return
	}
	list, next, err := s.store.ListArticles(ctx, uid, f)
	if err != nil {
		s.fail(w, err, "items")
		return
	}
	g, _ := s.grLoad(ctx, uid)
	out := map[string]any{"id": stream, "updated": time.Now().Unix(), "items": s.grItems(ctx, uid, g, list)}
	if next != "" {
		out["continuation"] = next
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *Server) grEditTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var ids []int64
	for _, v := range r.Form["i"] {
		if id := grParseID(v); id > 0 {
			ids = append(ids, id)
		}
	}
	t, f := true, false
	var st store.ArticleState
	apply := func(tag string, on bool) {
		v := &f
		if on {
			v = &t
		}
		switch tag {
		case grRead:
			st.Read = v
		case grKeptUnread:
			if on {
				st.Read = &f
			}
		case grStarred:
			st.Starred = v
		case grLabel + grReadLater:
			st.Saved = v
		}
	}
	for _, a := range r.Form["a"] {
		apply(a, true)
	}
	for _, rm := range r.Form["r"] {
		apply(rm, false)
	}
	if _, err := s.store.UpdateArticle(ctx, uid, ids, st); err != nil {
		grText(w, http.StatusInternalServerError, err.Error())
		return
	}
	grText(w, http.StatusOK, "OK")
}

func (s *Server) grMarkAllRead(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	stream := r.Form.Get("s")
	before := ""
	if ts, err := strconv.ParseInt(r.Form.Get("ts"), 10, 64); err == nil && ts > 0 {
		if ts > 1e15 {
			before = time.UnixMicro(ts).UTC().Format(time.RFC3339)
		} else {
			before = time.Unix(ts, 0).UTC().Format(time.RFC3339)
		}
	}
	var feedID, folderID int64
	switch {
	case strings.HasPrefix(stream, "feed/"):
		feedID = s.grFeedID(ctx, uid, stream)
		if feedID == 0 {
			grText(w, http.StatusNotFound, "no such feed")
			return
		}
	case strings.HasPrefix(stream, grLabel):
		folder, err := s.store.FolderByName(ctx, uid, strings.TrimPrefix(stream, grLabel), false)
		if err != nil {
			grText(w, http.StatusNotFound, "no such folder")
			return
		}
		folderID = folder.ID
	}
	if _, err := s.store.MarkAllRead(ctx, uid, feedID, folderID, before); err != nil {
		grText(w, http.StatusInternalServerError, err.Error())
		return
	}
	grText(w, http.StatusOK, "OK")
}

func (s *Server) grRenameTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	from := strings.TrimPrefix(r.Form.Get("s"), grLabel)
	to := strings.TrimSpace(strings.TrimPrefix(r.Form.Get("dest"), grLabel))
	f, err := s.store.FolderByName(ctx, uid, from, false)
	if err != nil || to == "" {
		grText(w, http.StatusNotFound, "no such folder")
		return
	}
	f.Name = to
	if err := s.store.UpdateFolder(ctx, uid, f); err != nil {
		grText(w, http.StatusConflict, err.Error())
		return
	}
	grText(w, http.StatusOK, "OK")
}

func (s *Server) grDisableTag(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	f, err := s.store.FolderByName(ctx, uid, strings.TrimPrefix(r.Form.Get("s"), grLabel), false)
	if err != nil {
		grText(w, http.StatusNotFound, "no such folder")
		return
	}
	_ = s.store.DeleteFolder(ctx, uid, f.ID)
	grText(w, http.StatusOK, "OK")
}
