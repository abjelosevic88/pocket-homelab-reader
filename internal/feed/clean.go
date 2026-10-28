package feed

import (
	"bytes"
	"net/url"
	"regexp"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

var policy = func() *bluemonday.Policy {
	p := bluemonday.UGCPolicy()
	p.AllowAttrs("src", "width", "height", "allowfullscreen", "title").OnElements("iframe")
	p.AllowElements("iframe", "figure", "figcaption", "picture", "source", "video", "audio", "details", "summary", "mark", "sup", "sub", "abbr", "time")
	p.AllowAttrs("srcset", "sizes", "type", "media").OnElements("source", "img")
	p.AllowAttrs("src", "controls", "poster", "preload", "width", "height").OnElements("video", "audio")
	p.AllowAttrs("loading").OnElements("img")
	p.RequireNoReferrerOnLinks(true)
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.AllowURLSchemes("http", "https", "mailto")
	return p
}()

// Embeds that may stay in a post; everything else in an iframe is dropped.
var reEmbed = regexp.MustCompile(`^https://(www\.)?(youtube\.com|youtube-nocookie\.com|player\.vimeo\.com|w\.soundcloud\.com|open\.spotify\.com|bandcamp\.com|player\.twitch\.tv|www\.dailymotion\.com)/`)

// Sanitize makes HTML from a feed or a page safe to show: scripts, styles,
// trackers and unknown embeds go; links and pictures become absolute;
// pictures load lazily.
func Sanitize(h string, base *url.URL) string {
	if strings.TrimSpace(h) == "" {
		return ""
	}
	h = rewrite(h, base)
	out := policy.Sanitize(h)
	return strings.TrimSpace(out)
}

// rewrite resolves relative addresses, keeps only known embeds, drops
// tracking pixels and adds lazy loading.
func rewrite(h string, base *url.URL) string {
	nodes, err := html.ParseFragment(strings.NewReader(h), &html.Node{Type: html.ElementNode, Data: "div", DataAtom: atom.Div})
	if err != nil {
		return h
	}
	var walk func(n *html.Node)
	var drop []*html.Node
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch n.Data {
			case "script", "style", "noscript", "form", "button", "input", "select", "textarea", "object", "embed":
				drop = append(drop, n)
				return
			case "iframe":
				src := abs(base, attr(n, "src"))
				if strings.HasPrefix(src, "https://www.youtube.com/embed/") {
					src = strings.Replace(src, "www.youtube.com", "www.youtube-nocookie.com", 1)
				}
				if !reEmbed.MatchString(src) {
					drop = append(drop, n)
					return
				}
				setAttr(n, "src", src)
			case "img":
				src := attr(n, "src")
				if src == "" {
					for _, k := range []string{"data-src", "data-lazy-src", "data-original"} {
						if v := attr(n, k); v != "" {
							src = v
							break
						}
					}
				}
				w, hh := attr(n, "width"), attr(n, "height")
				if (w == "1" && hh == "1") || strings.Contains(src, "feeds.feedburner.com/~r/") || strings.Contains(src, "/pixel") {
					drop = append(drop, n)
					return
				}
				setAttr(n, "src", abs(base, src))
				if ss := attr(n, "srcset"); ss != "" {
					setAttr(n, "srcset", absSrcset(base, ss))
				}
				setAttr(n, "loading", "lazy")
			case "a":
				if href := attr(n, "href"); href != "" && !strings.HasPrefix(href, "#") && !strings.HasPrefix(href, "mailto:") {
					setAttr(n, "href", abs(base, href))
				}
			case "source", "video", "audio":
				if src := attr(n, "src"); src != "" {
					setAttr(n, "src", abs(base, src))
				}
				if ss := attr(n, "srcset"); ss != "" {
					setAttr(n, "srcset", absSrcset(base, ss))
				}
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	for _, n := range drop {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
	}
	var buf bytes.Buffer
	for _, n := range nodes {
		if !isDropped(n, drop) {
			_ = html.Render(&buf, n)
		}
	}
	return buf.String()
}

func isDropped(n *html.Node, drop []*html.Node) bool {
	for _, d := range drop {
		if d == n {
			return true
		}
	}
	return false
}

func attr(n *html.Node, k string) string {
	for _, a := range n.Attr {
		if a.Key == k {
			return a.Val
		}
	}
	return ""
}

func setAttr(n *html.Node, k, v string) {
	for i, a := range n.Attr {
		if a.Key == k {
			n.Attr[i].Val = v
			return
		}
	}
	n.Attr = append(n.Attr, html.Attribute{Key: k, Val: v})
}

func abs(base *url.URL, ref string) string {
	if v := absURL(base, ref); v != "" {
		return v
	}
	return ref
}

func absSrcset(base *url.URL, ss string) string {
	parts := strings.Split(ss, ",")
	for i, p := range parts {
		f := strings.Fields(strings.TrimSpace(p))
		if len(f) > 0 {
			f[0] = abs(base, f[0])
			parts[i] = strings.Join(f, " ")
		}
	}
	return strings.Join(parts, ", ")
}
