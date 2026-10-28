package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-reader/internal/pocket"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// ---- highlights ----

var colors = map[string]bool{"yellow": true, "green": true, "blue": true, "pink": true}

func (s *Server) handleListHighlights(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	hs, err := s.store.ListHighlights(ctx, actorID(ctx), store.HighlightFilter{Query: r.URL.Query().Get("q"), Limit: queryInt(r, "limit"), Offset: queryInt(r, "offset")})
	if err != nil {
		s.fail(w, err, "highlights")
		return
	}
	writeJSON(w, http.StatusOK, hs)
}

func (s *Server) handleCreateHighlight(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in store.Highlight
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Text = strings.TrimSpace(in.Text)
	if in.Text == "" || len(in.Text) > 20000 {
		writeErr(w, http.StatusBadRequest, "select some text to highlight")
		return
	}
	if !colors[in.Color] {
		in.Color = "yellow"
	}
	in.ArticleID = idParam(r, "id")
	in.Prefix, in.Suffix = clip(in.Prefix, 64, true), clip(in.Suffix, 64, false)
	if err := s.store.CreateHighlight(ctx, uid, &in); err != nil {
		s.fail(w, err, "highlight")
		return
	}
	s.publish(ctx, uid, "reader.highlighted", in.ArticleURL, map[string]any{"highlight_id": in.ID, "article_id": in.ArticleID, "title": in.ArticleTitle, "url": in.ArticleURL, "text": in.Text})
	s.notesSoon(uid, in.ArticleID)
	writeJSON(w, http.StatusCreated, in)
}

func clip(s string, n int, tail bool) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	if tail {
		return string(r[len(r)-n:])
	}
	return string(r[:n])
}

func (s *Server) handleUpdateHighlight(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	h, err := s.store.GetHighlight(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "highlight")
		return
	}
	var in store.Highlight
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["note"] {
		h.Note = strings.TrimSpace(in.Note)
	}
	if keys["color"] && colors[in.Color] {
		h.Color = in.Color
	}
	if err := s.store.UpdateHighlight(ctx, uid, h); err != nil {
		s.fail(w, err, "highlight")
		return
	}
	s.notesSoon(uid, h.ArticleID)
	got, _ := s.store.GetHighlight(ctx, uid, h.ID)
	writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleDeleteHighlight(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	h, err := s.store.GetHighlight(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "highlight")
		return
	}
	if err := s.store.DeleteHighlight(ctx, uid, h.ID); err != nil {
		s.fail(w, err, "highlight")
		return
	}
	s.notesSoon(uid, h.ArticleID)
	w.WriteHeader(http.StatusNoContent)
}

// handleHighlightsMarkdown exports every highlight as Markdown, grouped by article.
func (s *Server) handleHighlightsMarkdown(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	hs, err := s.store.ListHighlights(ctx, uid, store.HighlightFilter{Limit: 1000})
	if err != nil {
		s.fail(w, err, "highlights")
		return
	}
	var b strings.Builder
	b.WriteString("# Highlights\n")
	seen := map[int64]bool{}
	for _, h := range hs {
		if seen[h.ArticleID] {
			continue
		}
		seen[h.ArticleID] = true
		a, err := s.store.GetArticle(ctx, uid, h.ArticleID)
		if err != nil {
			continue
		}
		list, _ := s.store.ListHighlights(ctx, uid, store.HighlightFilter{ArticleID: a.ID})
		b.WriteString("\n" + notesMarkdown(a, list, false))
	}
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-reader-highlights.md"`)
	_, _ = w.Write([]byte(b.String()))
}

// notesMarkdown is an article's highlights as a note.
func notesMarkdown(a *store.Article, hs []store.Highlight, withHeader bool) string {
	var b strings.Builder
	if !withHeader {
		fmt.Fprintf(&b, "## %s\n\n", a.Title)
	}
	src := a.SiteName
	if a.Author != "" {
		src = a.Author + ", " + src
	}
	fmt.Fprintf(&b, "Source: [%s](%s)", strings.TrimSpace(firstNonEmpty(src, a.URL)), a.URL)
	if a.Tags != "" {
		var tags []string
		for _, t := range store.SplitTags(a.Tags) {
			tags = append(tags, "#"+strings.ReplaceAll(t, " ", "-"))
		}
		fmt.Fprintf(&b, " · %s", strings.Join(tags, " "))
	}
	b.WriteString("\n\n")
	if a.Note != "" {
		b.WriteString(a.Note + "\n\n")
	}
	for _, h := range hs {
		for _, line := range strings.Split(strings.TrimSpace(h.Text), "\n") {
			b.WriteString("> " + strings.TrimSpace(line) + "\n")
		}
		if h.Note != "" {
			b.WriteString("\n" + h.Note + "\n")
		}
		b.WriteString("\n")
	}
	return b.String()
}

// ---- Pocket Notes ----

// notesSoon sends an article's highlights to Pocket Notes in the background
// when the person chose to keep them there automatically.
func (s *Server) notesSoon(uid, articleID int64) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil || !st.NotesAuto {
			return
		}
		if _, err := s.sendToNotes(ctx, uid, articleID); err != nil && !isNotConnected(err) {
			s.log.Debug("sending highlights to Pocket Notes failed", "article", articleID, "err", err)
		}
	}()
}

// sendToNotes writes (or rewrites) the note with an article's highlights.
func (s *Server) sendToNotes(ctx context.Context, uid, articleID int64) (*pocket.Note, error) {
	s.notesMu.Lock()
	defer s.notesMu.Unlock()
	a, err := s.store.GetArticle(ctx, uid, articleID)
	if err != nil {
		return nil, err
	}
	hs, err := s.store.ListHighlights(ctx, uid, store.HighlightFilter{ArticleID: a.ID})
	if err != nil {
		return nil, err
	}
	if len(hs) == 0 && a.NotesNoteID == 0 {
		return nil, errors.New("nothing highlighted in this article yet")
	}
	cl := s.clients(ctx, uid)
	conn, err := cl.conn(ctx, appNotes, scopeNotesWrite)
	if err != nil {
		return nil, err
	}
	body := notesMarkdown(a, hs, true)
	if a.NotesNoteID > 0 {
		n, err := conn.client.UpdateNote(ctx, a.NotesNoteID, body)
		if err == nil {
			_ = s.store.SetNotesNote(ctx, uid, a.ID, n.ID)
			return n, nil
		}
		if !errors.Is(err, pocket.ErrNotFound) {
			return nil, err
		}
	}
	n, err := conn.client.CreateNote(ctx, a.Title, cl.st.NotesFolder, body)
	if err != nil {
		return nil, err
	}
	_ = s.store.SetNotesNote(ctx, uid, a.ID, n.ID)
	return n, nil
}

func (s *Server) handleSendToNotes(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	n, err := s.sendToNotes(ctx, actorID(ctx), idParam(r, "id"))
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			s.fail(w, err, "article")
			return
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "note_id": n.ID, "title": n.Title})
}

// ---- rules ----

var ruleFields = map[string]bool{"title": true, "any": true, "author": true, "url": true}
var ruleActions = map[string]bool{"read": true, "star": true, "save": true, "tag": true, "notify": true}

func (s *Server) checkRule(ctx context.Context, uid int64, r *store.Rule) string {
	r.Words = strings.TrimSpace(r.Words)
	r.Name = strings.TrimSpace(r.Name)
	if r.Words == "" {
		return "a rule needs words to look for"
	}
	if r.Field == "" {
		r.Field = "title"
	}
	if !ruleFields[r.Field] {
		return "look in the title, the author, the address or everything"
	}
	if !ruleActions[r.Action] {
		return "a rule marks read, stars, saves, tags or notifies"
	}
	if r.Action == "tag" && strings.TrimSpace(r.Tag) == "" {
		return "which tag?"
	}
	r.Tag = strings.ToLower(strings.TrimSpace(r.Tag))
	if r.FeedID != nil && *r.FeedID > 0 {
		if _, err := s.store.GetFeed(ctx, uid, *r.FeedID); err != nil {
			return "no such feed"
		}
	} else {
		r.FeedID = nil
	}
	if r.FolderID != nil && *r.FolderID > 0 {
		if _, err := s.store.GetFolder(ctx, uid, *r.FolderID); err != nil {
			return "no such folder"
		}
	} else {
		r.FolderID = nil
	}
	return ""
}

func (s *Server) handleListRules(w http.ResponseWriter, r *http.Request) {
	rules, err := s.store.ListRules(r.Context(), actorID(r.Context()))
	if err != nil {
		s.fail(w, err, "rules")
		return
	}
	writeJSON(w, http.StatusOK, rules)
}

func (s *Server) handleCreateRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in store.Rule
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Enabled = true
	if msg := s.checkRule(ctx, uid, &in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.CreateRule(ctx, uid, &in); err != nil {
		s.fail(w, err, "rule")
		return
	}
	writeJSON(w, http.StatusCreated, in)
}

func (s *Server) handleUpdateRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in store.Rule
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.ID = idParam(r, "id")
	if msg := s.checkRule(ctx, uid, &in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	if err := s.store.UpdateRule(ctx, uid, &in); err != nil {
		s.fail(w, err, "rule")
		return
	}
	writeJSON(w, http.StatusOK, in)
}

func (s *Server) handleDeleteRule(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteRule(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "rule")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleTestRule shows which of the last posts a rule would have picked.
func (s *Server) handleTestRule(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in store.Rule
	if err := decode(r, &in); err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	in.Enabled = true
	if in.Action == "" {
		in.Action = "read"
	}
	if msg := s.checkRule(ctx, uid, &in); msg != "" {
		writeErr(w, http.StatusBadRequest, msg)
		return
	}
	posts, _, err := s.store.ListArticles(ctx, uid, store.ArticleFilter{FeedOnly: true, Limit: 500})
	if err != nil {
		s.fail(w, err, "posts")
		return
	}
	feeds := map[int64]*store.Feed{}
	var hits []map[string]any
	n := 0
	for _, p := range posts {
		if p.FeedID == nil {
			continue
		}
		f := feeds[*p.FeedID]
		if f == nil {
			f, _ = s.store.GetFeed(ctx, uid, *p.FeedID)
			if f == nil {
				continue
			}
			feeds[*p.FeedID] = f
		}
		if in.Field == "any" {
			full, err := s.store.GetArticle(ctx, uid, p.ID)
			if err == nil {
				p.Content = full.Content
			}
		}
		if ruleMatches(in, f, &p) {
			n++
			if len(hits) < 8 {
				hits = append(hits, map[string]any{"id": p.ID, "title": p.Title, "feed": f.Name(), "published_at": p.PublishedAt})
			}
		}
	}
	if hits == nil {
		hits = []map[string]any{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"matches": n, "of": len(posts), "examples": hits})
}
