package server

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/feed"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// What Pocket Search, Pocket Day and Pocket Hub ask Reader (the capabilities
// value in links.go): one route each, in the shape every Pocket app answers
// in. They read what /articles, /day and /dashboard read, as the request's
// person; only the capture adds something, a page on the read-later list.
// None of them shows the person's own notes on an article.

func itoa(n int64) string { return strconv.FormatInt(n, 10) }

// articlePath opens an article in the app.
func articlePath(id int64) string { return "/a/" + itoa(id) }

// articleTitle is the line an article is shown by: its title, else how it
// starts (a post without a title), else what the lists say.
func articleTitle(a store.Article) string {
	return firstNonEmpty(strings.TrimSpace(a.Title), store.Excerpt(a.Excerpt, 80), "Untitled")
}

// dots joins the parts of a line that are there: "Garden Essays · 4 min".
func dots(parts ...string) string {
	out := parts[:0:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return strings.Join(out, " · ")
}

// minutesWords says a length of time: "25 min", "1 h 5 min", "2 h".
func minutesWords(m int) string {
	switch {
	case m < 60:
		return fmt.Sprintf("%d min", m)
	case m%60 == 0:
		return fmt.Sprintf("%d h", m/60)
	}
	return fmt.Sprintf("%d h %d min", m/60, m%60)
}

// readingTime is how long an article takes to read, when its length is known.
func readingTime(a store.Article) string {
	if a.Words == 0 {
		return ""
	}
	return minutesWords(a.Minutes())
}

// ---------- search ----------

// searchPool is how many matches are looked at to put the closest first.
const searchPool = 50

var reSearchWord = regexp.MustCompile(`[\p{L}\p{N}]+`)

// pocketSearch answers Pocket Search with the search behind GET
// /articles?q=: saved pages and feed posts whose title, text or tags have
// every word ("quoted words" as a phrase). What the person kept (read
// later, archive, starred, highlighted) and a title that has the words come
// first, each newest first.
func (s *Server) pocketSearch(r *http.Request, q string, limit int) ([]core.SearchItem, error) {
	ctx := r.Context()
	// nothing to look for: the list without a search is every article there is
	words := reSearchWord.FindAllString(strings.ToLower(q), -1)
	if len(words) == 0 {
		return nil, nil
	}
	list, _, err := s.store.ListArticles(ctx, actorID(ctx), store.ArticleFilter{Query: q, Limit: searchPool})
	if err != nil {
		return nil, err
	}
	out := make([]core.SearchItem, 0, len(list))
	for _, a := range list {
		it := core.SearchItem{ID: itoa(a.ID), Title: articleTitle(a), Snippet: dots(a.SiteName, a.Excerpt), Kind: "article",
			Date: firstNonEmpty(a.PublishedAt, a.AddedAt), URL: articlePath(a.ID), Link: a.URL, Score: 1}
		if a.Saved {
			it.Date = firstNonEmpty(a.SavedAt, it.Date)
		}
		if a.Saved || a.Starred || a.Highlights > 0 {
			it.Score++
		}
		title := strings.ToLower(a.Title)
		if !slices.ContainsFunc(words, func(w string) bool { return !strings.Contains(title, w) }) {
			it.Score += 2
		}
		out = append(out, it)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Score > out[j].Score })
	if len(out) > limit {
		out = out[:limit]
	}
	return out, nil
}

// ---------- day ----------

// clock is a stored moment on the wall clock of loc: "09:30".
func clock(stamp string, loc *time.Location) string {
	t, err := time.Parse(time.RFC3339Nano, stamp)
	if err != nil {
		return ""
	}
	return t.In(loc).Format("15:04")
}

// sentence joins the parts of a day into one line: "Read 3 articles, saved
// 1 for later and highlighted 2 passages".
func sentence(parts []string) string {
	switch len(parts) {
	case 0:
		return ""
	case 1:
		return upperFirst(parts[0])
	}
	return upperFirst(strings.Join(parts[:len(parts)-1], ", ") + " and " + parts[len(parts)-1])
}

func upperFirst(s string) string {
	r, n := utf8.DecodeRuneInString(s)
	if n == 0 || unicode.IsUpper(r) {
		return s
	}
	return string(unicode.ToUpper(r)) + s[n:]
}

// pocketDay answers Pocket Day with the reading /day reports: the articles
// finished that day, the others read, and the ones saved for later. An
// article is finished on the day it was marked done; one that never was, on
// the day it was read to the end. It shows once, as the most telling of the
// three, at the time it happened. The minutes are the reading time of what
// was read, as /day counts it. With no time zone given the day is the
// server's, as in /day.
func (s *Server) pocketDay(r *http.Request, date string, tz *time.Location) (*core.Day, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	loc := time.Local
	if tz != nil {
		loc = tz
	}
	t, err := time.ParseInLocation("2006-01-02", date, loc)
	if err != nil {
		return nil, nil
	}
	from, to := t.UTC().Format(time.RFC3339), t.AddDate(0, 0, 1).UTC().Format(time.RFC3339)
	d, err := s.store.Day(ctx, uid, date, from, to)
	if err != nil {
		return nil, err
	}
	// marked done that day, whenever it was read
	done, _, err := s.store.ListArticles(ctx, uid, store.ArticleFilter{View: "archive", From: from, To: to, Limit: 200})
	if err != nil {
		return nil, err
	}
	doneAt := map[int64]string{}
	for _, a := range done {
		doneAt[a.ID] = a.ArchivedAt
	}

	day := &core.Day{Date: date}
	seen := map[int64]bool{}
	count := map[string]int{}
	minutes := 0
	add := func(a store.Article, kind, at string, read bool) {
		if seen[a.ID] {
			return
		}
		seen[a.ID] = true
		count[kind]++
		it := core.DayItem{ID: itoa(a.ID), Time: clock(at, loc), Title: articleTitle(a), Sub: a.SiteName, Kind: kind, Done: kind == "finished", URL: articlePath(a.ID)}
		if read && a.Words > 0 {
			it.Minutes = float64(a.Minutes())
			minutes += a.Minutes()
		}
		day.Items = append(day.Items, it)
	}
	for _, a := range d.Read {
		switch at := doneAt[a.ID]; {
		case at != "":
			add(a, "finished", at, true)
		case a.Progress >= 0.9 && !a.Archived:
			add(a, "finished", a.ReadAt, true)
		}
	}
	for _, a := range done {
		add(a, "finished", a.ArchivedAt, false)
	}
	for _, a := range d.Read {
		add(a, "read", a.ReadAt, true)
	}
	for _, a := range d.Saved {
		add(a, "saved", a.SavedAt, false)
	}

	var parts []string
	if n := count["finished"]; n > 0 {
		parts = append(parts, "finished "+plural(n, "article", "articles"))
	}
	if n := count["read"]; n > 0 {
		if len(parts) > 0 {
			parts = append(parts, fmt.Sprintf("read %d more", n))
		} else {
			parts = append(parts, "read "+plural(n, "article", "articles"))
		}
	}
	if n := count["saved"]; n > 0 {
		if len(parts) > 0 {
			parts = append(parts, fmt.Sprintf("saved %d for later", n))
		} else {
			parts = append(parts, "saved "+plural(n, "article", "articles")+" for later")
		}
	}
	if minutes > 0 {
		day.Metrics = append(day.Metrics, core.Metric{Label: "Reading time", Value: minutesWords(minutes)})
	}
	if n := len(d.Highlights); n > 0 {
		parts = append(parts, "highlighted "+plural(n, "passage", "passages"))
		day.Metrics = append(day.Metrics, core.Metric{Label: "Highlights", Value: strconv.Itoa(n)})
	}
	if len(parts) == 0 {
		return nil, nil
	}
	day.Summary = sentence(parts)
	return day, nil
}

// ---------- widget ----------

// widgetRows is how many articles the widget lists.
const widgetRows = 5

// pocketWidget answers Pocket Hub with the read-later list: how many
// articles wait there (the sidebar's own numbers) and the ones saved last.
// Someone with an empty list and no feeds gets nothing to show.
func (s *Server) pocketWidget(r *http.Request) (*core.WidgetData, error) {
	ctx := r.Context()
	uid := actorID(ctx)
	c, err := s.store.FeedCountsFor(ctx, uid, startOfToday())
	if err != nil {
		return nil, err
	}
	if c.Saved == 0 && c.Feeds == 0 {
		return nil, nil
	}
	w := &core.WidgetData{URL: "/saved", Metrics: []core.Metric{{Label: "To read", Value: strconv.Itoa(c.Saved)}}}
	var parts []string
	if c.Saved > 0 {
		parts = append(parts, fmt.Sprintf("%d to read", c.Saved))
	}
	if c.Feeds > 0 {
		w.Metrics = append(w.Metrics, core.Metric{Label: "Unread in feeds", Value: strconv.Itoa(c.Unread)})
		if c.Unread > 0 {
			parts = append(parts, fmt.Sprintf("%d unread in your feeds", c.Unread))
		}
	}
	// what was read at least half of, so "mark all as read" does not count
	w.Metrics = append(w.Metrics, core.Metric{Label: "Reading today", Value: minutesWords(c.ReadMinutes)})
	w.Line = firstNonEmpty(strings.Join(parts, ", "), "Nothing waiting to be read")
	if c.Saved > 0 {
		list, _, err := s.store.ListArticles(ctx, uid, store.ArticleFilter{View: "saved", Limit: widgetRows})
		if err != nil {
			return nil, err
		}
		for _, a := range list {
			w.Items = append(w.Items, core.WidgetItem{ID: itoa(a.ID), Title: articleTitle(a), Detail: dots(a.SiteName, readingTime(a)), URL: articlePath(a.ID)})
		}
	}
	return w, nil
}

// ---------- capture ----------

var reAddress = regexp.MustCompile(`(?i)\bhttps?://[^\s<>"]+`)

// trimAddress drops what a sentence puts after an address: "…/page." or
// "(see …/page)".
func trimAddress(a string) string {
	for a != "" {
		switch last := a[len(a)-1]; {
		case strings.ContainsRune(`.,;:!?'`, rune(last)):
		case last == ')' && !strings.Contains(a, "("):
		case last == ']' && !strings.Contains(a, "["):
		default:
			return a
		}
		a = a[:len(a)-1]
	}
	return a
}

// capturePage reads a quick line as a page to save: the one web address in
// it, the title when one was given and the other words as the person's note.
func capturePage(in core.CaptureInput) (saveInput, error) {
	var found []string
	// without takes the addresses out of a text. On a line that had one, what
	// joined it to the words goes too: "Read this: <address>." leaves "Read
	// this", "<address> - for the trip" leaves "for the trip".
	without := func(text string) string {
		var lines []string
		for _, l := range strings.Split(text, "\n") {
			had := false
			l = reAddress.ReplaceAllStringFunc(l, func(m string) string {
				if a := trimAddress(m); !slices.Contains(found, a) {
					found = append(found, a)
				}
				had = true
				return " "
			})
			l = strings.Join(strings.Fields(l), " ")
			if had {
				if l = strings.Trim(l, " -–—:|·,.()[]<>"); l == "" {
					continue
				}
			}
			lines = append(lines, l)
		}
		return strings.TrimSpace(strings.Join(lines, "\n"))
	}
	title, note := without(in.Title), without(in.Text)
	switch {
	case len(found) == 0:
		return saveInput{}, fmt.Errorf("%w: paste a web address to save it", core.ErrCapture)
	case len(found) > 1:
		return saveInput{}, fmt.Errorf("%w: one web address at a time", core.ErrCapture)
	}
	u, err := feed.CheckURL(found[0])
	if err != nil {
		return saveInput{}, fmt.Errorf("%w: %s", core.ErrCapture, err.Error())
	}
	return saveInput{URL: u.String(), Title: strings.Join(strings.Fields(title), " "), Note: note}, nil
}

// pocketCapture keeps a quick line from Pocket Hub as a page to read later,
// through the save behind POST /saved: the page is fetched as it is there,
// and an address the person already has is put back on the list instead of
// added twice (the words then become its note when it has none). The same
// request_id again answers the same article and saves nothing.
func (s *Server) pocketCapture(r *http.Request, in core.CaptureInput) (*core.CaptureResult, error) {
	// fetching a slow page can outlast the app that sent the line: the save
	// is finished anyway, and its retry gets the answer
	ctx := context.WithoutCancel(r.Context())
	uid := actorID(ctx)
	op, err := core.NewOperation(fmt.Sprintf("reader:user:%d:%s", uid, core.CapturePath), in.RequestID, in)
	if err != nil {
		return nil, err
	}
	var res core.CaptureResult
	replay := func() (*core.CaptureResult, error) {
		found, err := op.Replay(ctx, s.store.DB, &res)
		switch {
		case errors.Is(err, core.ErrOperationConflict):
			return nil, fmt.Errorf("%w: this was already kept with other words", core.ErrCapture)
		case err != nil || !found:
			return nil, err
		}
		return &res, nil
	}
	if kept, err := replay(); kept != nil || err != nil {
		return kept, err
	}

	page, err := capturePage(in)
	if err != nil {
		return nil, err
	}
	a, created, err := s.savePage(ctx, uid, page, firstNonEmpty(linkApp(ctx), "app"))
	if errors.Is(err, store.ErrConflict) {
		// the same address saved twice at once: the other one made the article
		if kept, rerr := replay(); kept != nil || rerr != nil {
			return kept, rerr
		}
		a, err = s.store.ArticleByURL(ctx, uid, page.URL)
	}
	if err != nil {
		return nil, err
	}
	if !created && page.Note != "" && a.Note == "" {
		if _, err := s.store.UpdateArticle(ctx, uid, []int64{a.ID}, store.ArticleState{Note: &page.Note}); err != nil {
			return nil, err
		}
	}
	// the receipt comes after the save, which fetches a page and so is not
	// one transaction; a retry in between finds the article by its address
	res = core.CaptureResult{ID: itoa(a.ID), Title: articleTitle(*a), URL: articlePath(a.ID)}
	if err := s.keepReceipt(ctx, op, res); err != nil {
		// the same line sent twice at once: the other one wrote the receipt
		if kept, rerr := replay(); kept != nil || rerr != nil {
			return kept, rerr
		}
		s.log.Warn("kept line: no receipt written", "article", a.ID, "err", err)
	}
	return &res, nil
}

// keepReceipt remembers the answer to a kept line for its request_id.
func (s *Server) keepReceipt(ctx context.Context, op *core.Operation, res core.CaptureResult) error {
	tx, err := s.store.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if err := op.Save(ctx, tx, res); err != nil {
		return err
	}
	return tx.Commit()
}
