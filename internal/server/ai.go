package server

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/ai"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// aiFor is the person's AI: Pocket AI through Core, their own
// OpenAI-compatible key, or nil with why.
func (s *Server) aiFor(ctx context.Context, uid int64, st store.Settings) (*ai.Client, string, string) {
	email := ""
	if u, err := s.store.GetUser(ctx, uid); err == nil {
		email = u.Email
	}
	pocket, status := s.core.SelectAI(ctx, email, st.AIProvider, st.OpenAIKey != "")
	if pocket != nil {
		if !status.Ready {
			return nil, "", "Pocket AI: " + status.Detail
		}
		c := ai.New("", "", "", 150*time.Second)
		c.Pocket = pocket.WithModel(st.AIPocketModel)
		return c, "pocket", ""
	}
	if st.OpenAIKey == "" {
		return nil, "", status.Detail
	}
	return ai.New(st.OpenAIBaseURL, st.OpenAIKey, st.OpenAIModel, 150*time.Second), "openai", ""
}

func (s *Server) handleAIStatus(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	st, _ := s.store.GetSettings(ctx, actorID(ctx))
	cl, used, why := s.aiFor(ctx, actorID(ctx), st)
	writeJSON(w, http.StatusOK, map[string]any{"ready": cl != nil, "provider": used, "detail": why})
}

func (s *Server) handleAIModels(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	c, status := s.core.SelectAI(ctx, userFrom(ctx).Email, "pocket", false)
	if c == nil || !status.Ready {
		writeErr(w, http.StatusServiceUnavailable, status.Detail)
		return
	}
	models, err := c.Models(ctx)
	if err != nil {
		writeErr(w, http.StatusBadGateway, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, models)
}

func validAIProvider(v string) bool { return core.ValidAIProvider(v) }

func validPocketModel(v string) bool { return core.ValidInferenceModel(v) }

const aiVoice = `You are a careful reading assistant inside someone's own feed reader. Use only the text given; never invent facts, quotes or numbers. Be concrete and brief. Plain language, no hype, no emojis, no exclamation marks. When the text is opinion, say whose.`

// articleText is an article as plain text for the model, cut to fit.
func articleText(a *store.Article, max int) string {
	t := store.PlainText(a.Content)
	if t == "" {
		t = a.Excerpt
	}
	if r := []rune(t); len(r) > max {
		t = string(r[:max]) + " …"
	}
	return t
}

func langLine(st store.Settings) string {
	if st.AILanguage != "" {
		return " Write in " + st.AILanguage + "."
	}
	return " Write in the article's language."
}

// handleSummarize writes (or returns the kept) summary of an article.
func (s *Server) handleSummarize(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	a, err := s.store.GetArticle(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "article")
		return
	}
	if a.AISummary != "" && !queryBool(r, "fresh") {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "text": a.AISummary, "kept": true})
		return
	}
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	text := articleText(a, 24000)
	if len(text) < 200 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "there is too little text to summarise; fetch the full article first"})
		return
	}
	prompt := "Summarise this article in 3–5 short bullet points (\"- \"), the most important first, then one line \"Why it matters:\" if it is clear from the text." + langLine(st) +
		"\n\nTitle: " + a.Title + "\nSource: " + firstNonEmpty(a.SiteName, a.FeedTitle) + "\n\n" + text
	out, err := cl.Complete(ctx, aiVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	out = strings.TrimSpace(out)
	_ = s.store.SetAISummary(ctx, uid, a.ID, out)
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "text": out})
}

// handleCatchUp sums up what is unread: a short briefing of the newest
// posts grouped by topic, each pointing at its post.
func (s *Server) handleCatchUp(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	var in struct {
		FeedID   int64 `json:"feed_id"`
		FolderID int64 `json:"folder_id"`
	}
	_ = decode(r, &in)
	posts, _, err := s.store.ListArticles(ctx, uid, store.ArticleFilter{View: "unread", FeedID: in.FeedID, FolderID: in.FolderID, Limit: 60})
	if err != nil {
		s.fail(w, err, "posts")
		return
	}
	if len(posts) == 0 {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": "nothing unread here"})
		return
	}
	var b strings.Builder
	for _, p := range posts {
		fmt.Fprintf(&b, "[%d] %s — %s: %s\n", p.ID, p.FeedTitle, p.Title, store.Excerpt(p.Excerpt, 280))
	}
	prompt := "These are someone's unread posts (id, feed, title, start of the text). Write a short briefing: group them into 2–6 topics, for each topic one or two sentences on what is new, and the ids of the posts it covers in square brackets like [12, 15]. Then one line \"Worth reading first:\" with up to three ids and why in a few words. Skip pure promotion." + langLine(st) + "\n\n" + b.String()
	out, err := cl.Complete(ctx, aiVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "text": strings.TrimSpace(out), "count": len(posts), "posts": posts})
}

// handleAsk answers a question about one article, or about what the person read lately.
func (s *Server) handleAsk(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	cl, used, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": why})
		return
	}
	var in struct {
		Question  string `json:"question"`
		ArticleID int64  `json:"article_id"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Question) == "" {
		writeErr(w, http.StatusBadRequest, "question is required")
		return
	}
	var ctxText string
	if in.ArticleID > 0 {
		a, err := s.store.GetArticle(ctx, uid, in.ArticleID)
		if err != nil {
			s.fail(w, err, "article")
			return
		}
		ctxText = "The article \"" + a.Title + "\" (" + firstNonEmpty(a.SiteName, a.FeedTitle) + "):\n" + articleText(a, 24000)
	} else {
		var b strings.Builder
		b.WriteString("What they read, saved and highlighted lately (newest first):\n")
		hist, _, _ := s.store.ListArticles(ctx, uid, store.ArticleFilter{View: "history", Limit: 40})
		saved, _, _ := s.store.ListArticles(ctx, uid, store.ArticleFilter{View: "saved", Limit: 30})
		for _, a := range append(hist, saved...) {
			fmt.Fprintf(&b, "- %s (%s): %s\n", a.Title, firstNonEmpty(a.SiteName, a.FeedTitle), store.Excerpt(a.Excerpt, 200))
		}
		hs, _ := s.store.ListHighlights(ctx, uid, store.HighlightFilter{Limit: 40})
		if len(hs) > 0 {
			b.WriteString("Highlights:\n")
			for _, h := range hs {
				fmt.Fprintf(&b, "- \"%s\" (%s)\n", store.Excerpt(h.Text, 240), h.ArticleTitle)
			}
		}
		ctxText = b.String()
	}
	prompt := "Question: " + strings.TrimSpace(in.Question) + "\n\n" + ctxText + "\n\nAnswer in a few sentences from the text above. If it does not say, say so."
	out, err := cl.Complete(ctx, aiVoice, prompt, false)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "provider": used, "text": strings.TrimSpace(out)})
}

// suggestTags asks the AI for up to three tags, preferring the person's own.
func (s *Server) suggestTags(ctx context.Context, uid int64, a *store.Article) ([]string, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	cl, _, why := s.aiFor(ctx, uid, st)
	if cl == nil {
		return nil, fmt.Errorf("%s", why)
	}
	have, _ := s.store.Tags(ctx, uid)
	var own []string
	for t := range have {
		own = append(own, t)
	}
	sort.Strings(own)
	if len(own) > 150 {
		own = own[:150]
	}
	prompt := "Suggest 1 to 3 short lowercase tags (one or two words each) for this article, so it can be found again by topic. Prefer these existing tags when they fit: " +
		strings.Join(own, ", ") + ".\nAnswer only with JSON: {\"tags\": [\"…\"]}\n\nTitle: " + a.Title + "\n\n" + articleText(a, 6000)
	out, err := cl.Complete(ctx, aiVoice, prompt, true)
	if err != nil {
		return nil, err
	}
	var res struct {
		Tags []string `json:"tags"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(out)), &res); err != nil {
		return nil, fmt.Errorf("the AI answered something else")
	}
	var tags []string
	for _, t := range res.Tags {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && len(t) <= 30 && len(tags) < 3 && !strings.ContainsAny(t, ",#") {
			tags = append(tags, t)
		}
	}
	return tags, nil
}

// autoTag tags a freshly saved page in the background.
func (s *Server) autoTag(uid, id int64) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	a, err := s.store.GetArticle(ctx, uid, id)
	if err != nil || a.Tags != "" {
		return
	}
	tags, err := s.suggestTags(ctx, uid, a)
	if err != nil || len(tags) == 0 {
		return
	}
	joined := strings.Join(tags, ", ")
	if cur, err := s.store.GetArticle(ctx, uid, id); err == nil && cur.Tags == "" {
		_, _ = s.store.UpdateArticle(ctx, uid, []int64{id}, store.ArticleState{Tags: &joined})
	}
}

func (s *Server) handleSuggestTags(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	a, err := s.store.GetArticle(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "article")
		return
	}
	tags, err := s.suggestTags(ctx, uid, a)
	if err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "tags": tags})
}
