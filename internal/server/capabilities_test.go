package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/config"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// fakeCapCore is a Pocket Core that vouches for link tokens
// pcx_<alice|bob>_<search|day|widget|capture|read|write> from Pocket Hub
// and remembers the manifest Reader registers with.
type fakeCapCore struct {
	*httptest.Server
	mu       sync.Mutex
	manifest map[string]any
}

func newCapApp(t *testing.T) (*app, *fakeCapCore) {
	t.Helper()
	fc := &fakeCapCore{}
	scopes := map[string]string{"search": core.ScopeSearch, "day": core.ScopeDay, "widget": core.ScopeWidget, "capture": core.ScopeCapture,
		"read": "reading:read", "write": "reading:write"}
	fc.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/api/v1/apps/register":
			var m map[string]any
			_ = json.NewDecoder(r.Body).Decode(&m)
			fc.mu.Lock()
			fc.manifest = m
			fc.mu.Unlock()
			io.WriteString(w, `{}`)
		case "/api/v1/auth/introspect":
			var in struct{ Token string }
			_ = json.NewDecoder(r.Body).Decode(&in)
			parts := strings.Split(in.Token, "_")
			if len(parts) != 3 || parts[0] != "pcx" || scopes[parts[2]] == "" {
				io.WriteString(w, `{"active":false}`)
				return
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"active": true, "kind": "delegated", "audience": "reader", "scope": scopes[parts[2]],
				"user": map[string]any{"id": 1, "sub": parts[1] + "@example.com", "email": parts[1] + "@example.com", "name": parts[1], "role": "member"},
				"app":  map[string]any{"id": 9, "slug": "hub", "name": "Pocket Hub"}})
		default:
			io.WriteString(w, `{}`)
		}
	}))
	t.Cleanup(fc.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL, c.CoreRegistrationKey = fc.URL, "key" }), fc
}

// save puts a page of the test site on a person's read-later list.
func (a *app) save(tok, path string) (int64, string) {
	a.t.Helper()
	m := a.expect(201, tok, "POST", "/api/v1/saved", map[string]any{"url": a.site.URL + path})
	id := int64(num(m["article"].(map[string]any), "id"))
	return id, itoa(id)
}

// item finds the row with that title.
func item(t *testing.T, m map[string]any, title string) map[string]any {
	t.Helper()
	for _, it := range items(m) {
		if it["title"] == title {
			return it
		}
	}
	t.Fatalf("no %q in %v", title, m["items"])
	return nil
}

func titles(m map[string]any) string {
	var out []string
	for _, it := range items(m) {
		out = append(out, it["title"].(string))
	}
	return strings.Join(out, " | ")
}

func metrics(m map[string]any) string {
	var out []string
	list, _ := m["metrics"].([]any)
	for _, x := range list {
		out = append(out, fmt.Sprintf("%v=%v", x.(map[string]any)["label"], x.(map[string]any)["value"]))
	}
	return strings.Join(out, ", ")
}

func TestPocketSearch(t *testing.T) {
	a := newApp(t, nil)
	a.subscribe(a.alice)
	_, page := a.save(a.alice, "/article")
	res := a.ok(a.alice, "GET", "/api/v1/pocket/search?q=promis", nil)
	if len(items(res)) != 1 {
		t.Fatalf("a word of the text: %v", res)
	}
	got := item(t, res, "On reading later")
	if got["id"] != page || got["kind"] != "article" || got["url"] != "/a/"+page || got["link"] != a.site.URL+"/article" ||
		!strings.HasPrefix(got["snippet"].(string), "Garden Essays · Most of what we keep in a read-later list") {
		t.Fatalf("the saved page: %v", got)
	}
	if at, err := time.Parse(time.RFC3339Nano, got["date"].(string)); err != nil || time.Since(at) > time.Minute {
		t.Fatalf("date is when it was saved: %v", got["date"])
	}
	// feed posts are found too, newest first; a title that has the word comes before a text that has it
	posts := a.ok(a.alice, "GET", "/api/v1/pocket/search?q=post", nil)
	if titles(posts) != "Post number 3 | Post number 1 | Sponsored: buy our thing" {
		t.Fatalf("posts: %v", titles(posts))
	}
	if p := items(posts)[0]; p["snippet"] != "Test Garden · Body of post 3 about gardens." || p["link"] != a.site.URL+"/post/3" || p["kind"] != "article" {
		t.Fatalf("a post: %v", p)
	}
	// what the person kept comes before what only passed by
	list := items(a.ok(a.alice, "GET", "/api/v1/articles?q=sponsored", nil))
	a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/articles/%d", int64(num(list[0], "id"))), map[string]any{"starred": true})
	if got := titles(a.ok(a.alice, "GET", "/api/v1/pocket/search?q=gardens", nil)); got != "Sponsored: buy our thing | Post number 3 | Post number 1" {
		t.Fatalf("kept first: %v", got)
	}
	if got := titles(a.ok(a.alice, "GET", "/api/v1/pocket/search?q=gardens&limit=1", nil)); got != "Sponsored: buy our thing" {
		t.Fatalf("limit 1: %v", got)
	}
	if got := items(a.ok(a.alice, "GET", "/api/v1/pocket/search?q=number+gardens", nil)); len(got) != 2 {
		t.Fatalf("every word: %v", got)
	}
	if got := items(a.ok(a.alice, "GET", "/api/v1/pocket/search?q=%22post+number%22", nil)); len(got) != 2 {
		t.Fatalf("a phrase: %v", got)
	}
	if got := items(a.ok(a.alice, "GET", "/api/v1/pocket/search?q=%22number+post%22", nil)); len(got) != 0 {
		t.Fatalf("not that phrase: %v", got)
	}
	// no words: nothing, though the list without a search is everything
	for _, q := range []string{"", "%3F%3F", "*", "_", "%22%22", "+-+"} {
		if m := a.ok(a.alice, "GET", "/api/v1/pocket/search?q="+q, nil); m["items"] == nil || len(items(m)) != 0 {
			t.Fatalf("q=%q: %v", q, m)
		}
	}
	a.ok(a.alice, "GET", "/api/v1/pocket/search?q=post+_+%22", nil)
	// another person's reading never appears
	for _, q := range []string{"promis", "post", "gardens"} {
		if got := items(a.ok(a.bob, "GET", "/api/v1/pocket/search?q="+q, nil)); len(got) != 0 {
			t.Fatalf("bob finds alice's %q: %v", q, got)
		}
	}
	a.expect(401, "", "GET", "/api/v1/pocket/search?q=post", nil)
	// the app's own search answers as before
	if l := items(a.ok(a.alice, "GET", "/api/v1/articles?q=post", nil)); len(l) != 3 || l[1]["title"] != "Sponsored: buy our thing" {
		t.Fatalf("/articles?q= changed: %v", l)
	}
}

func TestPocketDay(t *testing.T) {
	a := newApp(t, nil)
	a.subscribe(a.alice)
	today := time.Now().Format("2006-01-02")
	day := func(tok, date string) map[string]any {
		t.Helper()
		return a.ok(tok, "GET", "/api/v1/pocket/day/"+date, nil)
	}
	id, page := a.save(a.alice, "/article")
	d := day(a.alice, today)
	if d["date"] != today || d["summary"] != "Saved 1 article for later" || len(items(d)) != 1 || d["metrics"] != nil {
		t.Fatalf("a page saved: %v", d)
	}
	it := item(t, d, "On reading later")
	if it["id"] != page || it["kind"] != "saved" || it["sub"] != "Garden Essays" || it["url"] != "/a/"+page || it["minutes"] != nil || it["done"] != nil ||
		it["time"] != time.Now().Format("15:04") && it["time"] != time.Now().Add(-time.Minute).Format("15:04") {
		t.Fatalf("saved: %v", it)
	}
	// opened and partly read
	a.ok(a.alice, "PUT", "/api/v1/articles/"+page, map[string]any{"read": true, "progress": 0.5, "note": "my own words"})
	d = day(a.alice, today)
	if it := item(t, d, "On reading later"); d["summary"] != "Read 1 article" || len(items(d)) != 1 || it["kind"] != "read" || num(it, "minutes") != 1 || it["note"] != nil ||
		metrics(d) != "Reading time=1 min" {
		t.Fatalf("read: %v", d)
	}
	a.expect(201, a.alice, "POST", "/api/v1/articles/"+page+"/highlights", map[string]any{"text": "Search ties it together"})
	if d = day(a.alice, today); d["summary"] != "Read 1 article and highlighted 1 passage" || metrics(d) != "Reading time=1 min, Highlights=1" {
		t.Fatalf("highlighted: %v", d)
	}
	// marked done
	a.ok(a.alice, "PUT", "/api/v1/articles/"+page, map[string]any{"archived": true, "progress": 1})
	d = day(a.alice, today)
	if it := item(t, d, "On reading later"); d["summary"] != "Finished 1 article and highlighted 1 passage" || it["kind"] != "finished" || it["done"] != true || num(it, "minutes") != 1 {
		t.Fatalf("finished: %v", d)
	}
	// a post read some way in counts, one only opened does not; one read to the end is finished
	posts := items(a.ok(a.alice, "GET", "/api/v1/articles?view=unread", nil))
	for i, p := range []map[string]any{{"read": true, "progress": 0.5}, {"read": true}, {"read": true, "progress": 0.95}} {
		a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/articles/%d", int64(num(posts[i], "id"))), p)
	}
	_, other := a.save(a.alice, "/other")
	d = day(a.alice, today)
	if d["summary"] != "Finished 2 articles, read 1 more, saved 1 for later and highlighted 1 passage" || len(items(d)) != 4 {
		t.Fatalf("a full day: %v", d)
	}
	if it := item(t, d, "Post number 3"); it["kind"] != "read" || it["sub"] != "Test Garden" {
		t.Fatalf("a post read: %v", it)
	}
	if it := item(t, d, "Post number 1"); it["kind"] != "finished" || it["done"] != true {
		t.Fatalf("a post read to the end: %v", it)
	}
	if it := item(t, d, "Test Garden"); it["kind"] != "saved" || it["id"] != other {
		t.Fatalf("saved for later: %v", it)
	}

	// read to the end one day, marked done on a later one: finished on the later day only
	ctx := context.Background()
	if _, err := a.st.DB.ExecContext(ctx, `UPDATE articles SET saved_at = '2026-03-08T09:00:00Z', read_at = '2026-03-09T20:15:00Z', archived_at = '2026-03-10T23:30:00Z' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if d := day(a.alice, "2026-03-08?tz=UTC"); d["summary"] != "Saved 1 article for later" || item(t, d, "On reading later")["time"] != "09:00" {
		t.Fatalf("the day it was saved: %v", d)
	}
	if d := day(a.alice, "2026-03-09?tz=UTC"); d["summary"] != "Read 1 article" || item(t, d, "On reading later")["time"] != "20:15" {
		t.Fatalf("the day it was read: %v", d)
	}
	d = day(a.alice, "2026-03-10?tz=UTC")
	if it := item(t, d, "On reading later"); d["summary"] != "Finished 1 article" || it["time"] != "23:30" || it["minutes"] != nil || d["metrics"] != nil {
		t.Fatalf("the day it was marked done: %v", d)
	}
	// the day is the asker's: 23:30 in London is half past midnight in Sarajevo
	if d := day(a.alice, "2026-03-11?tz=Europe/Sarajevo"); d["summary"] != "Finished 1 article" || item(t, d, "On reading later")["time"] != "00:30" {
		t.Fatalf("the next day in Sarajevo: %v", d)
	}
	if d := day(a.alice, "2026-03-10?tz=Europe/Sarajevo"); d["summary"] != nil || len(items(d)) != 0 {
		t.Fatalf("not that day in Sarajevo: %v", d)
	}

	// a day with nothing, and somebody else's day
	if d := day(a.alice, "2019-03-04"); d["summary"] != nil || d["items"] == nil || len(items(d)) != 0 {
		t.Fatalf("an empty day: %v", d)
	}
	if d := day(a.bob, today); d["summary"] != nil || len(items(d)) != 0 {
		t.Fatalf("bob sees alice's day: %v", d)
	}
	a.expect(400, a.alice, "GET", "/api/v1/pocket/day/yesterday", nil)
	// the old route still answers as before
	if old := a.ok(a.alice, "GET", "/api/v1/day/"+today, nil); num(old, "read_count") != 2 || num(old, "saved_count") != 1 || len(old["highlights"].([]any)) != 1 {
		t.Fatalf("/day changed: %v", old)
	}
}

func TestPocketWidget(t *testing.T) {
	a := newApp(t, nil)
	// nothing saved and no feeds: nothing to show
	if w := a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil); w["line"] != nil || w["metrics"] != nil || w["items"] == nil || len(items(w)) != 0 {
		t.Fatalf("an empty account: %v", w)
	}
	_, first := a.save(a.alice, "/article")
	_, second := a.save(a.alice, "/other")
	w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil)
	if w["line"] != "2 to read" || w["url"] != "/saved" || metrics(w) != "To read=2, Reading today=0 min" {
		t.Fatalf("widget: %v", w)
	}
	rows := items(w)
	if len(rows) != 2 || rows[0]["id"] != second || rows[0]["title"] != "Test Garden" || rows[0]["url"] != "/a/"+second ||
		rows[1]["id"] != first || rows[1]["title"] != "On reading later" || rows[1]["detail"] != "Garden Essays · 1 min" || rows[1]["url"] != "/a/"+first {
		t.Fatalf("the newest first: %v", rows)
	}
	// with feeds, what is new there is said too
	a.subscribe(a.alice)
	if w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil); w["line"] != "2 to read, 3 unread in your feeds" || metrics(w) != "To read=2, Unread in feeds=3, Reading today=0 min" {
		t.Fatalf("with feeds: %v", w)
	}
	// only the few saved last are listed
	for i := range 5 {
		a.save(a.alice, fmt.Sprintf("/more/%d", i))
	}
	if w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil); w["line"] != "7 to read, 3 unread in your feeds" || len(items(w)) != widgetRows {
		t.Fatalf("a long list: %v", w)
	}
	// done with all of it
	list := items(a.ok(a.alice, "GET", "/api/v1/articles?view=saved", nil))
	for _, x := range list {
		a.ok(a.alice, "PUT", fmt.Sprintf("/api/v1/articles/%d", int64(num(x, "id"))), map[string]any{"archived": true, "read": true, "progress": 1})
	}
	if w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil); w["line"] != "3 unread in your feeds" || len(items(w)) != 0 || metrics(w) != "To read=0, Unread in feeds=3, Reading today=1 min" {
		t.Fatalf("an empty list: %v", w)
	}
	a.ok(a.alice, "POST", "/api/v1/articles/read-all", map[string]any{})
	if w := a.ok(a.alice, "GET", "/api/v1/pocket/widget", nil); w["line"] != "Nothing waiting to be read" || len(items(w)) != 0 {
		t.Fatalf("all read: %v", w)
	}
	// still nobody else's
	if w := a.ok(a.bob, "GET", "/api/v1/pocket/widget", nil); w["line"] != nil || len(items(w)) != 0 {
		t.Fatalf("bob's widget: %v", w)
	}
}

// capture posts a quick line and returns the answer.
func capture(a *app, tok, requestID, text string, more map[string]any) (int, map[string]any) {
	body := map[string]any{"request_id": requestID, "kind": "article", "text": text}
	for k, v := range more {
		body[k] = v
	}
	code, m, _ := a.call(tok, "POST", "/api/v1/pocket/capture", body)
	return code, m
}

// later is a person's read-later list.
func (a *app) later(tok string) []map[string]any {
	a.t.Helper()
	return items(a.ok(tok, "GET", "/api/v1/articles?view=saved", nil))
}

func TestPocketCapture(t *testing.T) {
	a := newApp(t, nil)
	site := a.site.URL
	keep := func(tok, requestID, text string, more map[string]any) map[string]any {
		t.Helper()
		code, m := capture(a, tok, requestID, text, more)
		if code != 201 || m["url"] != "/a/"+m["id"].(string) {
			t.Fatalf("capture %q → %d %v", text, code, m)
		}
		got := a.ok(tok, "GET", "/api/v1/articles/"+m["id"].(string), nil)
		if got["title"] != m["title"] {
			t.Fatalf("capture %q answered %v for %v", text, m, got["title"])
		}
		return got
	}
	refuse := func(tok, requestID, text, want string) {
		t.Helper()
		if code, m := capture(a, tok, requestID, text, nil); code != 400 || m["error"] != want {
			t.Fatalf("capture %q → %d %v, want 400 %q", text, code, m, want)
		}
	}
	refuse(a.alice, "req-0000000000000000", "remember the milk", "paste a web address to save it")
	refuse(a.alice, "req-0000000000000000", "ftp://files.example/list.txt", "paste a web address to save it")
	refuse(a.alice, "req-0000000000000000", site+"/article and "+site+"/other", "one web address at a time")
	refuse(a.alice, "req-0000000000000000", "https://?x", "that is not a web address")
	if len(a.later(a.alice)) != 0 {
		t.Fatal("a refused line was kept")
	}

	// the address is saved as the app saves one, the other words are the note
	line := "Read this: " + site + "/article."
	code, first := capture(a, a.alice, "req-0000000000000001", line, nil)
	if code != 201 || first["title"] != "On reading later" || first["url"] != "/a/"+first["id"].(string) {
		t.Fatalf("capture: %d %v", code, first)
	}
	got := a.ok(a.alice, "GET", "/api/v1/articles/"+first["id"].(string), nil)
	if got["url"] != site+"/article" || got["saved"] != true || got["note"] != "Read this" || got["source"] != "app" || got["extract_state"] != "done" || got["site_name"] != "Garden Essays" {
		t.Fatalf("the article: %v", got)
	}
	// the same request again: the same answer, nothing saved, nothing moved
	a.ok(a.alice, "PUT", "/api/v1/articles/"+first["id"].(string), map[string]any{"archived": true})
	if code, again := capture(a, a.alice, "req-0000000000000001", line, nil); code != 201 || again["id"] != first["id"] || again["title"] != first["title"] || again["url"] != first["url"] ||
		len(a.later(a.alice)) != 0 {
		t.Fatalf("a retry: %d %v, list %v", code, again, a.later(a.alice))
	}
	refuse(a.alice, "req-0000000000000001", site+"/other", "this was already kept with other words")

	// a title of the person's own; the address may stand anywhere, on its own line too
	if r := keep(a.alice, "req-0000000000000002", site+"/other", map[string]any{"title": "My own title"}); r["title"] != "My own title" || r["note"] != "" || r["url"] != site+"/other" {
		t.Fatalf("with a title: %v", r)
	}
	if r := keep(a.alice, "req-0000000000000003", "for the trip\n("+site+"/third?a=1&b=2)\nfrom Ana", nil); r["url"] != site+"/third?a=1&b=2" || r["note"] != "for the trip\nfrom Ana" {
		t.Fatalf("more lines: %v", r)
	}
	if r := keep(a.alice, "req-0000000000000004", "", map[string]any{"title": "HTTP://" + strings.TrimPrefix(site, "http://") + "/fourth"}); r["title"] != "Test Garden" {
		t.Fatalf("the address as the title: %v", r)
	}

	// an address that is there already is put back on the list, not added twice; its note is left alone
	before := len(a.ok(a.alice, "GET", "/api/v1/articles?view=all", nil)["items"].([]any))
	if r := keep(a.alice, "req-0000000000000005", site+"/article - worth a second look", nil); r["id"] != got["id"] || r["archived"] != false || r["note"] != "Read this" {
		t.Fatalf("saved before: %v", r)
	}
	if r := keep(a.alice, "req-0000000000000006", site+"/other - for Sunday", nil); r["note"] != "for Sunday" || r["title"] != "My own title" {
		t.Fatalf("a note where there was none: %v", r)
	}
	if n := len(a.ok(a.alice, "GET", "/api/v1/articles?view=all", nil)["items"].([]any)); n != before || len(a.later(a.alice)) != 4 {
		t.Fatalf("articles %d (were %d), list %d", n, before, len(a.later(a.alice)))
	}

	// what the route checks itself
	a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "short", "text": site + "/x"})
	a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-0000000000000009", "kind": "feed", "text": site + "/x"})
	a.expect(400, a.alice, "POST", "/api/v1/pocket/capture", map[string]any{"request_id": "req-0000000000000009", "kind": "article"})

	// per person: bob's line makes his own article, a request id is his own
	if r := keep(a.bob, "req-0000000000000001", site+"/article", nil); r["id"] == got["id"] || r["note"] != "" {
		t.Fatalf("bob's line: %v", r)
	}
	if len(a.later(a.bob)) != 1 || len(a.later(a.alice)) != 4 {
		t.Fatalf("bob's list %d, alice's %d", len(a.later(a.bob)), len(a.later(a.alice)))
	}
}

// The same line sent several times at once, with one request id or with
// several, is one article.
func TestPocketCaptureAtOnce(t *testing.T) {
	a := newApp(t, nil)
	for round, sameID := range []bool{true, false} {
		path := fmt.Sprintf("/at-once/%d", round)
		var wg sync.WaitGroup
		ids := make([]string, 6)
		for i := range ids {
			wg.Add(1)
			go func() {
				defer wg.Done()
				requestID := fmt.Sprintf("req-at-once-%d-00000", round)
				if !sameID {
					requestID += itoa(int64(i))
				}
				body, _ := json.Marshal(map[string]any{"request_id": requestID, "text": a.site.URL + path})
				if code, out := a.raw(a.alice, "POST", "/api/v1/pocket/capture", "application/json", body); code == 201 {
					var m map[string]string
					_ = json.Unmarshal(out, &m)
					ids[i] = m["id"]
				} else {
					ids[i] = fmt.Sprintf("%d %s", code, out)
				}
			}()
		}
		wg.Wait()
		for _, id := range ids {
			if id != ids[0] || strings.Contains(id, " ") {
				t.Fatalf("same id %v: answers %v", sameID, ids)
			}
		}
		if n := len(a.later(a.alice)); n != round+1 {
			t.Fatalf("same id %v: %d on the list, want %d", sameID, n, round+1)
		}
	}
}

// A quick line is read as one web address, a title and a note.
func TestCapturePage(t *testing.T) {
	for _, c := range []struct {
		title, text     string
		url, name, note string
	}{
		{"", "https://example.com/a", "https://example.com/a", "", ""},
		{"", "Read this: https://example.com/a.", "https://example.com/a", "", "Read this"},
		{"", "https://example.com/a - for the trip", "https://example.com/a", "", "for the trip"},
		{"", "(see https://example.com/a)", "https://example.com/a", "", "see"},
		{"", "<https://example.com/a>", "https://example.com/a", "", ""},
		{"", "[A page](https://example.com/a)", "https://example.com/a", "", "A page"},
		{"", "https://en.wikipedia.org/wiki/Rust_(programming_language), for work", "https://en.wikipedia.org/wiki/Rust_(programming_language)", "", "for work"},
		{"", "https://example.com/a?x=1&y=2#part", "https://example.com/a?x=1&y=2", "", ""},
		{"", "Ana said: read it.\nhttps://example.com/a\n- before Friday", "https://example.com/a", "", "Ana said: read it.\n- before Friday"},
		{"My  title", "https://example.com/a", "https://example.com/a", "My title", ""},
		{"https://example.com/a", "worth it", "https://example.com/a", "", "worth it"},
		{"Look: https://example.com/a", "https://example.com/a", "https://example.com/a", "Look", ""},
	} {
		got, err := capturePage(core.CaptureInput{Title: c.title, Text: c.text})
		if err != nil || got.URL != c.url || got.Title != c.name || got.Note != c.note {
			t.Errorf("%q / %q → %+v (%v), want %q, %q, %q", c.title, c.text, got, err, c.url, c.name, c.note)
		}
	}
	for text, want := range map[string]string{
		"remember the milk":                       "paste a web address to save it",
		"example.com/a":                           "paste a web address to save it",
		"https://example.com/a https://b.example": "one web address at a time",
	} {
		if _, err := capturePage(core.CaptureInput{Text: text}); !errors.Is(err, core.ErrCapture) || !strings.HasSuffix(err.Error(), ": "+want) {
			t.Errorf("%q → %v, want %q", text, err, want)
		}
	}
}

// A kept line tells the Core bus once, as a save in the app does; a retry not again.
func TestPocketCaptureEvent(t *testing.T) {
	site := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<html><head><title>A page</title></head><body><article><p>%s</p></article></body></html>`, longText)
	}))
	t.Cleanup(site.Close)
	cfg := testConfig(t.TempDir())
	cfg.CoreURL, cfg.FetchPrivate = "http://core.invalid", true
	st := openTestStore(t, cfg)
	s := New(cfg, st, slog.New(slog.NewTextHandler(io.Discard, nil)), nil)
	ctx := context.Background()
	if err := s.core.Init(ctx); err != nil {
		t.Fatal(err)
	}
	u := &store.User{Email: "ana@example.com", Name: "Ana", Role: "admin"}
	if err := st.CreateUser(ctx, u); err != nil {
		t.Fatal(err)
	}
	tok, _, _ := st.CreateAPIToken(ctx, u.ID, "t")
	ts := httptest.NewServer(s.Router())
	t.Cleanup(ts.Close)
	a := &app{t: t, url: ts.URL, s: s, st: st}
	for range 2 {
		if code, m := capture(a, tok, "req-0000000000000001", site.URL+"/page", nil); code != 201 {
			t.Fatalf("capture → %d %v", code, m)
		}
	}
	rows, err := st.DB.QueryContext(ctx, `SELECT body FROM pocket_outbox ORDER BY created_at, rowid`)
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	saved := 0
	for rows.Next() {
		var b []byte
		if err := rows.Scan(&b); err != nil {
			t.Fatal(err)
		}
		var ev struct {
			Topic   string
			Payload map[string]any
		}
		if err := json.Unmarshal(b, &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Topic != "reader.saved" {
			continue
		}
		saved++
		if ev.Payload["title"] != "A page" || ev.Payload["url"] != site.URL+"/page" || ev.Payload["source"] != "app" || ev.Payload["user_email"] != "ana@example.com" {
			t.Errorf("payload = %v", ev.Payload)
		}
	}
	if saved != 1 {
		t.Fatalf("reader.saved events = %d, want 1", saved)
	}
}

// A link token with one pocket:* scope reaches its one route and nothing else.
func TestPocketLinkTokens(t *testing.T) {
	a, _ := newCapApp(t)
	a.subscribe(a.alice)
	_, page := a.save(a.alice, "/article")
	a.ok(a.alice, "PUT", "/api/v1/articles/"+page, map[string]any{"note": "my own words"})
	today := time.Now().Format("2006-01-02")
	line := func(requestID string) map[string]any {
		return map[string]any{"request_id": requestID, "kind": "article", "text": a.site.URL + "/other"}
	}
	for _, c := range []struct {
		tok, method, path string
		body              any
		want              int
	}{
		{"pcx_alice_search", "GET", "/api/v1/pocket/search?q=reading", nil, 200},
		{"pcx_alice_day", "GET", "/api/v1/pocket/day/" + today, nil, 200},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/widget", nil, 200},
		{"pcx_alice_capture", "POST", "/api/v1/pocket/capture", line("req-0000000000000001"), 201},
		{"pcx_alice_widget", "GET", "/api/v1/auth/me", nil, 200},
		// another capability's route
		{"pcx_alice_search", "GET", "/api/v1/pocket/day/" + today, nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_search", "POST", "/api/v1/pocket/capture", line("req-0000000000000002"), 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/search?q=reading", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/search?q=reading", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/pocket/day/" + today, nil, 403},
		{"pcx_alice_widget", "POST", "/api/v1/pocket/capture", line("req-0000000000000003"), 403},
		{"pcx_alice_capture", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_capture", "GET", "/api/v1/pocket/search?q=reading", nil, 403},
		// the rest of the app
		{"pcx_alice_widget", "GET", "/api/v1/dashboard", nil, 403},
		{"pcx_alice_widget", "GET", "/api/v1/articles?view=saved", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/articles?q=reading", nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/articles/" + page, nil, 403},
		{"pcx_alice_search", "GET", "/api/v1/highlights", nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/day/" + today, nil, 403},
		{"pcx_alice_day", "GET", "/api/v1/feeds", nil, 403},
		{"pcx_alice_capture", "POST", "/api/v1/saved", map[string]any{"url": a.site.URL + "/x"}, 403},
		{"pcx_alice_capture", "POST", "/api/v1/feeds", map[string]any{"url": a.site.URL + "/"}, 403},
		{"pcx_alice_capture", "PUT", "/api/v1/articles/" + page, map[string]any{"title": "x"}, 403},
		{"pcx_alice_capture", "DELETE", "/api/v1/articles/" + page, nil, 403},
		{"pcx_alice_capture", "GET", "/api/v1/settings", nil, 403},
		{"pcx_alice_capture", "GET", "/api/v1/export.json", nil, 403},
		// the app's own scopes do not open the new routes
		{"pcx_alice_read", "GET", "/api/v1/pocket/search?q=reading", nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/day/" + today, nil, 403},
		{"pcx_alice_read", "GET", "/api/v1/pocket/widget", nil, 403},
		{"pcx_alice_write", "POST", "/api/v1/pocket/capture", line("req-0000000000000004"), 403},
		{"pcx_alice_read", "GET", "/api/v1/articles?q=reading", nil, 200},
		{"pcx_nobody", "GET", "/api/v1/pocket/widget", nil, 401},
	} {
		if code, m, _ := a.call(c.tok, c.method, c.path, c.body); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%v)", c.tok, c.method, c.path, code, c.want, m)
		}
	}
	// the token acts for its person only, and never shows her own notes
	found := a.ok("pcx_alice_search", "GET", "/api/v1/pocket/search?q=reading", nil)
	if got := items(found); len(got) != 1 || got[0]["title"] != "On reading later" || strings.Contains(fmt.Sprint(found), "my own words") {
		t.Fatalf("alice through the link: %v", found)
	}
	if d := a.ok("pcx_alice_day", "GET", "/api/v1/pocket/day/"+today, nil); len(items(d)) != 2 || strings.Contains(fmt.Sprint(d), "my own words") {
		t.Fatalf("alice's day through the link: %v", d)
	}
	if got := items(a.ok("pcx_bob_search", "GET", "/api/v1/pocket/search?q=reading", nil)); len(got) != 0 {
		t.Fatalf("bob through the link finds alice's page: %v", got)
	}
	if w := a.ok("pcx_bob_widget", "GET", "/api/v1/pocket/widget", nil); len(items(w)) != 0 {
		t.Fatalf("bob's widget through the link: %v", w)
	}
	if d := a.ok("pcx_bob_day", "GET", "/api/v1/pocket/day/"+today, nil); len(items(d)) != 0 {
		t.Fatalf("bob's day through the link: %v", d)
	}
	// only the one allowed capture landed, saved as the app that sent it; a retry adds nothing
	if code, m := capture(a, "pcx_alice_capture", "req-0000000000000001", a.site.URL+"/other", nil); code != 201 {
		t.Fatalf("retry through the link: %d %v", code, m)
	}
	list := a.later(a.alice)
	if len(list) != 2 || list[0]["url"] != a.site.URL+"/other" || list[0]["source"] != "hub" {
		t.Fatalf("alice's list: %v", list)
	}
	if len(a.later(a.bob)) != 0 {
		t.Fatalf("bob's list: %v", a.later(a.bob))
	}
}

func TestPocketLinkScopes(t *testing.T) {
	req := func(m, p string) *http.Request { return httptest.NewRequest(m, p, nil) }
	for _, c := range []struct {
		scope string
		r     *http.Request
		want  bool
	}{
		{core.ScopeSearch, req("GET", "/api/v1/pocket/search?q=x"), true},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/2026-01-02"), true},
		{core.ScopeWidget, req("GET", "/api/v1/pocket/widget"), true},
		{core.ScopeCapture, req("POST", "/api/v1/pocket/capture"), true},
		{core.ScopeWidget, req("GET", "/api/v1/auth/me"), true},
		{core.ScopeSearch, req("POST", "/api/v1/pocket/search"), false},
		{core.ScopeDay, req("GET", "/api/v1/pocket/day/today"), false},
		{core.ScopeDay, req("GET", "/api/v1/day/2026-01-02"), false},
		{core.ScopeWidget, req("GET", "/api/v1/dashboard"), false},
		{core.ScopeCapture, req("GET", "/api/v1/pocket/capture"), false},
		{core.ScopeCapture, req("POST", "/api/v1/saved"), false},
		{"reading:read", req("GET", "/api/v1/pocket/widget"), false},
		{"reading:read", req("GET", "/api/v1/pocket/search?q=x"), false},
		{"reading:write", req("POST", "/api/v1/pocket/capture"), false},
		{"reading:read", req("GET", "/api/v1/day/2026-01-02"), true},
		{"reading:write", req("POST", "/api/v1/saved"), true},
	} {
		if got := linkAllows([]string{c.scope}, c.r); got != c.want {
			t.Errorf("%s %s %s = %v, want %v", c.scope, c.r.Method, c.r.URL.Path, got, c.want)
		}
	}
}

// Reader tells Core what it can do, so Search, Day and Hub find it on their own.
func TestManifestCapabilities(t *testing.T) {
	a, fc := newCapApp(t)
	if err := a.s.registerWithCore(context.Background()); err != nil {
		t.Fatal(err)
	}
	fc.mu.Lock()
	m := fc.manifest
	fc.mu.Unlock()
	if m["slug"] != "reader" || m["category"] != "Media" {
		t.Fatalf("manifest: %v %v", m["slug"], m["category"])
	}
	search, _ := m["search"].(map[string]any)
	day, _ := m["day"].(map[string]any)
	widget, _ := m["widget"].(map[string]any)
	cap, _ := m["capture"].(map[string]any)
	if search == nil || fmt.Sprint(search["bangs"]) != "[reader read]" || fmt.Sprint(search["kinds"]) != "[article]" ||
		day == nil || day["label"] != "Reader" || day["one"] != "article" || day["many"] != "articles" || widget == nil || widget["title"] != "Read later" || cap == nil {
		t.Fatalf("capabilities: %v %v %v %v", search, day, widget, cap)
	}
	if k := cap["kinds"].([]any)[0].(map[string]any); len(cap["kinds"].([]any)) != 1 || k["kind"] != "article" || fmt.Sprint(k["fields"]) != "[title]" {
		t.Fatalf("capture kinds: %v", cap)
	}
	offered := map[string]bool{}
	for _, o := range m["offers"].([]any) {
		offered[o.(map[string]any)["scope"].(string)] = true
	}
	for _, sc := range []string{"reading:read", "reading:write", core.ScopeMoveExport, core.ScopeMoveImport, core.ScopeSearch, core.ScopeDay, core.ScopeWidget, core.ScopeCapture} {
		if !offered[sc] {
			t.Errorf("the manifest does not offer %s", sc)
		}
	}
}
