package server

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/abjelosevic88/pocket-homelab-reader/internal/feed"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/notify"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// ---- checking feeds ----
//
// Every feed has its own next check (next_fetch_at): the person's interval
// after a success, longer and longer after failures (up to a day). The
// refresher looks for due feeds every 30 seconds, or at once when kicked
// (a feed was added, someone pressed Refresh), and checks a few at a time.

// kickRefresh wakes the refresher without waiting.
func (s *Server) kickRefresh() {
	select {
	case s.kick <- struct{}{}:
	default:
	}
}

func (s *Server) refresher(ctx context.Context) {
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	s.refreshDue(ctx)
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		case <-s.kick:
		}
		s.refreshDue(ctx)
	}
}

// refreshDue checks every due feed, a few in parallel.
func (s *Server) refreshDue(ctx context.Context) {
	if !s.fetchMu.TryLock() {
		return
	}
	defer s.fetchMu.Unlock()
	for round := 0; round < 20; round++ {
		due, err := s.store.DueFeeds(ctx, 40)
		if err != nil || len(due) == 0 {
			return
		}
		settings := map[int64]store.Settings{}
		for _, f := range due {
			if _, ok := settings[f.UserID]; !ok {
				settings[f.UserID], _ = s.store.GetSettings(ctx, f.UserID)
			}
		}
		jobs := make(chan store.Feed)
		var wg sync.WaitGroup
		for i := 0; i < s.cfg.Workers; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				for f := range jobs {
					c, cancel := context.WithTimeout(ctx, 90*time.Second)
					_, _ = s.fetchFeed(c, &f, settings[f.UserID])
					cancel()
				}
			}()
		}
		for _, f := range due {
			select {
			case jobs <- f:
			case <-ctx.Done():
			}
		}
		close(jobs)
		wg.Wait()
		if ctx.Err() != nil || len(due) < 40 {
			return
		}
	}
}

func (s *Server) lockFeed(id int64) *sync.Mutex {
	s.feedMuMu.Lock()
	defer s.feedMuMu.Unlock()
	m := s.feedMu[id]
	if m == nil {
		m = &sync.Mutex{}
		s.feedMu[id] = m
	}
	return m
}

// interval is how long a feed waits between checks.
func interval(f *store.Feed, st store.Settings) time.Duration {
	m := f.RefreshMin
	if m <= 0 {
		m = st.RefreshMin
	}
	if m < 10 {
		m = 10
	}
	return time.Duration(m) * time.Minute
}

// backoff is the wait after the n-th failure in a row: the interval doubled
// each time, at most a day.
func backoff(base time.Duration, n int) time.Duration {
	d := time.Duration(float64(base) * math.Pow(2, float64(min(n, 6))))
	if d > 24*time.Hour {
		d = 24 * time.Hour
	}
	return d
}

// firstFetchUnread is how many posts of a newly added feed stay unread; the
// older ones arrive already read, so a new feed does not bury the list.
const firstFetchUnread = 10

// fetchResult says what one check of a feed found.
type fetchResult struct {
	New      int    `json:"new"`
	Error    string `json:"error,omitempty"`
	Modified bool   `json:"modified"`
}

// fetchFeed checks one feed now and stores its new posts.
func (s *Server) fetchFeed(ctx context.Context, f *store.Feed, st store.Settings) (*fetchResult, error) {
	mu := s.lockFeed(f.ID)
	mu.Lock()
	defer mu.Unlock()
	every := interval(f, st)
	res := &fetchResult{}
	first := f.FetchedAt == ""
	r, err := s.fetcher.Get(ctx, f.URL, "application/rss+xml, application/atom+xml, application/feed+json, application/xml;q=0.9, text/xml;q=0.9, */*;q=0.5", f.ETag, f.LastModified, 12<<20)
	if errors.Is(err, feed.ErrNotModified) {
		return res, s.store.FeedFetched(ctx, f, time.Now().Add(jitter(every)))
	}
	if err == nil && !feed.LooksLikeFeed(r) {
		err = errors.New("the address no longer sends a feed (it is a web page now)")
	}
	var pf *feed.Feed
	if err == nil {
		pf, err = feed.Parse(r.Body, r.URL)
		if err != nil {
			err = errors.New("the feed could not be read (it is broken or not a feed)")
		}
	}
	if err != nil {
		res.Error = err.Error()
		s.feedFailed(ctx, f, st, err.Error(), every)
		return res, err
	}
	res.Modified = true
	f.ETag, f.LastModified = r.ETag, r.LastModified
	f.Title, f.SiteURL, f.Description, f.Kind = pf.Title, pf.SiteURL, pf.Description, pf.Kind
	if f.IconURL == "" {
		f.IconURL = pf.IconURL
		if f.IconURL == "" && pf.SiteURL != "" {
			f.IconURL = faviconFor(pf.SiteURL)
		}
	}
	rules, _ := s.store.ListRules(ctx, f.UserID)
	var fresh []store.Article
	items := pf.Items
	if len(items) > 200 {
		items = items[:200]
	}
	for i, it := range items {
		if s.store.HasPost(ctx, f.UserID, f.ID, it.GUID) {
			continue
		}
		fid := f.ID
		a := store.Article{FeedID: &fid, GUID: it.GUID, URL: it.URL, Title: it.Title, Author: it.Author, Content: it.Content, Excerpt: it.Excerpt,
			ImageURL: it.ImageURL, MediaURL: it.MediaURL, MediaType: it.MediaType, PublishedAt: it.Published.UTC().Format(time.RFC3339),
			Tags: strings.Join(it.Tags, ", ")}
		if first && i >= firstFetchUnread {
			a.Read = true
		}
		if f.FullText && it.URL != "" && !a.Read && f.Kind != "youtube" {
			a.ExtractState = "pending"
		}
		notifyMe := applyRules(ctx, s, rules, f, &a)
		ok, err := s.store.InsertPost(ctx, f.UserID, &a)
		if err != nil {
			s.log.Warn("storing a post failed", "feed", f.ID, "err", err)
			continue
		}
		if !ok {
			continue
		}
		if a.PublishedAt > f.NewestAt {
			f.NewestAt = a.PublishedAt
		}
		res.New++
		if !a.Read && (notifyMe || (f.Notify && st.NotifyNewPosts)) && !first {
			fresh = append(fresh, a)
		}
	}
	if err := s.store.FeedFetched(ctx, f, time.Now().Add(jitter(every))); err != nil {
		return res, err
	}
	if res.New > 0 {
		s.log.Debug("feed checked", "feed", f.ID, "new", res.New)
	}
	if len(fresh) > 0 {
		s.notifyNewPosts(ctx, f, st, fresh)
	}
	if f.FullText && res.New > 0 {
		go func() {
			c, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
			defer cancel()
			s.extractPending(c)
		}()
	}
	return res, nil
}

// jitter spreads checks so a hundred feeds added at once do not stay in step.
func jitter(d time.Duration) time.Duration {
	n := time.Now().UnixNano() % int64(d/10+1)
	return d + time.Duration(n)
}

func (s *Server) feedFailed(ctx context.Context, f *store.Feed, st store.Settings, msg string, every time.Duration) {
	_ = s.store.FeedFailed(ctx, f.ID, msg, time.Now().Add(backoff(every, f.ErrorCount)))
	// one push when a feed has failed for a day of tries, not on every blip
	if st.NotifyFeedError && st.NotifyURL != "" && f.ErrorCount+1 == 6 {
		n := notify.New(st.NotifyURL, st.NotifyFormat)
		_ = n.Send(ctx, notify.Message{Title: "A feed stopped working", Body: fmt.Sprintf("%s: %s. Pocket Reader keeps trying once a day.", f.Name(), msg),
			Priority: "warning", Tags: []string{"warning"}, Link: s.cfg.BaseURL + "/feeds"})
	}
}

func faviconFor(site string) string {
	u, err := feed.CheckURL(site)
	if err != nil {
		return ""
	}
	return u.Scheme + "://" + u.Host + "/favicon.ico"
}

// ---- rules ----

// ruleMatches reports whether a rule picks a new post.
func ruleMatches(r store.Rule, f *store.Feed, a *store.Article) bool {
	if !r.Enabled {
		return false
	}
	if r.FeedID != nil && *r.FeedID != f.ID {
		return false
	}
	if r.FolderID != nil && (f.FolderID == nil || *f.FolderID != *r.FolderID) {
		return false
	}
	var hay string
	switch r.Field {
	case "author":
		hay = a.Author
	case "url":
		hay = a.URL
	case "any":
		hay = a.Title + " " + a.Author + " " + a.Tags + " " + store.PlainText(a.Content) + " " + a.Excerpt
	default:
		hay = a.Title
	}
	hay = strings.ToLower(hay)
	for _, w := range strings.Split(r.Words, ",") {
		if w = strings.ToLower(strings.TrimSpace(w)); w != "" && strings.Contains(hay, w) {
			return true
		}
	}
	return false
}

// applyRules runs a person's rules on a new post; true = notify about it.
func applyRules(ctx context.Context, s *Server, rules []store.Rule, f *store.Feed, a *store.Article) bool {
	notifyMe := false
	for _, r := range rules {
		if !ruleMatches(r, f, a) {
			continue
		}
		switch r.Action {
		case "read":
			a.Read = true
		case "star":
			a.Starred = true
		case "save":
			a.Saved = true
		case "tag":
			if r.Tag != "" {
				a.Tags = store.JoinTags(a.Tags + "," + r.Tag)
			}
		case "notify":
			notifyMe = true
		}
		if s != nil {
			s.store.RuleHit(ctx, r.ID, 1)
		}
	}
	return notifyMe && !a.Read
}

// ---- full text ----

// extractPending fetches the whole article for posts and pages waiting for it.
func (s *Server) extractPending(ctx context.Context) {
	for round := 0; round < 10; round++ {
		list, err := s.store.PendingExtracts(ctx, 20)
		if err != nil || len(list) == 0 {
			return
		}
		for _, a := range list {
			if ctx.Err() != nil {
				return
			}
			s.extractOne(ctx, a.UserID, a.ID, a.URL)
		}
	}
}

// extractOne fetches one article's page and keeps its readable part.
func (s *Server) extractOne(ctx context.Context, uid, id int64, url string) {
	c, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	x, err := s.fetcher.Extract(c, url)
	if err != nil {
		msg := err.Error()
		if errors.Is(err, feed.ErrNoArticle) {
			msg = "no readable article on that page; open the original"
		}
		_ = s.store.SetExtracted(ctx, uid, id, nil, "failed", msg)
		return
	}
	_ = s.store.SetExtracted(ctx, uid, id, articleFrom(x), "done", "")
}

func articleFrom(x *feed.Article) *store.Article {
	a := &store.Article{URL: x.URL, Title: x.Title, Author: x.Author, SiteName: x.SiteName, Excerpt: x.Excerpt, Content: x.Content, ImageURL: x.ImageURL, Lang: x.Lang}
	if !x.Published.IsZero() {
		a.PublishedAt = x.Published.UTC().Format(time.RFC3339)
	}
	return a
}
