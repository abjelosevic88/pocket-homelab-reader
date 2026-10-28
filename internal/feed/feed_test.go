package feed

import (
	"net/url"
	"strings"
	"testing"
)

func TestSanitize(t *testing.T) {
	base, _ := url.Parse("https://blog.example/posts/1")
	in := `<p onclick="x()">Hi <a href="/about">me</a></p><script>alert(1)</script><style>p{}</style>
<img src="pic.jpg"><img src="https://t.example/pixel.gif" width="1" height="1">
<iframe src="https://evil.example/x"></iframe><iframe src="https://www.youtube.com/embed/abcdefghijk"></iframe>`
	out := Sanitize(in, base)
	for _, bad := range []string{"<script", "onclick", "<style", "evil.example", "pixel.gif"} {
		if strings.Contains(out, bad) {
			t.Errorf("kept %q: %s", bad, out)
		}
	}
	for _, good := range []string{`href="https://blog.example/about"`, `src="https://blog.example/posts/pic.jpg"`, `loading="lazy"`, "youtube-nocookie.com/embed/abcdefghijk", `rel="nofollow noreferrer noopener"`} {
		if !strings.Contains(out, good) {
			t.Errorf("missing %q: %s", good, out)
		}
	}
}

func TestParseRSSAndYouTube(t *testing.T) {
	rss := `<?xml version="1.0"?><rss version="2.0"><channel><title>Blog</title><link>https://blog.example/</link>
<item><title>Hello &amp; welcome</title><link>/hello</link><guid>g1</guid><description>&lt;p&gt;First &lt;b&gt;post&lt;/b&gt;&lt;/p&gt;</description><category>News</category></item>
<item><title></title><link>https://blog.example/two</link><description>Untitled body text here</description></item></channel></rss>`
	f, err := Parse([]byte(rss), "https://blog.example/feed.xml")
	if err != nil {
		t.Fatal(err)
	}
	if f.Title != "Blog" || len(f.Items) != 2 {
		t.Fatalf("feed: %+v", f)
	}
	var hello Item
	for _, it := range f.Items {
		if it.GUID == "g1" {
			hello = it
		}
	}
	if hello.Title != "Hello & welcome" || hello.URL != "https://blog.example/hello" || !strings.Contains(hello.Content, "<b>post</b>") || hello.Tags[0] != "news" {
		t.Fatalf("item: %+v", hello)
	}
	for _, it := range f.Items {
		if it.GUID != "g1" && (it.GUID != "https://blog.example/two" || it.Title == "") {
			t.Fatalf("untitled item: %+v", it)
		}
	}
	yt := `<?xml version="1.0"?><feed xmlns="http://www.w3.org/2005/Atom" xmlns:yt="http://www.youtube.com/xml/schemas/2015" xmlns:media="http://search.yahoo.com/mrss/">
<title>Channel</title><link rel="alternate" href="https://www.youtube.com/channel/UC1"/>
<entry><id>yt:video:abcdefghijk</id><yt:videoId>abcdefghijk</yt:videoId><title>A video</title><link rel="alternate" href="https://www.youtube.com/watch?v=abcdefghijk"/>
<published>2026-10-01T10:00:00+00:00</published><media:group><media:description>What it is about</media:description><media:thumbnail url="https://i.ytimg.com/x.jpg"/></media:group></entry></feed>`
	f, err = Parse([]byte(yt), "https://www.youtube.com/feeds/videos.xml?channel_id=UC1")
	if err != nil {
		t.Fatal(err)
	}
	it := f.Items[0]
	if f.Kind != "youtube" || !strings.Contains(it.Content, "youtube-nocookie.com/embed/abcdefghijk") || !strings.Contains(it.Content, "What it is about") || !strings.Contains(it.ImageURL, "abcdefghijk") {
		t.Fatalf("youtube item: %+v", it)
	}
}

func TestDiscover(t *testing.T) {
	base, _ := url.Parse("https://site.example/blog/")
	page := `<html><head><link rel="alternate" type="application/rss+xml" title="Posts" href="/feed.xml">
<link rel="alternate" type="application/rss+xml" title="Comments" href="/comments/feed">
<link rel="alternate" type="application/atom+xml" href="atom.xml"><meta property="og:title" content="The Blog"></head></html>`
	got := discover(page, base)
	if len(got) != 2 || got[0].URL != "https://site.example/feed.xml" || got[1].URL != "https://site.example/blog/atom.xml" {
		t.Fatalf("discover: %+v", got)
	}
	title, _, _, site := pageMeta(page, base)
	if title != "The Blog" || site != "site.example" {
		t.Fatalf("meta: %q %q", title, site)
	}
}

func TestCheckURL(t *testing.T) {
	for in, want := range map[string]string{"example.com": "https://example.com", "feed://x.example/rss": "https://x.example/rss", "http://a.example/p#frag": "http://a.example/p"} {
		u, err := CheckURL(in)
		if err != nil || u.String() != want {
			t.Errorf("%s → %v %v", in, u, err)
		}
	}
	for _, bad := range []string{"javascript:alert(1)", "ftp://x.example", "", "http://"} {
		if _, err := CheckURL(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}
