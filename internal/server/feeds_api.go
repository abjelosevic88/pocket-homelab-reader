package server

import (
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-reader/internal/feed"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// ---- feeds and folders ----

func (s *Server) handleListFeeds(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	feeds, err := s.store.ListFeeds(ctx, uid)
	if err != nil {
		s.fail(w, err, "feeds")
		return
	}
	folders, err := s.store.ListFolders(ctx, uid)
	if err != nil {
		s.fail(w, err, "folders")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"feeds": feeds, "folders": folders})
}

func (s *Server) handleGetFeed(w http.ResponseWriter, r *http.Request) {
	f, err := s.store.GetFeed(r.Context(), actorID(r.Context()), idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "feed")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

type feedInput struct {
	URL         string `json:"url"`
	FolderID    *int64 `json:"folder_id"`
	Folder      string `json:"folder"` // a folder by name, made when missing
	Title       string `json:"custom_title"`
	FullText    bool   `json:"full_text"`
	Notify      bool   `json:"notify"`
	Paused      bool   `json:"paused"`
	RefreshMin  int    `json:"refresh_min"`
	MarkOldRead *bool  `json:"mark_old_read"`
}

// resolveFolder turns a folder id or name into the person's folder id.
func (s *Server) resolveFolder(ctx context.Context, uid int64, id *int64, name string) (*int64, error) {
	if id != nil && *id > 0 {
		if _, err := s.store.GetFolder(ctx, uid, *id); err != nil {
			return nil, err
		}
		return id, nil
	}
	if strings.TrimSpace(name) != "" {
		f, err := s.store.FolderByName(ctx, uid, name, true)
		if err != nil {
			return nil, err
		}
		return &f.ID, nil
	}
	return nil, nil
}

// findFeed turns whatever address someone pasted (a feed, a website, a
// YouTube channel) into a feed address.
func (s *Server) findFeed(ctx context.Context, raw string) (string, *feed.Probe, error) {
	p, err := s.fetcher.Probe(ctx, raw)
	if err != nil {
		return "", nil, err
	}
	if p.IsFeed {
		return p.URL, p, nil
	}
	if len(p.Feeds) == 0 {
		p.Feeds = s.fetcher.Guess(ctx, p.URL)
	}
	if len(p.Feeds) == 0 {
		return "", p, errors.New("that page has no feed; save it to read later instead")
	}
	return p.Feeds[0].URL, p, nil
}

// handleCreateFeed subscribes and checks the feed at once.
func (s *Server) handleCreateFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in feedInput
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.URL) == "" {
		writeErr(w, http.StatusBadRequest, "a feed or website address is needed")
		return
	}
	f, res, err := s.subscribe(ctx, uid, in, firstNonEmpty(linkApp(ctx), "app"))
	if err != nil {
		if errors.Is(err, store.ErrConflict) {
			writeJSON(w, http.StatusConflict, map[string]any{"error": "you already follow this feed", "feed": f})
			return
		}
		if errors.Is(err, store.ErrNotFound) {
			s.fail(w, err, "folder")
			return
		}
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"feed": f, "result": res})
}

// subscribe adds a feed for a person and checks it once.
func (s *Server) subscribe(ctx context.Context, uid int64, in feedInput, source string) (*store.Feed, *fetchResult, error) {
	c, cancel := context.WithTimeout(ctx, 45*time.Second)
	defer cancel()
	feedURL, probe, err := s.findFeed(c, in.URL)
	if err != nil {
		return nil, nil, err
	}
	if old, err := s.store.FeedByURL(ctx, uid, feedURL); err == nil {
		return old, nil, store.ErrConflict
	}
	folder, err := s.resolveFolder(ctx, uid, in.FolderID, in.Folder)
	if err != nil {
		return nil, nil, err
	}
	f := &store.Feed{URL: feedURL, FolderID: folder, CustomTitle: strings.TrimSpace(in.Title), FullText: in.FullText, Notify: in.Notify, RefreshMin: in.RefreshMin, Source: source}
	if probe != nil && probe.Feed != nil {
		f.Title, f.SiteURL, f.Kind = probe.Feed.Title, probe.Feed.SiteURL, probe.Feed.Kind
	}
	if err := s.store.CreateFeed(ctx, uid, f); err != nil {
		return nil, nil, err
	}
	st, _ := s.store.GetSettings(ctx, uid)
	res, _ := s.fetchFeed(c, f, st)
	got, err := s.store.GetFeed(ctx, uid, f.ID)
	if err != nil {
		return nil, nil, err
	}
	s.publish(ctx, uid, "reader.subscribed", got.URL, map[string]any{"feed_id": got.ID, "title": got.Name(), "url": got.URL, "site_url": got.SiteURL})
	return got, res, nil
}

func (s *Server) handleUpdateFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	f, err := s.store.GetFeed(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "feed")
		return
	}
	var in feedInput
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["folder_id"] || keys["folder"] {
		folder, err := s.resolveFolder(ctx, uid, in.FolderID, in.Folder)
		if err != nil {
			s.fail(w, err, "folder")
			return
		}
		f.FolderID = folder
	}
	if keys["custom_title"] {
		f.CustomTitle = strings.TrimSpace(in.Title)
	}
	if keys["url"] && strings.TrimSpace(in.URL) != "" && strings.TrimSpace(in.URL) != f.URL {
		u, err := feed.CheckURL(in.URL)
		if err != nil {
			writeErr(w, http.StatusBadRequest, err.Error())
			return
		}
		f.URL = u.String()
		f.NextFetchAt = ""
	}
	if keys["full_text"] {
		f.FullText = in.FullText
	}
	if keys["notify"] {
		f.Notify = in.Notify
	}
	if keys["paused"] {
		f.Paused = in.Paused
	}
	if keys["refresh_min"] {
		if in.RefreshMin != 0 && (in.RefreshMin < 10 || in.RefreshMin > 7*24*60) {
			writeErr(w, http.StatusBadRequest, "a feed is checked every 10 minutes to every week")
			return
		}
		f.RefreshMin = in.RefreshMin
	}
	if err := s.store.UpdateFeed(ctx, uid, f); err != nil {
		s.fail(w, err, "save feed")
		return
	}
	if keys["url"] || (keys["paused"] && !in.Paused) {
		_ = s.store.FeedDueNow(ctx, uid, f.ID)
		s.kickRefresh()
	}
	got, _ := s.store.GetFeed(ctx, uid, f.ID)
	writeJSON(w, http.StatusOK, got)
}

func (s *Server) handleDeleteFeed(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteFeed(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "unsubscribe")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleRefreshFeed checks one feed now and says what it found.
func (s *Server) handleRefreshFeed(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	f, err := s.store.GetFeed(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "feed")
		return
	}
	f.ETag, f.LastModified = "", ""
	st, _ := s.store.GetSettings(ctx, uid)
	c, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	res, _ := s.fetchFeed(c, f, st)
	writeJSON(w, http.StatusOK, res)
}

// handleRefreshAll makes all of a person's feeds due and starts checking.
func (s *Server) handleRefreshAll(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if err := s.store.FeedDueNow(ctx, actorID(ctx), 0); err != nil {
		s.fail(w, err, "refresh")
		return
	}
	s.kickRefresh()
	writeJSON(w, http.StatusAccepted, map[string]any{"ok": true})
}

func (s *Server) handleCreateFolder(w http.ResponseWriter, r *http.Request) {
	var in store.Folder
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.Name) == "" {
		writeErr(w, http.StatusBadRequest, "a folder needs a name")
		return
	}
	if len(in.Name) > 80 {
		writeErr(w, http.StatusBadRequest, "that name is too long")
		return
	}
	f := &store.Folder{Name: in.Name}
	if err := s.store.CreateFolder(r.Context(), actorID(r.Context()), f); err != nil {
		s.fail(w, err, "folder")
		return
	}
	writeJSON(w, http.StatusCreated, f)
}

func (s *Server) handleUpdateFolder(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	f, err := s.store.GetFolder(ctx, uid, idParam(r, "id"))
	if err != nil {
		s.fail(w, err, "folder")
		return
	}
	var in store.Folder
	keys, err := decodeKeys(r, &in)
	if err != nil {
		writeErr(w, http.StatusBadRequest, "invalid json")
		return
	}
	if keys["name"] && strings.TrimSpace(in.Name) != "" {
		f.Name = in.Name
	}
	if keys["sort"] {
		f.Sort = in.Sort
	}
	if err := s.store.UpdateFolder(ctx, uid, f); err != nil {
		s.fail(w, err, "folder")
		return
	}
	writeJSON(w, http.StatusOK, f)
}

func (s *Server) handleDeleteFolder(w http.ResponseWriter, r *http.Request) {
	if err := s.store.DeleteFolder(r.Context(), actorID(r.Context()), idParam(r, "id")); err != nil {
		s.fail(w, err, "folder")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// handleDiscover looks at an address someone pasted and says what can be
// done with it: the feeds it offers (and whether they are followed already)
// and the page itself (and whether it is saved already).
func (s *Server) handleDiscover(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	var in struct {
		URL string `json:"url"`
	}
	if err := decode(r, &in); err != nil || strings.TrimSpace(in.URL) == "" {
		writeErr(w, http.StatusBadRequest, "paste an address")
		return
	}
	c, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	p, err := s.fetcher.Probe(c, in.URL)
	if err != nil {
		writeErr(w, http.StatusUnprocessableEntity, err.Error())
		return
	}
	type option struct {
		feed.Found
		FeedID int64 `json:"feed_id"`
	}
	var feeds []option
	add := func(f feed.Found) {
		o := option{Found: f}
		if have, err := s.store.FeedByURL(ctx, uid, f.URL); err == nil {
			o.FeedID = have.ID
		}
		feeds = append(feeds, o)
	}
	out := map[string]any{"url": p.URL, "is_feed": p.IsFeed}
	if p.IsFeed {
		add(feed.Found{URL: p.URL, Title: p.Title, Kind: p.Feed.Kind})
		var items []map[string]string
		for i, it := range p.Feed.Items {
			if i == 3 {
				break
			}
			items = append(items, map[string]string{"title": it.Title, "published_at": it.Published.UTC().Format(time.RFC3339)})
		}
		out["preview"] = items
	} else {
		list := p.Feeds
		if len(list) == 0 {
			list = s.fetcher.Guess(c, p.URL)
		}
		for _, f := range list {
			add(f)
		}
		page := map[string]any{"title": p.Title, "excerpt": p.Excerpt, "image_url": p.ImageURL, "site_name": p.SiteName}
		if a, err := s.store.ArticleByURL(ctx, uid, p.URL); err == nil && a.Saved {
			page["saved_id"] = a.ID
		}
		out["page"] = page
	}
	if feeds == nil {
		feeds = []option{}
	}
	out["feeds"] = feeds
	writeJSON(w, http.StatusOK, out)
}

// ---- OPML ----

type opmlDoc struct {
	XMLName xml.Name    `xml:"opml"`
	Version string      `xml:"version,attr"`
	Title   string      `xml:"head>title"`
	Body    []opmlEntry `xml:"body>outline"`
}

type opmlEntry struct {
	Text     string      `xml:"text,attr"`
	Title    string      `xml:"title,attr,omitempty"`
	Type     string      `xml:"type,attr,omitempty"`
	XMLURL   string      `xml:"xmlUrl,attr,omitempty"`
	HTMLURL  string      `xml:"htmlUrl,attr,omitempty"`
	Children []opmlEntry `xml:"outline"`
}

func (s *Server) handleExportOPML(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	uid := actorID(ctx)
	feeds, err := s.store.ListFeeds(ctx, uid)
	if err != nil {
		s.fail(w, err, "export")
		return
	}
	folders, _ := s.store.ListFolders(ctx, uid)
	byFolder := map[int64][]opmlEntry{}
	var top []opmlEntry
	for _, f := range feeds {
		e := opmlEntry{Text: f.Name(), Title: f.Name(), Type: "rss", XMLURL: f.URL, HTMLURL: f.SiteURL}
		if f.FolderID != nil {
			byFolder[*f.FolderID] = append(byFolder[*f.FolderID], e)
		} else {
			top = append(top, e)
		}
	}
	doc := opmlDoc{Version: "2.0", Title: "Pocket Reader subscriptions"}
	for _, fo := range folders {
		if kids := byFolder[fo.ID]; len(kids) > 0 {
			doc.Body = append(doc.Body, opmlEntry{Text: fo.Name, Title: fo.Name, Children: kids})
		}
	}
	doc.Body = append(doc.Body, top...)
	w.Header().Set("Content-Type", "text/x-opml; charset=utf-8")
	w.Header().Set("Content-Disposition", `attachment; filename="pocket-reader-subscriptions.opml"`)
	_, _ = io.WriteString(w, xml.Header)
	enc := xml.NewEncoder(w)
	enc.Indent("", "  ")
	_ = enc.Encode(doc)
}

// opmlFeed is one subscription read from an OPML file.
type opmlFeed struct {
	URL, Title, Site, Folder string
}

func parseOPML(b []byte) ([]opmlFeed, error) {
	var doc opmlDoc
	dec := xml.NewDecoder(strings.NewReader(string(b)))
	dec.Strict = false
	dec.CharsetReader = func(_ string, in io.Reader) (io.Reader, error) { return in, nil }
	if err := dec.Decode(&doc); err != nil {
		return nil, errors.New("that is not an OPML file")
	}
	var out []opmlFeed
	var walk func(list []opmlEntry, folder string)
	walk = func(list []opmlEntry, folder string) {
		for _, e := range list {
			if e.XMLURL != "" {
				out = append(out, opmlFeed{URL: strings.TrimSpace(e.XMLURL), Title: firstNonEmpty(e.Title, e.Text), Site: e.HTMLURL, Folder: folder})
				continue
			}
			name := firstNonEmpty(e.Title, e.Text)
			if folder != "" {
				name = folder // nested folders flatten into the outer one
			}
			walk(e.Children, name)
		}
	}
	walk(doc.Body, "")
	return out, nil
}

// importOPML adds the feeds of an OPML file without fetching them first:
// they are checked in the background as they fall due.
func (s *Server) importOPML(ctx context.Context, uid int64, b []byte) (map[string]int, error) {
	list, err := parseOPML(b)
	if err != nil {
		return nil, err
	}
	counts := map[string]int{"feeds": 0, "already": 0, "skipped": 0}
	for _, o := range list {
		u, err := feed.CheckURL(o.URL)
		if err != nil {
			counts["skipped"]++
			continue
		}
		if _, err := s.store.FeedByURL(ctx, uid, u.String()); err == nil {
			counts["already"]++
			continue
		}
		folder, _ := s.resolveFolder(ctx, uid, nil, o.Folder)
		f := &store.Feed{URL: u.String(), Title: o.Title, SiteURL: o.Site, FolderID: folder, Source: "import"}
		if err := s.store.CreateFeed(ctx, uid, f); err != nil {
			counts["skipped"]++
			continue
		}
		counts["feeds"]++
	}
	s.kickRefresh()
	return counts, nil
}

// folderNames maps a person's folder ids to names.
func (s *Server) folderNames(ctx context.Context, uid int64) map[int64]string {
	out := map[int64]string{}
	list, _ := s.store.ListFolders(ctx, uid)
	for _, f := range list {
		out[f.ID] = f.Name
	}
	return out
}

func sortedKeys(m map[string]int) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func plural(n int, one, many string) string {
	if n == 1 {
		return fmt.Sprintf("%d %s", n, one)
	}
	return fmt.Sprintf("%d %s", n, many)
}
