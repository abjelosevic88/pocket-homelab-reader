package server

import (
	"bytes"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-reader/internal/feed"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// ---- export ----

// exportDoc is the JSON export of one person's reading: feeds and folders,
// every article they kept (saved, starred, highlighted or with a note) with
// its text, their highlights and rules. Unread feed posts are not in it:
// the feeds bring them back.
type exportDoc struct {
	App        string            `json:"app"`
	Version    string            `json:"version"`
	Exported   string            `json:"exported_at"`
	Folders    []string          `json:"folders"`
	Feeds      []exportFeed      `json:"feeds"`
	Articles   []exportArticle   `json:"articles"`
	Highlights []exportHighlight `json:"highlights"`
	Rules      []exportRule      `json:"rules"`
}

type exportFeed struct {
	URL         string `json:"url"`
	Title       string `json:"title"`
	CustomTitle string `json:"custom_title"`
	SiteURL     string `json:"site_url"`
	Folder      string `json:"folder"`
	FullText    bool   `json:"full_text"`
	Notify      bool   `json:"notify"`
	Paused      bool   `json:"paused"`
	RefreshMin  int    `json:"refresh_min"`
}

type exportArticle struct {
	URL         string  `json:"url"`
	Title       string  `json:"title"`
	Author      string  `json:"author"`
	SiteName    string  `json:"site_name"`
	Excerpt     string  `json:"excerpt"`
	Content     string  `json:"content"`
	ImageURL    string  `json:"image_url"`
	PublishedAt string  `json:"published_at"`
	Feed        string  `json:"feed"` // the feed's address, when it came from one
	Saved       bool    `json:"saved"`
	SavedAt     string  `json:"saved_at"`
	Archived    bool    `json:"archived"`
	Starred     bool    `json:"starred"`
	Read        bool    `json:"read"`
	Progress    float64 `json:"progress"`
	Tags        string  `json:"tags"`
	Note        string  `json:"note"`
}

type exportHighlight struct {
	URL       string  `json:"url"` // the article's address
	Text      string  `json:"text"`
	Note      string  `json:"note"`
	Color     string  `json:"color"`
	Prefix    string  `json:"prefix"`
	Suffix    string  `json:"suffix"`
	Position  float64 `json:"position"`
	CreatedAt string  `json:"created_at"`
}

type exportRule struct {
	Name    string `json:"name"`
	Feed    string `json:"feed"`
	Folder  string `json:"folder"`
	Field   string `json:"field"`
	Words   string `json:"words"`
	Action  string `json:"action"`
	Tag     string `json:"tag"`
	Enabled bool   `json:"enabled"`
}

// kept lists every article of a person worth keeping in an export.
func (s *Server) kept(ctx context.Context, uid int64) ([]store.Article, error) {
	var out []store.Article
	seen := map[int64]bool{}
	for _, view := range []string{"saved", "archive", "starred", "highlights"} {
		cursor := ""
		for {
			f := store.ArticleFilter{View: view, Limit: 500}
			f.Before, f.BeforeID = store.ParseCursor(cursor)
			list, next, err := s.store.ListArticles(ctx, uid, f)
			if err != nil {
				return nil, err
			}
			for _, a := range list {
				if !seen[a.ID] {
					seen[a.ID] = true
					out = append(out, a)
				}
			}
			if next == "" {
				break
			}
			cursor = next
		}
	}
	return out, nil
}

func (s *Server) handleExportJSON(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	doc := exportDoc{App: "pocket-reader", Version: Version, Exported: store.Now(), Folders: []string{}, Feeds: []exportFeed{}, Articles: []exportArticle{}, Highlights: []exportHighlight{}, Rules: []exportRule{}}
	folders := s.folderNames(ctx, uid)
	fl, _ := s.store.ListFolders(ctx, uid)
	for _, f := range fl {
		doc.Folders = append(doc.Folders, f.Name)
	}
	feeds, _ := s.store.ListFeeds(ctx, uid)
	feedURL := map[int64]string{}
	for _, f := range feeds {
		feedURL[f.ID] = f.URL
		e := exportFeed{URL: f.URL, Title: f.Title, CustomTitle: f.CustomTitle, SiteURL: f.SiteURL, FullText: f.FullText, Notify: f.Notify, Paused: f.Paused, RefreshMin: f.RefreshMin}
		if f.FolderID != nil {
			e.Folder = folders[*f.FolderID]
		}
		doc.Feeds = append(doc.Feeds, e)
	}
	list, err := s.kept(ctx, uid)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	for _, a := range list {
		full, err := s.store.GetArticle(ctx, uid, a.ID)
		if err != nil {
			continue
		}
		e := exportArticle{URL: full.URL, Title: full.Title, Author: full.Author, SiteName: full.SiteName, Excerpt: full.Excerpt, Content: full.Content,
			ImageURL: full.ImageURL, PublishedAt: full.PublishedAt, Saved: full.Saved, SavedAt: full.SavedAt, Archived: full.Archived, Starred: full.Starred,
			Read: full.Read, Progress: full.Progress, Tags: full.Tags, Note: full.Note}
		if full.FeedID != nil {
			e.Feed = feedURL[*full.FeedID]
		}
		doc.Articles = append(doc.Articles, e)
		hs, _ := s.store.ListHighlights(ctx, uid, store.HighlightFilter{ArticleID: a.ID})
		for _, h := range hs {
			doc.Highlights = append(doc.Highlights, exportHighlight{URL: full.URL, Text: h.Text, Note: h.Note, Color: h.Color, Prefix: h.Prefix, Suffix: h.Suffix, Position: h.Position, CreatedAt: h.CreatedAt})
		}
	}
	rules, _ := s.store.ListRules(ctx, uid)
	for _, ru := range rules {
		e := exportRule{Name: ru.Name, Field: ru.Field, Words: ru.Words, Action: ru.Action, Tag: ru.Tag, Enabled: ru.Enabled}
		if ru.FeedID != nil {
			e.Feed = feedURL[*ru.FeedID]
		}
		if ru.FolderID != nil {
			e.Folder = folders[*ru.FolderID]
		}
		doc.Rules = append(doc.Rules, e)
	}
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-reader-export.json"`)
	writeJSON(w, http.StatusOK, doc)
}

// handleExportCSV writes the read-later list (and archive) as CSV.
func (s *Server) handleExportCSV(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	list, err := s.kept(ctx, actorID(ctx))
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	w.Header().Set("Content-Type", "text/csv; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-reader-saved.csv"`)
	cw := csv.NewWriter(w)
	_ = cw.Write([]string{"url", "title", "site", "author", "saved_at", "status", "starred", "tags", "highlights", "minutes"})
	for _, a := range list {
		status := "unread"
		switch {
		case a.Archived:
			status = "archived"
		case a.Read:
			status = "read"
		}
		_ = cw.Write([]string{a.URL, a.Title, a.SiteName, a.Author, a.SavedAt, status, strconv.FormatBool(a.Starred), a.Tags, strconv.Itoa(a.Highlights), strconv.Itoa(a.Minutes())})
	}
	cw.Flush()
}

// ---- import ----

// handleImport takes a file (multipart "file" or the raw body) and works out
// what it is: a Pocket Reader export, OPML (FreshRSS, Feedly, any reader),
// a Karakeep / Hoarder export, browser bookmarks (Netscape HTML: Chrome,
// Firefox, Safari, Raindrop, Instapaper) or a CSV of links (Pocket,
// Instapaper, Omnivore, Readwise). Importing the same file twice adds nothing.
func (s *Server) handleImport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var body []byte
	var err error
	if strings.HasPrefix(r.Header.Get("Content-Type"), "multipart/") {
		if err := r.ParseMultipartForm(s.cfg.MaxBodyBytes); err != nil {
			writeErr(w, http.StatusBadRequest, "that file is too large")
			return
		}
		f, _, ferr := r.FormFile("file")
		if ferr != nil {
			writeErr(w, http.StatusBadRequest, "choose a file")
			return
		}
		defer f.Close()
		body, err = io.ReadAll(io.LimitReader(f, s.cfg.MaxBodyBytes))
	} else {
		body, err = s.readBody(r)
	}
	if err != nil || len(bytes.TrimSpace(body)) == 0 {
		writeErr(w, http.StatusBadRequest, "the file is empty")
		return
	}
	kind := r.URL.Query().Get("kind")
	if kind == "" || kind == "auto" {
		kind = sniffImport(body)
	}
	var counts map[string]int
	switch kind {
	case "opml":
		counts, err = s.importOPML(ctx, uid, body)
	case "pocket-reader":
		counts, err = s.importOwn(ctx, uid, body)
	case "karakeep":
		counts, err = s.importKarakeep(ctx, uid, body)
	case "bookmarks":
		counts, err = s.importBookmarks(ctx, uid, body)
	case "csv":
		counts, err = s.importCSV(ctx, uid, body)
	default:
		writeErr(w, http.StatusBadRequest, "this file is not one Pocket Reader can read (OPML, a Pocket Reader or Karakeep export, browser bookmarks, or a CSV of links)")
		return
	}
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	go func() {
		c, cancel := context.WithTimeout(context.Background(), 30*time.Minute)
		defer cancel()
		s.extractPending(c)
	}()
	writeJSON(w, http.StatusOK, map[string]any{"kind": kind, "counts": counts})
}

func sniffImport(b []byte) string {
	head := strings.ToLower(string(bytes.TrimSpace(b[:min(len(b), 4096)])))
	switch {
	case strings.Contains(head, "<opml"):
		return "opml"
	case strings.Contains(head, "netscape-bookmark-file") || (strings.Contains(head, "<dl") && strings.Contains(head, "<dt")):
		return "bookmarks"
	case strings.HasPrefix(head, "{") || strings.HasPrefix(head, "["):
		if strings.Contains(head, `"app":"pocket-reader"`) || strings.Contains(head, `"app": "pocket-reader"`) {
			return "pocket-reader"
		}
		return "karakeep"
	case strings.Contains(strings.SplitN(head, "\n", 2)[0], "url"):
		return "csv"
	}
	return ""
}

// importPage saves one imported link. Its text is fetched in the background
// (when the person keeps full articles), so a big file imports at once.
func (s *Server) importPage(ctx context.Context, uid int64, st store.Settings, a store.Article, counts map[string]int) {
	u, err := feed.CheckURL(a.URL)
	if err != nil {
		counts["skipped"]++
		return
	}
	a.URL = u.String()
	if have, err := s.store.ArticleByURL(ctx, uid, a.URL); err == nil {
		if have.Saved || !a.Saved {
			counts["already"]++
			return
		}
	}
	if a.Title == "" {
		a.Title = strings.TrimPrefix(u.Hostname(), "www.") + u.Path
	}
	if a.SiteName == "" {
		a.SiteName = strings.TrimPrefix(u.Hostname(), "www.")
	}
	if a.Content == "" && st.AutoExtract {
		a.ExtractState = "pending"
	}
	a.Source = "import"
	if _, created, err := s.store.SavePage(ctx, uid, &a); err != nil {
		counts["skipped"]++
	} else if created {
		counts["saved"]++
	} else {
		counts["already"]++
	}
}

// importOwn merges a Pocket Reader export into the caller's own reading.
func (s *Server) importOwn(ctx context.Context, uid int64, body []byte) (map[string]int, error) {
	var doc exportDoc
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, errors.New("invalid json")
	}
	if doc.App != "pocket-reader" {
		return nil, errors.New("this is not a Pocket Reader export")
	}
	counts := map[string]int{"feeds": 0, "saved": 0, "highlights": 0, "rules": 0, "already": 0, "skipped": 0}
	for _, name := range doc.Folders {
		_, _ = s.store.FolderByName(ctx, uid, name, true)
	}
	for _, f := range doc.Feeds {
		u, err := feed.CheckURL(f.URL)
		if err != nil {
			counts["skipped"]++
			continue
		}
		if _, err := s.store.FeedByURL(ctx, uid, u.String()); err == nil {
			counts["already"]++
			continue
		}
		folder, _ := s.resolveFolder(ctx, uid, nil, f.Folder)
		nf := &store.Feed{URL: u.String(), Title: f.Title, CustomTitle: f.CustomTitle, SiteURL: f.SiteURL, FolderID: folder, FullText: f.FullText, Notify: f.Notify, Paused: f.Paused, RefreshMin: f.RefreshMin, Source: "import"}
		if err := s.store.CreateFeed(ctx, uid, nf); err == nil {
			counts["feeds"]++
		}
	}
	st, _ := s.store.GetSettings(ctx, uid)
	for _, a := range doc.Articles {
		// a starred or highlighted feed post comes back as a saved page in the archive
		s.importPage(ctx, uid, st, store.Article{URL: a.URL, Title: a.Title, Author: a.Author, SiteName: a.SiteName, Excerpt: a.Excerpt, Content: a.Content,
			ImageURL: a.ImageURL, PublishedAt: a.PublishedAt, Saved: true, SavedAt: a.SavedAt, Archived: a.Archived || !a.Saved, Starred: a.Starred, Read: a.Read,
			Tags: a.Tags, Note: a.Note, ExtractState: map[bool]string{true: "done", false: ""}[a.Content != ""]}, counts)
		if a.Progress > 0 {
			if got, err := s.store.ArticleByURL(ctx, uid, a.URL); err == nil && got.Progress == 0 {
				p := a.Progress
				_, _ = s.store.UpdateArticle(ctx, uid, []int64{got.ID}, store.ArticleState{Progress: &p})
			}
		}
	}
	for _, h := range doc.Highlights {
		a, err := s.store.ArticleByURL(ctx, uid, h.URL)
		if err != nil || strings.TrimSpace(h.Text) == "" {
			counts["skipped"]++
			continue
		}
		have, _ := s.store.ListHighlights(ctx, uid, store.HighlightFilter{ArticleID: a.ID})
		dup := false
		for _, x := range have {
			if x.Text == h.Text {
				dup = true
				break
			}
		}
		if dup {
			counts["already"]++
			continue
		}
		if !colors[h.Color] {
			h.Color = "yellow"
		}
		nh := store.Highlight{ArticleID: a.ID, Text: h.Text, Note: h.Note, Color: h.Color, Prefix: h.Prefix, Suffix: h.Suffix, Position: h.Position, CreatedAt: h.CreatedAt}
		if err := s.store.CreateHighlight(ctx, uid, &nh); err == nil {
			counts["highlights"]++
		}
	}
	have, _ := s.store.ListRules(ctx, uid)
	for _, ru := range doc.Rules {
		dup := false
		for _, x := range have {
			if x.Words == ru.Words && x.Action == ru.Action && x.Field == ru.Field && x.Tag == ru.Tag {
				dup = true
			}
		}
		if dup {
			counts["already"]++
			continue
		}
		nr := store.Rule{Name: ru.Name, Field: ru.Field, Words: ru.Words, Action: ru.Action, Tag: ru.Tag, Enabled: ru.Enabled}
		if ru.Feed != "" {
			if f, err := s.store.FeedByURL(ctx, uid, ru.Feed); err == nil {
				nr.FeedID = &f.ID
			}
		}
		if ru.Folder != "" {
			if f, err := s.store.FolderByName(ctx, uid, ru.Folder, false); err == nil {
				nr.FolderID = &f.ID
			}
		}
		if s.checkRule(ctx, uid, &nr) == "" && s.store.CreateRule(ctx, uid, &nr) == nil {
			counts["rules"]++
		}
	}
	s.kickRefresh()
	return counts, nil
}

// importKarakeep reads a Karakeep (formerly Hoarder) export: bookmarks with
// tags, notes, favourites and the archive. Text notes and pictures are skipped.
func (s *Server) importKarakeep(ctx context.Context, uid int64, body []byte) (map[string]int, error) {
	var doc struct {
		Bookmarks []struct {
			CreatedAt  json.Number       `json:"createdAt"`
			Title      *string           `json:"title"`
			Tags       []json.RawMessage `json:"tags"`
			Note       *string           `json:"note"`
			Archived   bool              `json:"archived"`
			Favourited bool              `json:"favourited"`
			URL        string            `json:"url"`
			Content    struct {
				Type        string  `json:"type"`
				URL         string  `json:"url"`
				Title       *string `json:"title"`
				Description *string `json:"description"`
				ImageURL    *string `json:"imageUrl"`
				Author      *string `json:"author"`
				Publisher   *string `json:"publisher"`
			} `json:"content"`
		} `json:"bookmarks"`
	}
	if err := json.Unmarshal(body, &doc); err != nil {
		return nil, errors.New("this is not a Karakeep export")
	}
	if doc.Bookmarks == nil {
		return nil, errors.New("this is not a Karakeep export (no bookmarks in it)")
	}
	st, _ := s.store.GetSettings(ctx, uid)
	counts := map[string]int{"saved": 0, "already": 0, "skipped": 0}
	str := func(p *string) string {
		if p == nil {
			return ""
		}
		return strings.TrimSpace(*p)
	}
	for _, b := range doc.Bookmarks {
		link := firstNonEmpty(b.Content.URL, b.URL)
		if link == "" || (b.Content.Type != "" && b.Content.Type != "link") {
			counts["skipped"]++
			continue
		}
		var tags []string
		for _, raw := range b.Tags {
			var t string
			if json.Unmarshal(raw, &t) != nil {
				var o struct {
					Name string `json:"name"`
				}
				_ = json.Unmarshal(raw, &o)
				t = o.Name
			}
			if t = strings.TrimSpace(t); t != "" {
				tags = append(tags, t)
			}
		}
		a := store.Article{URL: link, Title: firstNonEmpty(str(b.Title), str(b.Content.Title)), Excerpt: str(b.Content.Description), ImageURL: str(b.Content.ImageURL),
			Author: str(b.Content.Author), SiteName: str(b.Content.Publisher), Note: str(b.Note), Tags: strings.Join(tags, ","),
			Saved: true, Archived: b.Archived, Starred: b.Favourited, SavedAt: epochish(b.CreatedAt.String())}
		s.importPage(ctx, uid, st, a, counts)
	}
	return counts, nil
}

// epochish reads a time as seconds or milliseconds since 1970, or RFC 3339.
func epochish(v string) string {
	v = strings.TrimSpace(v)
	if v == "" {
		return ""
	}
	if n, err := strconv.ParseInt(v, 10, 64); err == nil && n > 0 {
		if n > 1e12 {
			return time.UnixMilli(n).UTC().Format(time.RFC3339)
		}
		return time.Unix(n, 0).UTC().Format(time.RFC3339)
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02 15:04:05", "2006-01-02"} {
		if t, err := time.Parse(layout, v); err == nil {
			return t.UTC().Format(time.RFC3339)
		}
	}
	return ""
}

var reBookmarkTok = regexp.MustCompile(`(?is)<h3[^>]*>(.*?)</h3>|<dl[^>]*>|</dl>|<a\s([^>]*)>(.*?)</a>`)

// importBookmarks reads browser bookmarks (the Netscape HTML every browser,
// Raindrop, Instapaper and Pocket export). Folders become tags.
func (s *Server) importBookmarks(ctx context.Context, uid int64, body []byte) (map[string]int, error) {
	st, _ := s.store.GetSettings(ctx, uid)
	counts := map[string]int{"saved": 0, "already": 0, "skipped": 0}
	// walk the file in order: a heading names the folder of the list that follows it
	var stack []string
	pending := ""
	for _, m := range reBookmarkTok.FindAllStringSubmatch(string(body), -1) {
		tok := strings.ToLower(m[0])
		switch {
		case strings.HasPrefix(tok, "<h3"):
			pending = feed.PlainText(m[1])
			continue
		case strings.HasPrefix(tok, "<dl"):
			stack = append(stack, pending)
			pending = ""
			continue
		case strings.HasPrefix(tok, "</dl"):
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
			}
			continue
		}
		folder := ""
		if len(stack) > 0 {
			folder = stack[len(stack)-1]
		}
		a := attrMap(m[2])
		title := feed.PlainText(m[3])
		link := a["href"]
		if !strings.HasPrefix(link, "http") {
			counts["skipped"]++
			continue
		}
		tags := a["tags"]
		if folder != "" && !strings.EqualFold(folder, "bookmarks bar") && !strings.EqualFold(folder, "bookmarks toolbar") && !strings.EqualFold(folder, "unread") && !strings.EqualFold(folder, "archive") {
			tags = strings.Trim(tags+","+folder, ",")
		}
		s.importPage(ctx, uid, st, store.Article{URL: link, Title: title, Tags: tags, Saved: true, SavedAt: epochish(a["add_date"]),
			Archived: strings.EqualFold(folder, "archive") || a["toread"] == "0"}, counts)
	}
	if counts["saved"]+counts["already"]+counts["skipped"] == 0 {
		return nil, errors.New("no links in that file")
	}
	return counts, nil
}

var reAttr = regexp.MustCompile(`(?is)([a-zA-Z_:-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)

func attrMap(s string) map[string]string {
	out := map[string]string{}
	for _, m := range reAttr.FindAllStringSubmatch(s, -1) {
		v := firstNonEmpty(m[3], m[4], m[5])
		out[strings.ToLower(m[1])] = html.UnescapeString(v)
	}
	return out
}

// importCSV reads a CSV of links with a header row naming the columns (any
// of url, title, tags, time_added / timestamp / saved_at / created, status / folder).
func (s *Server) importCSV(ctx context.Context, uid int64, body []byte) (map[string]int, error) {
	cr := csv.NewReader(bytes.NewReader(body))
	cr.FieldsPerRecord = -1
	cr.LazyQuotes = true
	rows, err := cr.ReadAll()
	if err != nil || len(rows) < 2 {
		return nil, errors.New("that CSV has no links in it")
	}
	col := map[string]int{}
	for i, h := range rows[0] {
		col[strings.ToLower(strings.TrimSpace(h))] = i
	}
	get := func(row []string, names ...string) string {
		for _, n := range names {
			if i, ok := col[n]; ok && i < len(row) {
				return strings.TrimSpace(row[i])
			}
		}
		return ""
	}
	if _, ok := col["url"]; !ok {
		return nil, errors.New("that CSV has no url column")
	}
	st, _ := s.store.GetSettings(ctx, uid)
	counts := map[string]int{"saved": 0, "already": 0, "skipped": 0}
	for _, row := range rows[1:] {
		status := strings.ToLower(get(row, "status", "folder", "state"))
		tags := strings.ReplaceAll(get(row, "tags", "labels"), "|", ",")
		s.importPage(ctx, uid, st, store.Article{URL: get(row, "url"), Title: get(row, "title"), Tags: tags, Note: get(row, "note", "notes", "selection"), Saved: true,
			SavedAt: epochish(get(row, "time_added", "timestamp", "saved_at", "created", "created_at", "date")), Archived: status == "archive" || status == "archived" || status == "read"}, counts)
	}
	return counts, nil
}

// handleImportOPMLText is the OPML import of the add dialog (a pasted file).
func (s *Server) handleImportOPMLText(w http.ResponseWriter, r *http.Request) {
	b, err := s.readBody(r)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "that file is too large")
		return
	}
	counts, err := s.importOPML(r.Context(), actorID(r.Context()), b)
	if err != nil {
		writeErr(w, http.StatusBadRequest, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"kind": "opml", "counts": counts, "summary": fmt.Sprintf("%s added, %d already followed", plural(counts["feeds"], "feed", "feeds"), counts["already"])})
}
