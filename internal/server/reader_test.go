package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/abjelosevic88/pocket-homelab-reader/internal/config"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

type app struct {
	t     *testing.T
	url   string
	s     *Server
	st    *store.Store
	alice string
	bob   string
	ids   map[string]int64
	site  *httptest.Server
	posts *atomic.Int32 // how many posts the test feed lists
}

const longText = `Most of what we keep in a read-later list never gets read, and that is fine. The list is a promise to a future self who has more time. ` +
	`What helps is making the list calm: one place, no noise, the text without the page around it. A good reader keeps your place and lets you mark what matters. ` +
	`Highlights turn reading into something you keep. A note next to a passage is worth more than ten saved links. ` +
	`Search ties it together: the sentence you half remember is one query away.`

// newApp starts Reader with Alice (admin) and Bob (member) and a little
// website with a feed, a page offering it and an article.
func newApp(t *testing.T, tweak func(*config.Config)) *app {
	t.Helper()
	a := &app{t: t, ids: map[string]int64{}, posts: &atomic.Int32{}}
	a.posts.Store(3)
	mux := http.NewServeMux()
	mux.HandleFunc("/feed.xml", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/rss+xml")
		var items strings.Builder
		for i := int(a.posts.Load()); i >= 1; i-- {
			title := fmt.Sprintf("Post number %d", i)
			if i == 2 {
				title = "Sponsored: buy our thing"
			}
			fmt.Fprintf(&items, `<item><guid>post-%d</guid><title>%s</title><link>%s/post/%d</link><pubDate>%s</pubDate><description><![CDATA[<p>Body of post %d about gardens.</p><script>alert(1)</script><img src="/img.png" width="1" height="1">]]></description></item>`,
				i, title, a.site.URL, i, time.Now().Add(-time.Duration(10-i)*time.Hour).Format(time.RFC1123Z), i)
		}
		fmt.Fprintf(w, `<?xml version="1.0"?><rss version="2.0"><channel><title>Test Garden</title><link>%s/</link><description>A test feed</description>%s</channel></rss>`, a.site.URL, items.String())
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head><title>Test Garden</title><link rel="alternate" type="application/rss+xml" title="Test Garden" href="/feed.xml"></head><body><p>Home</p></body></html>`)
	})
	mux.HandleFunc("/article", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head><title>On reading later</title><meta property="og:site_name" content="Garden Essays"></head><body><nav>menu menu</nav><article><h1>On reading later</h1><p>%s</p><p>%s</p></article><footer>footer</footer></body></html>`, longText, longText)
	})
	a.site = httptest.NewServer(mux)
	t.Cleanup(a.site.Close)

	cfg := testConfig(t.TempDir())
	cfg.BaseURL = "http://reader.test"
	cfg.MaxBodyBytes = 64 << 20
	cfg.FetchPrivate = true
	if tweak != nil {
		tweak(cfg)
	}
	st := openTestStore(t, cfg)
	a.st = st
	ctx := context.Background()
	for _, u := range []*store.User{{Email: "alice@example.com", Name: "Alice", Role: "admin"}, {Email: "bob@example.com", Name: "Bob", Role: "member"}} {
		if err := st.CreateUser(ctx, u); err != nil {
			t.Fatal(err)
		}
		tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
		a.ids[u.Name] = u.ID
		if u.Name == "Alice" {
			a.alice = tok
		} else {
			a.bob = tok
		}
	}
	ctx, cancel := context.WithCancel(ctx)
	t.Cleanup(cancel)
	a.s = New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	if err := a.s.Bootstrap(ctx); err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(a.s.Router())
	t.Cleanup(ts.Close)
	a.url = ts.URL
	return a
}

func (a *app) call(auth, method, path string, body any) (int, map[string]any, []any) {
	a.t.Helper()
	var rd io.Reader
	if body != nil {
		j, _ := json.Marshal(body)
		rd = bytes.NewReader(j)
	}
	req, _ := http.NewRequest(method, a.url+path, rd)
	req.Header.Set("Content-Type", "application/json")
	if auth != "" {
		req.Header.Set("Authorization", "Bearer "+auth)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	raw, _ := io.ReadAll(res.Body)
	var m map[string]any
	var l []any
	if json.Unmarshal(raw, &m) != nil {
		_ = json.Unmarshal(raw, &l)
	}
	return res.StatusCode, m, l
}

func (a *app) ok(auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code >= 300 {
		a.t.Fatalf("%s %s → %d %v", method, path, code, m)
	}
	return m
}

func (a *app) expect(want int, auth, method, path string, body any) map[string]any {
	a.t.Helper()
	code, m, _ := a.call(auth, method, path, body)
	if code != want {
		a.t.Fatalf("%s %s → %d, want %d (%v)", method, path, code, want, m)
	}
	return m
}

func num(m map[string]any, k string) float64 { v, _ := m[k].(float64); return v }

func items(m map[string]any) []map[string]any {
	var out []map[string]any
	list, _ := m["items"].([]any)
	for _, x := range list {
		out = append(out, x.(map[string]any))
	}
	return out
}

// subscribe follows the test site (by its home page: the feed is discovered).
func (a *app) subscribe(tok string) int64 {
	a.t.Helper()
	m := a.expect(201, tok, "POST", "/api/v1/feeds", map[string]any{"url": a.site.URL + "/", "folder": "Garden"})
	f := m["feed"].(map[string]any)
	return int64(num(f, "id"))
}

func TestSubscribeDiscoversAndCleans(t *testing.T) {
	a := newApp(t, nil)
	fid := a.subscribe(a.alice)
	list := items(a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/articles?feed=%d", fid), nil))
	if len(list) != 3 {
		t.Fatalf("want 3 posts, got %d", len(list))
	}
	id := int64(num(list[0], "id"))
	got := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/articles/%d", id), nil)
	content := got["content"].(string)
	if strings.Contains(content, "<script") || strings.Contains(content, "width=\"1\"") {
		t.Fatalf("content not cleaned: %s", content)
	}
	// following again is a conflict, not a second feed
	a.expect(409, a.alice, "POST", "/api/v1/feeds", map[string]any{"url": a.site.URL + "/feed.xml"})
	// a new post arrives on refresh
	a.posts.Store(4)
	res := a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/feeds/%d/refresh", fid), nil)
	if num(res, "new") != 1 {
		t.Fatalf("want 1 new post, got %v", res)
	}
	feeds := a.ok(a.alice, "GET", "/api/v1/feeds", nil)
	if fs := feeds["feeds"].([]any); len(fs) != 1 || num(fs[0].(map[string]any), "unread") != 4 {
		t.Fatalf("feeds: %v", feeds)
	}
}

func TestPerPersonIsolation(t *testing.T) {
	a := newApp(t, nil)
	fid := a.subscribe(a.alice)
	list := items(a.ok(a.alice, "GET", "/api/v1/articles?view=unread", nil))
	if len(list) == 0 {
		t.Fatal("alice has no posts")
	}
	post := int64(num(list[0], "id"))
	saved := a.expect(201, a.alice, "POST", "/api/v1/saved", map[string]any{"url": a.site.URL + "/article"})
	page := int64(num(saved["article"].(map[string]any), "id"))
	hl := a.expect(201, a.alice, "POST", fmt.Sprintf("/api/v1/articles/%d/highlights", page), map[string]any{"text": "Highlights turn reading into something you keep."})
	hid := int64(num(hl, "id"))
	rule := a.expect(201, a.alice, "POST", "/api/v1/rules", map[string]any{"words": "sponsored", "action": "read"})
	rid := int64(num(rule, "id"))

	// Bob sees none of it, and cannot touch it
	for _, p := range []string{fmt.Sprintf("/api/v1/feeds/%d", fid), fmt.Sprintf("/api/v1/articles/%d", post), fmt.Sprintf("/api/v1/articles/%d", page)} {
		a.expect(404, a.bob, "GET", p, nil)
	}
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/articles/%d", post), map[string]any{"starred": true})
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/articles/%d", page), nil)
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/feeds/%d", fid), map[string]any{"paused": true})
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/feeds/%d", fid), nil)
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/highlights/%d", hid), map[string]any{"note": "mine"})
	a.expect(404, a.bob, "DELETE", fmt.Sprintf("/api/v1/highlights/%d", hid), nil)
	a.expect(404, a.bob, "POST", fmt.Sprintf("/api/v1/articles/%d/highlights", page), map[string]any{"text": "x"})
	a.expect(404, a.bob, "PUT", fmt.Sprintf("/api/v1/rules/%d", rid), map[string]any{"words": "x", "action": "read"})
	a.expect(404, a.bob, "POST", fmt.Sprintf("/api/v1/feeds/%d/refresh", fid), nil)
	// bulk changes silently skip what is not his
	a.ok(a.bob, "POST", "/api/v1/articles/state", map[string]any{"ids": []int64{post, page}, "read": true})
	if got := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/articles/%d", post), nil); got["read"] == true {
		t.Fatal("bob marked alice's post read")
	}
	if l := items(a.ok(a.bob, "GET", "/api/v1/articles?view=all", nil)); len(l) != 0 {
		t.Fatalf("bob sees %d articles", len(l))
	}
	if l := items(a.ok(a.bob, "GET", "/api/v1/articles?q=highlights", nil)); len(l) != 0 {
		t.Fatal("bob finds alice's article in search")
	}
	_, _, hs := a.call(a.bob, "GET", "/api/v1/highlights", nil)
	if len(hs) != 0 {
		t.Fatal("bob sees alice's highlights")
	}
	// read-all for Bob does not touch Alice
	a.ok(a.bob, "POST", "/api/v1/articles/read-all", map[string]any{})
	if c := a.ok(a.alice, "GET", "/api/v1/dashboard", nil); num(c, "unread") == 0 {
		t.Fatal("bob's mark-all-read reached alice")
	}
	// the same page saved by both is two separate articles
	b := a.expect(201, a.bob, "POST", "/api/v1/saved", map[string]any{"url": a.site.URL + "/article"})
	if int64(num(b["article"].(map[string]any), "id")) == page {
		t.Fatal("bob got alice's article")
	}
	// Bob's highlights list stays his own
	_, _, hs = a.call(a.bob, "GET", "/api/v1/highlights", nil)
	if len(hs) != 0 {
		t.Fatal("bob sees highlights he did not make")
	}
}

func TestSaveExtractsSearchAndHighlights(t *testing.T) {
	a := newApp(t, nil)
	m := a.expect(201, a.alice, "POST", "/api/v1/saved", map[string]any{"url": a.site.URL + "/article", "tags": "Reading, Later"})
	art := m["article"].(map[string]any)
	if art["extract_state"] != "done" || art["site_name"] != "Garden Essays" || art["tags"] != "reading, later" {
		t.Fatalf("saved page: %v", art)
	}
	id := int64(num(art, "id"))
	// saving the same page again reuses it
	again := a.expect(200, a.alice, "POST", "/api/v1/saved", map[string]any{"url": a.site.URL + "/article"})
	if int64(num(again["article"].(map[string]any), "id")) != id || again["created"] != false {
		t.Fatalf("saved twice: %v", again)
	}
	// search finds it by a word in the text, and by prefix
	if l := items(a.ok(a.alice, "GET", "/api/v1/articles?q=promis", nil)); len(l) != 1 {
		t.Fatalf("search: %d results", len(l))
	}
	if l := items(a.ok(a.alice, "GET", "/api/v1/articles?view=saved&tag=later", nil)); len(l) != 1 {
		t.Fatal("tag filter")
	}
	a.expect(201, a.alice, "POST", fmt.Sprintf("/api/v1/articles/%d/highlights", id), map[string]any{"text": "A note next to a passage is worth more than ten saved links.", "note": "yes", "color": "green"})
	view := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/articles/%d", id), nil)
	if l, _ := view["highlight_list"].([]any); len(l) != 1 {
		t.Fatalf("highlights on the article: %v", view["highlight_list"])
	}
	// finishing it archives it
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/articles/%d", id), map[string]any{"archived": true, "progress": 1})
	if l := items(a.ok(a.alice, "GET", "/api/v1/articles?view=archive", nil)); len(l) != 1 {
		t.Fatal("archive")
	}
	if l := items(a.ok(a.alice, "GET", "/api/v1/articles?view=saved", nil)); len(l) != 0 {
		t.Fatal("still on the read-later list")
	}
	// a bad address is refused plainly
	a.expect(422, a.alice, "POST", "/api/v1/saved", map[string]any{"url": "javascript:alert(1)"})
}

func TestRulesApplyToNewPosts(t *testing.T) {
	a := newApp(t, nil)
	a.expect(201, a.alice, "POST", "/api/v1/rules", map[string]any{"words": "sponsored", "action": "read", "field": "title"})
	a.expect(201, a.alice, "POST", "/api/v1/rules", map[string]any{"words": "gardens", "action": "tag", "tag": "garden", "field": "any"})
	a.expect(400, a.alice, "POST", "/api/v1/rules", map[string]any{"words": "", "action": "read"})
	a.expect(400, a.alice, "POST", "/api/v1/rules", map[string]any{"words": "x", "action": "explode"})
	fid := a.subscribe(a.alice)
	list := items(a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/articles?feed=%d&view=all", fid), nil))
	for _, p := range list {
		if strings.HasPrefix(p["title"].(string), "Sponsored") && p["read"] != true {
			t.Fatal("sponsored post not marked read")
		}
		if !strings.Contains(p["tags"].(string), "garden") {
			t.Fatalf("post not tagged: %v", p["tags"])
		}
	}
	test := a.ok(a.alice, "POST", "/api/v1/rules/test", map[string]any{"words": "post number", "field": "title"})
	if num(test, "matches") != 2 {
		t.Fatalf("rule test: %v", test)
	}
}

func TestFirstFetchKeepsOnlyNewestUnread(t *testing.T) {
	a := newApp(t, nil)
	a.posts.Store(25)
	a.subscribe(a.alice)
	c := a.ok(a.alice, "GET", "/api/v1/dashboard", nil)
	if num(c, "unread") != firstFetchUnread {
		t.Fatalf("unread after first fetch = %v, want %d", c["unread"], firstFetchUnread)
	}
}

func TestDeleteAndPruneLeaveTombstones(t *testing.T) {
	a := newApp(t, nil)
	fid := a.subscribe(a.alice)
	list := items(a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/articles?feed=%d&view=all", fid), nil))
	victim := int64(num(list[0], "id"))
	a.expect(204, a.alice, "DELETE", fmt.Sprintf("/api/v1/articles/%d", victim), nil)
	res := a.ok(a.alice, "POST", fmt.Sprintf("/api/v1/feeds/%d/refresh", fid), nil)
	if num(res, "new") != 0 {
		t.Fatal("a deleted post came back")
	}
	// read posts older than the limit are removed; starred ones stay
	ctx := context.Background()
	uid := a.ids["Alice"]
	old := time.Now().AddDate(0, 0, -40).UTC().Format(time.RFC3339)
	_, _ = a.st.DB.Exec(`UPDATE articles SET read = 1, read_at = ? WHERE user_id = ?`, old, uid)
	keep := int64(num(list[1], "id"))
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/articles/%d", keep), map[string]any{"starred": true})
	n, err := a.st.Prune(ctx, uid, 30, 0)
	if err != nil || n != 1 {
		t.Fatalf("pruned %d (%v), want 1", n, err)
	}
	a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/articles/%d", keep), nil)
}

func TestUnsubscribeKeepsWhatYouKept(t *testing.T) {
	a := newApp(t, nil)
	fid := a.subscribe(a.alice)
	list := items(a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/articles?feed=%d&view=all", fid), nil))
	keep := int64(num(list[0], "id"))
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/articles/%d", keep), map[string]any{"starred": true})
	a.expect(204, a.alice, "DELETE", fmt.Sprintf("/api/v1/feeds/%d", fid), nil)
	got := a.ok(a.alice, "GET", fmt.Sprintf("/api/v1/articles/%d", keep), nil)
	if got["feed_id"] != nil || got["saved"] != true {
		t.Fatalf("kept post: %v", got)
	}
	if c := a.ok(a.alice, "GET", "/api/v1/dashboard", nil); num(c, "unread") != 0 {
		t.Fatal("unread posts of an unfollowed feed remain")
	}
}

func TestExportImportIdempotent(t *testing.T) {
	a := newApp(t, nil)
	a.subscribe(a.alice)
	m := a.expect(201, a.alice, "POST", "/api/v1/saved", map[string]any{"url": a.site.URL + "/article", "tags": "essays"})
	id := int64(num(m["article"].(map[string]any), "id"))
	a.expect(201, a.alice, "POST", fmt.Sprintf("/api/v1/articles/%d/highlights", id), map[string]any{"text": "Search ties it together"})
	a.expect(201, a.alice, "POST", "/api/v1/rules", map[string]any{"words": "sponsored", "action": "read"})
	req, _ := http.NewRequest("GET", a.url+"/api/v1/export.json", nil)
	req.Header.Set("Authorization", "Bearer "+a.alice)
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	raw, _ := io.ReadAll(res.Body)
	res.Body.Close()
	// into Bob's empty account, twice
	post := func() map[string]any {
		req, _ := http.NewRequest("POST", a.url+"/api/v1/import", bytes.NewReader(raw))
		req.Header.Set("Authorization", "Bearer "+a.bob)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != 200 {
			t.Fatalf("import → %d %v", res.StatusCode, out)
		}
		return out["counts"].(map[string]any)
	}
	first := post()
	if num(first, "feeds") != 1 || num(first, "saved") != 1 || num(first, "highlights") != 1 || num(first, "rules") != 1 {
		t.Fatalf("first import: %v", first)
	}
	second := post()
	if num(second, "feeds")+num(second, "saved")+num(second, "highlights")+num(second, "rules") != 0 {
		t.Fatalf("second import added: %v", second)
	}
	// OPML round trip
	req, _ = http.NewRequest("GET", a.url+"/api/v1/export.opml", nil)
	req.Header.Set("Authorization", "Bearer "+a.alice)
	res, _ = http.DefaultClient.Do(req)
	opml, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if !strings.Contains(string(opml), "Garden") || !strings.Contains(string(opml), "feed.xml") {
		t.Fatalf("opml: %s", opml)
	}
}

func TestImportFormats(t *testing.T) {
	a := newApp(t, nil)
	send := func(body string) map[string]any {
		req, _ := http.NewRequest("POST", a.url+"/api/v1/import", strings.NewReader(body))
		req.Header.Set("Authorization", "Bearer "+a.alice)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var out map[string]any
		_ = json.NewDecoder(res.Body).Decode(&out)
		if res.StatusCode != 200 {
			t.Fatalf("import → %d %v", res.StatusCode, out)
		}
		return out
	}
	kk := send(`{"bookmarks":[{"createdAt":1720000000,"title":"A saved essay","tags":["essays","reading"],"content":{"type":"link","url":"https://essays.example/one"},"note":"good","archived":false,"favourited":true},{"createdAt":1720000001,"title":"note","content":{"type":"text","text":"hi"}}]}`)
	if kk["kind"] != "karakeep" || num(kk["counts"].(map[string]any), "saved") != 1 {
		t.Fatalf("karakeep: %v", kk)
	}
	bm := send(`<!DOCTYPE NETSCAPE-Bookmark-file-1><DL><DT><H3>Cooking</H3><DL><DT><A HREF="https://food.example/bread" ADD_DATE="1700000000">Bread</A></DL><DT><A HREF="https://news.example/x">X</A></DL>`)
	if bm["kind"] != "bookmarks" || num(bm["counts"].(map[string]any), "saved") != 2 {
		t.Fatalf("bookmarks: %v", bm)
	}
	csv := send("url,title,time_added,tags,status\nhttps://csv.example/a,A,1700000000,x|y,archive\n")
	if csv["kind"] != "csv" || num(csv["counts"].(map[string]any), "saved") != 1 {
		t.Fatalf("csv: %v", csv)
	}
	opml := send(`<?xml version="1.0"?><opml version="2.0"><body><outline text="News"><outline text="Garden" type="rss" xmlUrl="` + a.site.URL + `/feed.xml"/></outline></body></opml>`)
	if opml["kind"] != "opml" || num(opml["counts"].(map[string]any), "feeds") != 1 {
		t.Fatalf("opml: %v", opml)
	}
	bread := items(a.ok(a.alice, "GET", "/api/v1/articles?view=saved&tag=cooking", nil))
	if len(bread) != 1 {
		t.Fatal("bookmark folder did not become a tag")
	}
	if l := items(a.ok(a.alice, "GET", "/api/v1/articles?view=starred", nil)); len(l) != 1 {
		t.Fatal("karakeep favourite not starred")
	}
}

func TestSettingsArePersonal(t *testing.T) {
	a := newApp(t, nil)
	a.ok(a.alice, "PUT", "/api/v1/settings", map[string]any{"font_size": 22, "notes_folder": "Reading/Highlights", "notify_url": "http://example.invalid/x", "title": "Our Reader"})
	a.ok(a.bob, "PUT", "/api/v1/settings", map[string]any{"title": "Bob's title"})
	b := a.ok(a.bob, "GET", "/api/v1/settings", nil)
	if num(b, "font_size") != 19 || b["notify_url"] != "" || b["notes_folder"] != "Reader" {
		t.Fatalf("bob got alice's settings: %v", b)
	}
	if b["title"] != "Our Reader" {
		t.Fatalf("a member changed the box title: %v", b["title"])
	}
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"font_size": 99})
	a.expect(400, a.alice, "PUT", "/api/v1/settings", map[string]any{"notes_folder": "../etc"})
}

// A reading:read link reads; reading:write saves and removes only what it saved.
func TestLinkScopes(t *testing.T) {
	links := map[string]string{"pcx_read": "reading:read", "pcx_write": "reading:write", "pcx_other": "reading:read"}
	core := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		scope, ok := links[in.Token]
		if !ok {
			io.WriteString(w, `{"active":false}`)
			return
		}
		aud := "reader"
		if in.Token == "pcx_other" {
			aud = "notes" // a token for another app
		}
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":%q,"user":{"id":1,"sub":"alice@example.com","email":"alice@example.com","name":"Alice","role":"admin"},"app":{"id":5,"slug":"day","name":"Pocket Day"}}`, scope, aud)
	}))
	t.Cleanup(core.Close)
	a := newApp(t, func(c *config.Config) { c.CoreURL = core.URL })
	a.subscribe(a.alice)
	read, write := "pcx_read", "pcx_write"
	a.expect(200, read, "GET", "/api/v1/dashboard", nil)
	a.expect(401, "pcx_other", "GET", "/api/v1/dashboard", nil)
	a.expect(200, read, "GET", "/api/v1/day/"+time.Now().Format("2006-01-02"), nil)
	a.expect(403, read, "POST", "/api/v1/saved", map[string]any{"url": a.site.URL + "/article"})
	a.expect(403, read, "PUT", "/api/v1/settings", map[string]any{"font_size": 20})
	a.expect(403, read, "GET", "/api/v1/export.json", nil)
	saved := a.expect(201, write, "POST", "/api/v1/saved", map[string]any{"url": a.site.URL + "/article"})
	art := saved["article"].(map[string]any)
	if art["source"] != "day" {
		t.Fatalf("source of a link save: %v", art["source"])
	}
	// the link may remove what it saved, nothing else
	a.expect(204, write, "DELETE", fmt.Sprintf("/api/v1/articles/%d", int64(num(art, "id"))), nil)
	list := items(a.ok(a.alice, "GET", "/api/v1/articles?view=all", nil))
	a.expect(403, write, "DELETE", fmt.Sprintf("/api/v1/articles/%d", int64(num(list[0], "id"))), nil)
}

func TestGoogleReaderAPI(t *testing.T) {
	a := newApp(t, nil)
	fid := a.subscribe(a.alice)
	form := func(path, auth string, v url.Values) (int, string) {
		req, _ := http.NewRequest("POST", a.url+"/api/greader.php"+path, strings.NewReader(v.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if auth != "" {
			req.Header.Set("Authorization", "GoogleLogin auth="+auth)
		}
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		b, _ := io.ReadAll(res.Body)
		return res.StatusCode, string(b)
	}
	get := func(path, auth string) (int, map[string]any) {
		req, _ := http.NewRequest("GET", a.url+"/api/greader.php"+path, nil)
		req.Header.Set("Authorization", "GoogleLogin auth="+auth)
		res, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer res.Body.Close()
		var m map[string]any
		_ = json.NewDecoder(res.Body).Decode(&m)
		return res.StatusCode, m
	}
	if c, _ := form("/accounts/ClientLogin", "", url.Values{"Email": {"alice@example.com"}, "Passwd": {"wrong"}}); c != 401 {
		t.Fatalf("bad login → %d", c)
	}
	c, body := form("/accounts/ClientLogin", "", url.Values{"Email": {"alice@example.com"}, "Passwd": {a.alice}})
	if c != 200 || !strings.Contains(body, "Auth="+a.alice) {
		t.Fatalf("login → %d %s", c, body)
	}
	if c, m := get("/reader/api/0/subscription/list?output=json", a.alice); c != 200 || len(m["subscriptions"].([]any)) != 1 {
		t.Fatalf("subscriptions → %d %v", c, m)
	}
	_, ids := get("/reader/api/0/stream/items/ids?s=user/-/state/com.google/reading-list&xt=user/-/state/com.google/read&n=100", a.alice)
	refs := ids["itemRefs"].([]any)
	if len(refs) != 3 {
		t.Fatalf("unread ids: %v", ids)
	}
	first := refs[0].(map[string]any)["id"].(string)
	c, body = form("/reader/api/0/edit-tag", a.alice, url.Values{"i": {first}, "a": {"user/-/state/com.google/read"}})
	if c != 200 || body != "OK" {
		t.Fatalf("edit-tag → %d %s", c, body)
	}
	_, cnt := get("/reader/api/0/unread-count?output=json", a.alice)
	if num(cnt, "max") != 2 {
		t.Fatalf("unread count: %v", cnt)
	}
	c, body = form("/reader/api/0/stream/items/contents", a.alice, url.Values{"i": {fmt.Sprintf("tag:google.com,2005:reader/item/%016x", grParseID(first))}})
	if c != 200 || !strings.Contains(body, `"user/-/state/com.google/read"`) || !strings.Contains(body, fmt.Sprintf(`"feed/%d"`, fid)) {
		t.Fatalf("contents → %d %s", c, body)
	}
	if c, body = form("/reader/api/0/mark-all-as-read", a.alice, url.Values{"s": {fmt.Sprintf("feed/%d", fid)}}); c != 200 {
		t.Fatalf("mark all → %d %s", c, body)
	}
	_, cnt = get("/reader/api/0/unread-count?output=json", a.alice)
	if num(cnt, "max") != 0 {
		t.Fatalf("unread after mark all: %v", cnt)
	}
	// Bob's token sees nothing of Alice's
	if _, m := get("/reader/api/0/subscription/list", a.bob); len(m["subscriptions"].([]any)) != 0 {
		t.Fatal("bob sees alice's subscriptions")
	}
	if c, body = form("/reader/api/0/edit-tag", a.bob, url.Values{"i": {first}, "r": {"user/-/state/com.google/read"}}); c != 200 {
		t.Fatalf("bob edit → %d", c)
	}
	if got := a.ok(a.alice, "GET", "/api/v1/articles/"+first, nil); got["read"] != true {
		t.Fatal("bob's edit-tag reached alice's post")
	}
}
