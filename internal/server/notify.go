package server

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-reader/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// notifyNewPosts sends one push for a feed's new posts (the person asked for
// this feed, or a rule picked them).
func (s *Server) notifyNewPosts(ctx context.Context, f *store.Feed, st store.Settings, posts []store.Article) {
	if st.NotifyURL == "" || len(posts) == 0 {
		return
	}
	n := notify.New(st.NotifyURL, st.NotifyFormat)
	msg := notify.Message{Priority: "info", Tags: []string{"newspaper"}}
	if len(posts) == 1 {
		p := posts[0]
		msg.Title = f.Name()
		msg.Body = p.Title
		if p.Excerpt != "" {
			msg.Body += "\n" + store.Excerpt(p.Excerpt, 160)
		}
		msg.Link = s.cfg.BaseURL + fmt.Sprintf("/a/%d", p.ID)
	} else {
		msg.Title = fmt.Sprintf("%d new posts from %s", len(posts), f.Name())
		var lines []string
		for i, p := range posts {
			if i == 5 {
				lines = append(lines, fmt.Sprintf("…and %d more", len(posts)-5))
				break
			}
			lines = append(lines, "• "+p.Title)
		}
		msg.Body = strings.Join(lines, "\n")
		msg.Link = s.cfg.BaseURL + fmt.Sprintf("/feed/%d", f.ID)
	}
	if err := n.Send(ctx, msg); err != nil {
		s.log.Debug("notify failed", "err", err)
	}
}

// pushes runs every minute and sends the morning digest at each person's hour.
func (s *Server) pushes(ctx context.Context) {
	now := time.Now()
	if now.Minute() > 4 {
		return
	}
	ids, err := s.store.SettingsUsers(ctx)
	if err != nil {
		return
	}
	wd := int(now.Weekday())
	if wd == 0 {
		wd = 7
	}
	today := store.Today()
	for _, uid := range ids {
		st, err := s.store.GetSettings(ctx, uid)
		if err != nil || !st.DigestEnabled || st.NotifyURL == "" || now.Hour() != st.DigestHour || !strings.Contains(st.DigestDays, fmt.Sprint(wd)) {
			continue
		}
		key := fmt.Sprintf("push:%d:%s:digest", uid, today)
		if _, _, done := s.store.CacheGet(ctx, key); done {
			continue
		}
		_ = s.store.CachePut(ctx, key, "1")
		title, body := s.digest(ctx, uid)
		if body == "" {
			continue
		}
		_ = notify.New(st.NotifyURL, st.NotifyFormat).Send(ctx, notify.Message{Title: title, Body: body, Priority: "info", Tags: []string{"newspaper"}, Link: s.cfg.BaseURL + "/"})
	}
}

// digest is the morning push: what is waiting, by feed, and the read-later list.
func (s *Server) digest(ctx context.Context, uid int64) (string, string) {
	since := time.Now().Add(-24 * time.Hour).UTC().Format(time.RFC3339)
	posts, _, err := s.store.ListArticles(ctx, uid, store.ArticleFilter{View: "today", From: since, Limit: 300})
	if err != nil {
		return "", ""
	}
	byFeed := map[string]int{}
	var unread []store.Article
	for _, p := range posts {
		if !p.Read {
			byFeed[p.FeedTitle]++
			unread = append(unread, p)
		}
	}
	c, _ := s.store.FeedCountsFor(ctx, uid, since)
	if len(unread) == 0 && c.Saved == 0 {
		return "", ""
	}
	type kv struct {
		k string
		v int
	}
	var top []kv
	for k, v := range byFeed {
		top = append(top, kv{k, v})
	}
	sort.Slice(top, func(i, j int) bool { return top[i].v > top[j].v || (top[i].v == top[j].v && top[i].k < top[j].k) })
	var b strings.Builder
	if len(unread) > 0 {
		var parts []string
		for i, t := range top {
			if i == 4 {
				parts = append(parts, fmt.Sprintf("%d more feeds", len(top)-4))
				break
			}
			parts = append(parts, fmt.Sprintf("%s %d", t.k, t.v))
		}
		fmt.Fprintf(&b, "%d new since yesterday: %s.\n", len(unread), strings.Join(parts, ", "))
		for i, p := range unread {
			if i == 3 {
				break
			}
			fmt.Fprintf(&b, "• %s\n", p.Title)
		}
	}
	if c.Saved > 0 {
		fmt.Fprintf(&b, "%d waiting on your read-later list.", c.Saved)
	}
	return "Your reading this morning", strings.TrimSpace(b.String())
}

// handleDigestNow sends (or shows) the digest at once.
func (s *Server) handleDigestNow(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	st, _ := s.store.GetSettings(ctx, uid)
	title, body := s.digest(ctx, uid)
	if body == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sent": false, "report": "Nothing new since yesterday."})
		return
	}
	if st.NotifyURL == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sent": false, "report": body})
		return
	}
	if err := notify.New(st.NotifyURL, st.NotifyFormat).Send(ctx, notify.Message{Title: title, Body: body, Priority: "info", Tags: []string{"newspaper"}, Link: s.cfg.BaseURL + "/"}); err != nil {
		writeJSON(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "sent": true, "report": body})
}
