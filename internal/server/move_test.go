package server

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/config"
)

// moveLinks starts Reader behind a fake Core whose pcx_<who>_<export|import>
// tokens are Pocket Move links acting for alice@ or bob@.
func moveLinks(t *testing.T) *app {
	t.Helper()
	coreSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if r.URL.Path != "/api/v1/auth/introspect" {
			io.WriteString(w, `{}`)
			return
		}
		var in struct {
			Token string `json:"token"`
		}
		_ = json.NewDecoder(r.Body).Decode(&in)
		parts := strings.SplitN(strings.TrimPrefix(in.Token, "pcx_"), "_", 2)
		if len(parts) != 2 {
			io.WriteString(w, `{"active":false}`)
			return
		}
		scope := map[string]string{"export": core.ScopeMoveExport, "import": core.ScopeMoveImport, "read": "reading:read"}[parts[1]]
		email := parts[0] + "@example.com"
		fmt.Fprintf(w, `{"active":true,"kind":"delegated","scope":%q,"audience":"reader","user":{"id":1,"sub":%q,"email":%q,"name":%q,"role":"member"},"app":{"id":7,"slug":"move","name":"Pocket Move"}}`, scope, email, email, parts[0])
	}))
	t.Cleanup(coreSrv.Close)
	return newApp(t, func(c *config.Config) { c.CoreURL = coreSrv.URL })
}

func (a *app) raw(tok, method, path, ctype string, body []byte) (int, []byte) {
	a.t.Helper()
	req, _ := http.NewRequest(method, a.url+path, bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer "+tok)
	if ctype != "" {
		req.Header.Set("Content-Type", ctype)
	}
	res, err := http.DefaultClient.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	return res.StatusCode, b
}

func (a *app) moveImport(tok, path string, data []byte, multi bool) map[string]any {
	a.t.Helper()
	ctype, body := "text/x-opml", data
	if multi {
		var buf bytes.Buffer
		mw := multipart.NewWriter(&buf)
		fw, _ := mw.CreateFormFile("file", "upload")
		_, _ = fw.Write(data)
		_ = mw.Close()
		ctype, body = mw.FormDataContentType(), buf.Bytes()
	}
	code, out := a.raw(tok, "POST", path, ctype, body)
	if code != 200 {
		a.t.Fatalf("POST %s → %d %s", path, code, out)
	}
	var m map[string]any
	_ = json.Unmarshal(out, &m)
	return m
}

func TestMoveLinkScopes(t *testing.T) {
	a := moveLinks(t)
	exp, imp, read := "pcx_bob_export", "pcx_bob_import", "pcx_bob_read"
	for _, c := range []struct {
		tok, method, path string
		want              int
	}{
		{exp, "GET", core.PortablePath, 200},
		{exp, "GET", "/api/v1/export.json", 200},
		{exp, "GET", "/api/v1/export.opml", 200},
		{exp, "GET", "/api/v1/export.csv", 200},
		{exp, "GET", "/api/v1/highlights/export.md", 200},
		{exp, "POST", "/api/v1/import", 403},
		{exp, "POST", "/api/v1/import/opml", 403},
		{exp, "GET", "/api/v1/settings", 403},
		{exp, "GET", "/api/v1/dashboard", 403},
		{exp, "GET", "/api/v1/backup", 403},
		{imp, "GET", core.PortablePath, 200},
		{imp, "GET", "/api/v1/export.json", 403},
		{imp, "PUT", "/api/v1/settings", 403},
		{imp, "POST", "/api/v1/saved", 403},
		{imp, "POST", "/api/v1/feeds", 403},
		{read, "GET", core.PortablePath, 403},
		{read, "GET", "/api/v1/export.json", 403},
	} {
		if code, body := a.raw(c.tok, c.method, c.path, "application/json", []byte("{}")); code != c.want {
			t.Errorf("%s %s %s → %d, want %d (%s)", c.tok, c.method, c.path, code, c.want, body)
		}
	}
	_, body := a.raw(exp, "GET", core.PortablePath, "", nil)
	var p core.Portable
	if err := json.Unmarshal(body, &p); err != nil || len(p.Exports) != 4 || len(p.Imports) != 2 || p.Imports[1].Form != "" {
		t.Fatalf("portable: %s", body)
	}
}

// Move brings Alice's reading to Bob: each import adds once, and nothing
// of one person shows up for the other.
func TestMoveImportsAddOnce(t *testing.T) {
	a := moveLinks(t)
	a.subscribe(a.alice)
	m := a.expect(201, a.alice, "POST", "/api/v1/saved", map[string]any{"url": a.site.URL + "/article", "tags": "essays"})
	id := int64(num(m["article"].(map[string]any), "id"))
	a.expect(201, a.alice, "POST", fmt.Sprintf("/api/v1/articles/%d/highlights", id), map[string]any{"text": "Search ties it together"})

	code, export := a.raw("pcx_alice_export", "GET", "/api/v1/export.json", "", nil)
	if code != 200 || !strings.Contains(string(export), "/article") {
		t.Fatalf("alice's export: %d %s", code, export)
	}
	if _, bob := a.raw("pcx_bob_export", "GET", "/api/v1/export.json", "", nil); strings.Contains(string(bob), a.site.URL) {
		t.Fatalf("bob's export holds alice's reading: %s", bob)
	}

	first := a.moveImport("pcx_bob_import", "/api/v1/import", export, true)
	if first["kind"] != "pocket-reader" || num(first, "feeds") != 1 || num(first, "saved") != 1 || num(first, "highlights") != 1 {
		t.Fatalf("first import: %v", first)
	}
	second := a.moveImport("pcx_bob_import", "/api/v1/import", export, true)
	if num(second, "feeds")+num(second, "saved")+num(second, "highlights")+num(second, "rules") != 0 {
		t.Fatalf("second import added: %v", second)
	}

	opml := []byte(`<?xml version="1.0"?><opml version="2.0"><body><outline text="Blogs"><outline text="Other" type="rss" xmlUrl="https://blog.example/feed.xml"/>` +
		`<outline text="Garden" type="rss" xmlUrl="` + a.site.URL + `/feed.xml"/></outline></body></opml>`)
	o1 := a.moveImport("pcx_bob_import", "/api/v1/import/opml", opml, false)
	o2 := a.moveImport("pcx_bob_import", "/api/v1/import/opml", opml, false)
	if num(o1, "feeds") != 1 || num(o1, "already") != 1 || num(o2, "feeds") != 0 || num(o2, "already") != 2 {
		t.Fatalf("opml imports: %v then %v", o1, o2)
	}

	bobFeeds, _ := a.st.ListFeeds(t.Context(), a.ids["Bob"])
	aliceFeeds, _ := a.st.ListFeeds(t.Context(), a.ids["Alice"])
	if len(bobFeeds) != 2 || len(aliceFeeds) != 1 {
		t.Fatalf("feeds: bob %d, alice %d", len(bobFeeds), len(aliceFeeds))
	}
	if l := items(a.ok(a.bob, "GET", "/api/v1/articles?view=saved", nil)); len(l) != 1 {
		t.Fatalf("bob's saved: %v", l)
	}
	if l := items(a.ok(a.alice, "GET", "/api/v1/articles?view=saved", nil)); len(l) != 1 {
		t.Fatalf("alice's saved changed: %v", l)
	}
}
