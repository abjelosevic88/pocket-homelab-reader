package server

import (
	"context"
	"errors"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-reader/internal/feed"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// ---- articles ----

var views = map[string]bool{"unread": true, "all": true, "today": true, "saved": true, "archive": true, "starred": true, "history": true, "highlights": true}

// handleListArticles is every list in the app: unread, a feed, a folder,
// read later, starred, history, a tag, a search.
func (s *Server) handleListArticles(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	q := r.URL.Query()
	st, _ := s.store.GetSettings(ctx, uid)
	f := store.ArticleFilter{View: q.Get("view"), Tag: q.Get("tag"), Query: q.Get("q"), Limit: queryInt(r, "limit")}
	if !views[f.View] {
		f.View = "unread"
	}
	if f.View == "all" {
		f.View = ""
	}
	if f.Query != "" && q.Get("view") == "" {
		f.View = ""
	}
	f.FeedID, _ = strconv.ParseInt(q.Get("feed"), 10, 64)
	f.FolderID, _ = strconv.ParseInt(q.Get("folder"), 10, 64)
	if (f.FeedID > 0 || f.FolderID > 0) && q.Get("view") == "" {
		f.View = "unread"
		if !st.HideRead {
			f.View = ""
		}
	}
	f.Oldest = st.SortOldest
	if v := q.Get("oldest"); v != "" {
		f.Oldest = queryBool(r, "oldest")
	}
	if c := q.Get("cursor"); c != "" {
		f.Before, f.BeforeID = store.ParseCursor(c)
	}
	f.From, f.To = q.Get("from"), q.Get("to")
	if ids := q.Get("ids"); ids != "" {
		for _, p := range strings.Split(ids, ",") {
			if id, err := strconv.ParseInt(strings.TrimSpace(p), 10, 64); err == nil {
				f.IDs = append(f.IDs, id)
			}
		}
	}
	items, next, err := s.store.ListArticles(ctx, uid, f)
	if err != nil {
		s.fail(w, err, "articles")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": items, "next": next})
}

// articleView is an article with what the reader needs around it.
type articleView struct {
	*store.Article
	Minutes    int               `json:"minutes"`
	Feed       *store.Feed       `json:"feed,omitempty"`
	HighlightL []store.Highlight `json:"highlight_list"`
}

func (s *Server) handleGetArticle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	a, err := s.store.GetArticle(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "article")
		return
	}
	v := articleView{Article: a, Minutes: a.Minutes()}
	if a.FeedID != nil {
		v.Feed, _ = s.store.GetFeed(ctx, uid, *a.FeedID)
	}
	v.HighlightL, _ = s.store.ListHighlights(ctx, uid, store.HighlightFilter{ArticleID: a.ID})
	// a link token reads articles, not the person's private notes on them
	if linkApp(ctx) != "" {
		a.Note = ""
	}
	writeJSON(w, http.StatusOK, v)
}

// handleUpdateArticle changes one article: read, starred, saved, archived,
// progress, tags, note, title.
func (s *Server) handleUpdateArticle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	id := idParam(r, "id")
	before, err := s.store.GetArticle(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "article")
		return
	}
	var in store.ArticleState
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if _, err := s.store.UpdateArticle(ctx, uid, []int64{id}, in); err != nil {
		s.fail(w, err, "save")
		return
	}
	after, err := s.store.GetArticle(ctx, uid, id)
	if err != nil {
		s.fail(w, err, "article")
		return
	}
	s.afterChange(ctx, uid, before, after)
	after.Content = ""
	writeJSON(w, http.StatusOK, after)
}

// afterChange publishes what other apps may care about.
func (s *Server) afterChange(ctx context.Context, uid int64, before, after *store.Article) {
	finished := (after.Progress >= 0.9 && before.Progress < 0.9) || (after.Archived && !before.Archived)
	if finished && !strings.HasPrefix(after.Source, "demo") {
		s.publish(ctx, uid, "reader.finished", after.URL, map[string]any{"article_id": after.ID, "title": after.Title, "url": after.URL,
			"site": after.SiteName, "minutes": after.Minutes(), "saved": after.Saved})
	}
	if after.Saved && !before.Saved {
		s.publish(ctx, uid, "reader.saved", after.URL, map[string]any{"article_id": after.ID, "title": after.Title, "url": after.URL, "site": after.SiteName})
	}
}

// handleBulkState changes many articles at once ("mark these read").
func (s *Server) handleBulkState(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		IDs []int64 `json:"ids"`
		store.ArticleState
	}
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if len(in.IDs) > 1000 {
		writeErr(w, http.StatusBadRequest, "at most 1000 at a time")
		return
	}
	n, err := s.store.UpdateArticle(ctx, actorID(ctx), in.IDs, in.ArticleState)
	if err != nil {
		s.fail(w, err, "save")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": n})
}

// handleReadAll marks a list read: everything, a feed or a folder.
func (s *Server) handleReadAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	var in struct {
		FeedID   int64  `json:"feed_id"`
		FolderID int64  `json:"folder_id"`
		Before   string `json:"before"`
	}
	_ = decode(r, &in)
	n, err := s.store.MarkAllRead(ctx, actorID(ctx), in.FeedID, in.FolderID, in.Before)
	if err != nil {
		s.fail(w, err, "mark read")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"changed": n})
}

func (s *Server) handleDeleteArticle(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	id := idParam(r, "id")
	if app := linkApp(ctx); app != "" {
		a, err := s.store.GetArticle(ctx, uid, id)
		if err != nil {
			s.fail(w, err, "article")
			return
		}
		if a.Source != app {
			writeErr(w, http.StatusForbidden, "this Pocket Core link may only remove what "+app+" saved")
			return
		}
	}
	if err := s.store.DeleteArticle(ctx, uid, id); err != nil {
		s.fail(w, err, "delete")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleExtract fetches an article's full text again, now.
func (s *Server) handleExtract(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	a, err := s.store.GetArticle(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "article")
		return
	}
	if a.URL == "" {
		writeErr(w, http.StatusBadRequest, "this article has no address")
		return
	}
	s.extractOne(ctx, uid, a.ID, a.URL)
	got, err := s.store.GetArticle(ctx, uid, a.ID)
	if err != nil {
		s.fail(w, err, "article")
		return
	}
	writeJSON(w, http.StatusOK, articleView{Article: got, Minutes: got.Minutes()})
}

// saveInput saves a page to read later.
type saveInput struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Tags  string `json:"tags"`
	Note  string `json:"note"`
	HTML  string `json:"html"` // the page as the browser has it (logged-in pages, paywalls the person passed)
}

// handleSave puts a page on the read-later list, with its readable article.
func (s *Server) handleSave(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in saveInput
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	src := firstNonEmpty(linkApp(ctx), r.URL.Query().Get("source"), "app")
	if src != "app" && src != "bookmarklet" && src != "share" && linkApp(ctx) == "" {
		src = "app"
	}
	a, created, err := s.savePage(ctx, uid, in, src)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	status := http.StatusOK
	if created {
		status = http.StatusCreated
	}
	writeJSON(w, status, map[string]any{"article": a, "created": created})
}

// savePage fetches the page (or reads the HTML given) and saves it.
func (s *Server) savePage(ctx context.Context, uid int64, in saveInput, source string) (*store.Article, bool, error) {
	u, err := feed.CheckURL(in.URL)
	if err != nil {
		return nil, false, err
	}
	in.URL = u.String()
	if have, err := s.store.ArticleByURL(ctx, uid, in.URL); err == nil {
		before := *have
		a, _, err := s.store.SavePage(ctx, uid, &store.Article{URL: in.URL, Tags: in.Tags})
		if err == nil {
			s.afterChange(ctx, uid, &before, a)
		}
		return a, false, err
	}
	st, _ := s.store.GetSettings(ctx, uid)
	a := &store.Article{URL: in.URL, Title: strings.TrimSpace(in.Title), Tags: in.Tags, Note: in.Note, Source: source}
	var x *feed.Article
	if in.HTML != "" {
		x, err = feed.ExtractPage([]byte(in.HTML), in.URL)
	} else if st.AutoExtract {
		c, cancel := context.WithTimeout(ctx, 30*time.Second)
		x, err = s.fetcher.Extract(c, in.URL)
		cancel()
	}
	switch {
	case x != nil && err == nil:
		got := articleFrom(x)
		a.Content, a.Author, a.SiteName, a.ImageURL, a.Lang, a.Excerpt = got.Content, got.Author, got.SiteName, got.ImageURL, got.Lang, got.Excerpt
		if a.Title == "" {
			a.Title = got.Title
		}
		if got.PublishedAt != "" {
			a.PublishedAt = got.PublishedAt
		}
		a.ExtractState = "done"
	case x != nil:
		// no readable article, but the page said what it is
		a.Excerpt, a.SiteName, a.ImageURL = x.Excerpt, x.SiteName, x.ImageURL
		if a.Title == "" {
			a.Title = x.Title
		}
		a.ExtractState = "failed"
	case err != nil:
		a.ExtractState = "pending" // try again in the background
	}
	if a.Title == "" {
		a.Title = strings.TrimPrefix(u.Hostname(), "www.") + u.Path
	}
	if a.SiteName == "" {
		a.SiteName = strings.TrimPrefix(u.Hostname(), "www.")
	}
	saved, created, err := s.store.SavePage(ctx, uid, a)
	if err != nil {
		return nil, false, err
	}
	if created {
		s.publish(ctx, uid, "reader.saved", saved.URL, map[string]any{"article_id": saved.ID, "title": saved.Title, "url": saved.URL, "site": saved.SiteName, "source": source})
		if st.AIAutoTag && saved.Tags == "" && saved.ExtractState == "done" {
			go s.autoTag(uid, saved.ID)
		}
	}
	return saved, created, nil
}

// handleTags lists a person's tags with counts.
func (s *Server) handleTags(w http.ResponseWriter, r *http.Request) {
	tags, err := s.store.Tags(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "tags")
		return
	}
	type tc struct {
		Tag   string `json:"tag"`
		Count int    `json:"count"`
	}
	out := []tc{}
	for k, v := range tags {
		out = append(out, tc{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Count > out[j].Count || (out[i].Count == out[j].Count && out[i].Tag < out[j].Tag)
	})
	writeJSON(w, http.StatusOK, out)
}

// startOfToday is local midnight in UTC, as stored.
func startOfToday() string {
	n := time.Now()
	return time.Date(n.Year(), n.Month(), n.Day(), 0, 0, 0, 0, n.Location()).UTC().Format(time.RFC3339)
}

// handleDashboard is the sidebar's numbers (and a Homepage widget).
func (s *Server) handleDashboard(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	c, err := s.store.FeedCountsFor(ctx, uid, startOfToday())
	if err != nil {
		s.fail(w, err, "dashboard")
		return
	}
	writeJSON(w, http.StatusOK, c)
}

// handleDay is one day's reading, what Pocket Day writes into the journal.
func (s *Server) handleDay(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	date := chiParam(r, "date")
	t, err := time.ParseInLocation("2006-01-02", date, time.Local)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "the date is YYYY-MM-DD")
		return
	}
	from, to := t.UTC().Format(time.RFC3339), t.AddDate(0, 0, 1).UTC().Format(time.RFC3339)
	d, err := s.store.Day(ctx, actorID(ctx), date, from, to)
	if err != nil {
		s.fail(w, err, "day")
		return
	}
	if linkApp(ctx) != "" {
		for i := range d.Read {
			d.Read[i].Note = ""
		}
		for i := range d.Saved {
			d.Saved[i].Note = ""
		}
	}
	writeJSON(w, http.StatusOK, d)
}

var errNoArticle = errors.New("no such article")
