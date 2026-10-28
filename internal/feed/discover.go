package feed

import (
	"bytes"
	"context"
	"errors"
	"html"
	"net/url"
	"regexp"
	"strings"
	"time"

	readability "codeberg.org/readeck/go-readability/v2"
)

// Found is a feed offered by a web page.
type Found struct {
	URL   string `json:"url"`
	Title string `json:"title"`
	Kind  string `json:"kind"` // rss | atom | json | youtube
}

// Probe is what an address turned out to be.
type Probe struct {
	URL      string  `json:"url"`       // after redirects
	IsFeed   bool    `json:"is_feed"`   // the address itself is a feed
	Feed     *Feed   `json:"-"`         // parsed, when it is a feed
	Feeds    []Found `json:"feeds"`     // feeds the page offers
	Title    string  `json:"title"`     // the page's or feed's title
	Excerpt  string  `json:"excerpt"`   // a line about it
	ImageURL string  `json:"image_url"` // its picture
	SiteName string  `json:"site_name"`
	Page     []byte  `json:"-"` // the page, for saving it to read later
}

var (
	reLinkTag   = regexp.MustCompile(`(?is)<link\s[^>]*>`)
	reAttr      = regexp.MustCompile(`(?is)([a-zA-Z_:.-]+)\s*=\s*("([^"]*)"|'([^']*)'|([^\s>]+))`)
	reTitle     = regexp.MustCompile(`(?is)<title[^>]*>(.*?)</title>`)
	reMeta      = regexp.MustCompile(`(?is)<meta\s[^>]*>`)
	reChannelID = regexp.MustCompile(`"(?:channelId|externalId|browseId)"\s*:\s*"(UC[A-Za-z0-9_-]{22})"`)
	rePlaylist  = regexp.MustCompile(`[?&]list=([A-Za-z0-9_-]{10,})`)
)

func attrs(tag string) map[string]string {
	out := map[string]string{}
	for _, m := range reAttr.FindAllStringSubmatch(tag, -1) {
		v := m[3]
		if v == "" {
			v = m[4]
		}
		if v == "" {
			v = m[5]
		}
		out[strings.ToLower(m[1])] = html.UnescapeString(v)
	}
	return out
}

// Probe fetches an address and tells whether it is a feed, which feeds the
// page offers, and what the page is (for saving it to read later).
func (f *Fetcher) Probe(ctx context.Context, raw string) (*Probe, error) {
	u, err := CheckURL(raw)
	if err != nil {
		return nil, err
	}
	// a YouTube playlist has a feed of its own
	if isYouTube(u.Host) {
		if m := rePlaylist.FindStringSubmatch(u.String()); m != nil && !strings.Contains(u.Path, "/watch") {
			return f.probeFeed(ctx, "https://www.youtube.com/feeds/videos.xml?playlist_id="+m[1])
		}
	}
	r, err := f.Get(ctx, u.String(), acceptFeed, "", "", 8<<20)
	if err != nil {
		return nil, err
	}
	if LooksLikeFeed(r) {
		pf, err := Parse(r.Body, r.URL)
		if err != nil {
			return nil, err
		}
		return &Probe{URL: r.URL, IsFeed: true, Feed: pf, Title: pf.Title, Excerpt: pf.Description, ImageURL: pf.IconURL}, nil
	}
	p := &Probe{URL: r.URL, Page: r.Body}
	page := string(r.Body)
	base, _ := url.Parse(r.URL)
	p.Title, p.Excerpt, p.ImageURL, p.SiteName = pageMeta(page, base)
	p.Feeds = discover(page, base)
	if isYouTube(base.Host) && !strings.Contains(base.Path, "/watch") && !strings.HasPrefix(base.Path, "/shorts/") {
		if m := reChannelID.FindStringSubmatch(page); m != nil {
			yt := "https://www.youtube.com/feeds/videos.xml?channel_id=" + m[1]
			if !hasFound(p.Feeds, yt) {
				p.Feeds = append([]Found{{URL: yt, Title: strings.TrimSuffix(p.Title, " - YouTube"), Kind: "youtube"}}, p.Feeds...)
			}
		}
	}
	for i := range p.Feeds {
		if strings.Contains(p.Feeds[i].URL, "youtube.com/feeds/videos.xml") {
			p.Feeds[i].Kind = "youtube"
		}
	}
	return p, nil
}

func (f *Fetcher) probeFeed(ctx context.Context, feedURL string) (*Probe, error) {
	r, err := f.Get(ctx, feedURL, acceptFeed, "", "", 8<<20)
	if err != nil {
		return nil, err
	}
	pf, err := Parse(r.Body, r.URL)
	if err != nil {
		return nil, err
	}
	return &Probe{URL: r.URL, IsFeed: true, Feed: pf, Title: pf.Title, Excerpt: pf.Description, ImageURL: pf.IconURL}, nil
}

// Guess tries the usual feed addresses of a site when its pages name none.
func (f *Fetcher) Guess(ctx context.Context, site string) []Found {
	u, err := CheckURL(site)
	if err != nil {
		return nil
	}
	root := u.Scheme + "://" + u.Host
	var out []Found
	for _, p := range []string{"/feed", "/rss", "/feed.xml", "/rss.xml", "/atom.xml", "/index.xml", "/feed.json", "/blog/feed", "/blog/rss.xml"} {
		c, cancel := context.WithTimeout(ctx, 8*time.Second)
		r, err := f.Get(c, root+p, acceptFeed, "", "", 4<<20)
		cancel()
		if err != nil || !LooksLikeFeed(r) {
			continue
		}
		pf, err := Parse(r.Body, r.URL)
		if err != nil {
			continue
		}
		out = append(out, Found{URL: r.URL, Title: pf.Title, Kind: "rss"})
		break
	}
	return out
}

func isYouTube(host string) bool {
	host = strings.TrimPrefix(strings.ToLower(host), "www.")
	return host == "youtube.com" || host == "m.youtube.com" || host == "youtu.be"
}

func hasFound(list []Found, u string) bool {
	for _, f := range list {
		if f.URL == u {
			return true
		}
	}
	return false
}

func discover(page string, base *url.URL) []Found {
	var out []Found
	for _, tag := range reLinkTag.FindAllString(page, -1) {
		a := attrs(tag)
		if !strings.Contains(strings.ToLower(a["rel"]), "alternate") {
			continue
		}
		kind := ""
		switch strings.ToLower(strings.TrimSpace(a["type"])) {
		case "application/rss+xml", "application/rdf+xml":
			kind = "rss"
		case "application/atom+xml":
			kind = "atom"
		case "application/feed+json", "application/json":
			if strings.Contains(strings.ToLower(a["type"]), "feed") || strings.Contains(a["href"], "feed") {
				kind = "json"
			}
		}
		if kind == "" {
			continue
		}
		href := absURL(base, a["href"])
		if href == "" || hasFound(out, href) {
			continue
		}
		title := strings.TrimSpace(a["title"])
		// comment feeds are rarely what someone wants
		if strings.Contains(strings.ToLower(title), "comments") || strings.Contains(href, "/comments/feed") {
			continue
		}
		out = append(out, Found{URL: href, Title: title, Kind: kind})
	}
	return out
}

func pageMeta(page string, base *url.URL) (title, excerpt, image, site string) {
	meta := map[string]string{}
	for _, tag := range reMeta.FindAllString(page[:min(len(page), 400_000)], -1) {
		a := attrs(tag)
		k := strings.ToLower(firstNonEmpty(a["property"], a["name"]))
		if k != "" && a["content"] != "" {
			if _, ok := meta[k]; !ok {
				meta[k] = strings.TrimSpace(a["content"])
			}
		}
	}
	title = firstNonEmpty(meta["og:title"], meta["twitter:title"])
	if title == "" {
		if m := reTitle.FindStringSubmatch(page); m != nil {
			title = clean(m[1])
		}
	}
	excerpt = Excerpt(firstNonEmpty(meta["og:description"], meta["description"], meta["twitter:description"]), 300)
	image = absURL(base, firstNonEmpty(meta["og:image"], meta["twitter:image"]))
	site = firstNonEmpty(meta["og:site_name"], meta["application-name"])
	if site == "" && base != nil {
		site = strings.TrimPrefix(base.Hostname(), "www.")
	}
	return clean(title), clean(excerpt), image, clean(site)
}

// Article is the readable part of a web page.
type Article struct {
	URL       string
	Title     string
	Author    string
	SiteName  string
	Excerpt   string
	Content   string // cleaned HTML
	ImageURL  string
	Lang      string
	Published time.Time
}

// ErrNoArticle means the page has no readable article (a home page, an app, a paywall).
var ErrNoArticle = errors.New("no readable article on that page")

// Extract downloads a page and keeps its readable article.
func (f *Fetcher) Extract(ctx context.Context, raw string) (*Article, error) {
	r, err := f.Get(ctx, raw, acceptPage, "", "", 8<<20)
	if err != nil {
		return nil, err
	}
	if r.ContentType != "" && !strings.Contains(r.ContentType, "html") && !strings.Contains(r.ContentType, "xml") && r.ContentType != "text/plain" {
		return nil, errors.New("that link is not a web page (" + r.ContentType + ")")
	}
	return ExtractPage(r.Body, r.URL)
}

// ExtractPage keeps the readable article of a downloaded page.
func ExtractPage(page []byte, pageURL string) (*Article, error) {
	base, err := url.Parse(pageURL)
	if err != nil {
		return nil, err
	}
	title, excerpt, image, site := pageMeta(string(page), base)
	a := &Article{URL: pageURL, Title: title, Excerpt: excerpt, ImageURL: image, SiteName: site}
	art, err := readability.FromReader(bytes.NewReader(page), base)
	if err != nil || art.Node == nil {
		return a, ErrNoArticle
	}
	var buf bytes.Buffer
	if err := art.RenderHTML(&buf); err != nil {
		return a, ErrNoArticle
	}
	a.Content = Sanitize(buf.String(), base)
	if len(PlainText(a.Content)) < 200 {
		a.Content = ""
		return a, ErrNoArticle
	}
	a.Title = firstNonEmpty(clean(art.Title()), a.Title)
	a.Author = clean(art.Byline())
	a.SiteName = firstNonEmpty(clean(art.SiteName()), a.SiteName)
	a.Excerpt = firstNonEmpty(a.Excerpt, Excerpt(clean(art.Excerpt()), 300))
	a.ImageURL = firstNonEmpty(a.ImageURL, absURL(base, art.ImageURL()))
	a.Lang = art.Language()
	if t, err := art.PublishedTime(); err == nil {
		a.Published = t
	}
	return a, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if strings.TrimSpace(s) != "" {
			return s
		}
	}
	return ""
}
