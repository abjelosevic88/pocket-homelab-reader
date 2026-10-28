package feed

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"html"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/mmcdole/gofeed"
	ext "github.com/mmcdole/gofeed/extensions"
)

// Feed is a parsed feed.
type Feed struct {
	Title       string
	SiteURL     string
	Description string
	IconURL     string
	Kind        string // web | youtube | podcast
	Items       []Item
}

// Item is one post of a feed.
type Item struct {
	GUID      string
	URL       string
	Title     string
	Author    string
	Content   string // cleaned HTML
	Excerpt   string // plain text when the feed only sends a summary
	ImageURL  string
	MediaURL  string
	MediaType string
	Published time.Time
	Tags      []string
}

// ErrNotFeed means the document is not a feed.
var ErrNotFeed = errors.New("that is not a feed")

// LooksLikeFeed reports whether a downloaded document is a feed rather than a web page.
func LooksLikeFeed(r *Response) bool {
	switch r.ContentType {
	case "application/rss+xml", "application/atom+xml", "application/feed+json", "application/rdf+xml":
		return true
	case "text/html", "application/xhtml+xml":
		return false
	}
	head := bytes.ToLower(bytes.TrimSpace(r.Body[:min(len(r.Body), 2048)]))
	return bytes.Contains(head, []byte("<rss")) || bytes.Contains(head, []byte("<feed")) || bytes.Contains(head, []byte("<rdf:rdf")) ||
		(bytes.HasPrefix(head, []byte("{")) && bytes.Contains(head, []byte("jsonfeed.org")))
}

// Parse reads a feed document fetched from feedURL.
func Parse(body []byte, feedURL string) (*Feed, error) {
	p := gofeed.NewParser()
	gf, err := p.Parse(bytes.NewReader(body))
	if err != nil {
		return nil, ErrNotFeed
	}
	base, _ := url.Parse(feedURL)
	f := &Feed{Title: clean(gf.Title), SiteURL: absURL(base, gf.Link), Description: Excerpt(PlainText(gf.Description), 400), Kind: "web"}
	if gf.Image != nil {
		f.IconURL = absURL(base, gf.Image.URL)
	}
	if gf.ITunesExt != nil {
		f.Kind = "podcast"
		if f.IconURL == "" {
			f.IconURL = gf.ITunesExt.Image
		}
	}
	if strings.Contains(feedURL, "youtube.com/feeds/videos.xml") {
		f.Kind = "youtube"
	}
	if f.SiteURL == "" || f.SiteURL == feedURL {
		if base != nil {
			f.SiteURL = base.Scheme + "://" + base.Host + "/"
		}
	}
	siteBase, _ := url.Parse(f.SiteURL)
	if siteBase == nil {
		siteBase = base
	}
	for _, gi := range gf.Items {
		if gi == nil {
			continue
		}
		it := Item{Title: clean(gi.Title), URL: absURL(siteBase, gi.Link)}
		if it.URL == "" && len(gi.Links) > 0 {
			it.URL = absURL(siteBase, gi.Links[0])
		}
		if gi.Author != nil {
			it.Author = clean(gi.Author.Name)
		} else if len(gi.Authors) > 0 && gi.Authors[0] != nil {
			it.Author = clean(gi.Authors[0].Name)
		} else if gi.DublinCoreExt != nil && len(gi.DublinCoreExt.Creator) > 0 {
			it.Author = clean(gi.DublinCoreExt.Creator[0])
		}
		switch {
		case gi.PublishedParsed != nil:
			it.Published = *gi.PublishedParsed
		case gi.UpdatedParsed != nil:
			it.Published = *gi.UpdatedParsed
		}
		if it.Published.IsZero() || it.Published.After(time.Now().Add(48*time.Hour)) {
			it.Published = time.Now()
		}
		body := gi.Content
		if strings.TrimSpace(PlainText(body)) == "" || len(gi.Description) > len(body)*2 {
			body = gi.Description
		}
		if gi.Image != nil {
			it.ImageURL = absURL(siteBase, gi.Image.URL)
		}
		for _, e := range gi.Enclosures {
			if e == nil || e.URL == "" {
				continue
			}
			switch {
			case strings.HasPrefix(e.Type, "image/") && it.ImageURL == "":
				it.ImageURL = absURL(siteBase, e.URL)
			case strings.HasPrefix(e.Type, "audio/") || strings.HasPrefix(e.Type, "video/"):
				if it.MediaURL == "" {
					it.MediaURL, it.MediaType = absURL(siteBase, e.URL), e.Type
				}
			}
		}
		if thumb, desc := mediaExt(gi.Extensions); thumb != "" || desc != "" {
			if it.ImageURL == "" {
				it.ImageURL = thumb
			}
			if strings.TrimSpace(PlainText(body)) == "" && desc != "" {
				body = "<p>" + strings.ReplaceAll(html.EscapeString(desc), "\n", "<br>") + "</p>"
			}
		}
		if vid := youTubeID(gi.Extensions, it.URL); vid != "" {
			it.ImageURL = "https://i.ytimg.com/vi/" + vid + "/hqdefault.jpg"
			body = `<p><iframe src="https://www.youtube-nocookie.com/embed/` + vid + `" width="560" height="315" allowfullscreen></iframe></p>` + body
			f.Kind = "youtube"
		}
		it.Content = Sanitize(body, siteBase)
		if it.ImageURL == "" {
			it.ImageURL = firstImage(it.Content)
		}
		for _, c := range gi.Categories {
			if c = strings.ToLower(strings.TrimSpace(c)); c != "" && len(c) < 40 && len(it.Tags) < 6 {
				it.Tags = append(it.Tags, c)
			}
		}
		it.GUID = strings.TrimSpace(gi.GUID)
		if it.GUID == "" {
			it.GUID = it.URL
		}
		if it.GUID == "" {
			sum := sha1.Sum([]byte(it.Title + "\x00" + PlainText(it.Content)))
			it.GUID = "sha1:" + hex.EncodeToString(sum[:])
		}
		if len(it.GUID) > 500 {
			sum := sha1.Sum([]byte(it.GUID))
			it.GUID = "sha1:" + hex.EncodeToString(sum[:])
		}
		if it.Title == "" {
			it.Title = Excerpt(PlainText(it.Content), 80)
		}
		f.Items = append(f.Items, it)
	}
	sort.SliceStable(f.Items, func(i, j int) bool { return f.Items[i].Published.After(f.Items[j].Published) })
	return f, nil
}

func mediaExt(e ext.Extensions) (thumb, desc string) {
	m := e["media"]
	if m == nil {
		return "", ""
	}
	pick := func(set map[string][]ext.Extension) {
		if ts := set["thumbnail"]; len(ts) > 0 && thumb == "" {
			thumb = ts[0].Attrs["url"]
		}
		if cs := set["content"]; len(cs) > 0 && thumb == "" {
			for _, c := range cs {
				if strings.HasPrefix(c.Attrs["type"], "image/") || c.Attrs["medium"] == "image" {
					thumb = c.Attrs["url"]
					break
				}
			}
		}
		if ds := set["description"]; len(ds) > 0 && desc == "" {
			desc = strings.TrimSpace(ds[0].Value)
		}
	}
	pick(m)
	for _, g := range m["group"] {
		pick(g.Children)
	}
	return thumb, desc
}

var reYouTube = regexp.MustCompile(`(?:youtube\.com/(?:watch\?v=|shorts/|embed/)|youtu\.be/)([A-Za-z0-9_-]{11})`)

func youTubeID(e ext.Extensions, link string) string {
	if yt := e["yt"]; yt != nil {
		if v := yt["videoId"]; len(v) > 0 && len(v[0].Value) == 11 {
			return v[0].Value
		}
	}
	if m := reYouTube.FindStringSubmatch(link); m != nil {
		return m[1]
	}
	return ""
}

var (
	reTags  = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpace = regexp.MustCompile(`\s+`)
	reImg   = regexp.MustCompile(`(?i)<img[^>]+src="([^"]+)"`)
)

func clean(s string) string {
	return strings.TrimSpace(reSpace.ReplaceAllString(html.UnescapeString(reTags.ReplaceAllString(s, " ")), " "))
}

// PlainText turns HTML into one line of text.
func PlainText(h string) string {
	h = strings.NewReplacer("</p>", " </p>", "<br", " <br", "</li>", " </li>", "</div>", " </div>").Replace(h)
	return clean(h)
}

// Excerpt is the first n runes of text, cut at a word.
func Excerpt(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	cut := string(r[:n])
	if i := strings.LastIndex(cut, " "); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:-–—") + "…"
}

func firstImage(h string) string {
	if m := reImg.FindStringSubmatch(h); m != nil && strings.HasPrefix(m[1], "http") && !strings.Contains(m[1], "feedburner") && !strings.Contains(m[1], "pixel") {
		return html.UnescapeString(m[1])
	}
	return ""
}

func absURL(base *url.URL, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return ""
	}
	u, err := url.Parse(ref)
	if err != nil {
		return ""
	}
	if base != nil {
		u = base.ResolveReference(u)
	}
	if u.Scheme != "http" && u.Scheme != "https" {
		return ""
	}
	return u.String()
}
