// Package feed fetches and reads the web for Pocket Reader: RSS, Atom and
// JSON feeds (with conditional requests), feed discovery on web pages and
// YouTube channels, the readable article of a page, and HTML cleaning so a
// post can be shown safely.
package feed

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"syscall"
	"time"
)

// ErrBlocked is returned for addresses the fetcher refuses (private networks).
var ErrBlocked = errors.New("that address is on a private network")

// ErrNotModified means the feed has not changed since the last check.
var ErrNotModified = errors.New("not modified")

// Fetcher downloads feeds and pages.
type Fetcher struct {
	UserAgent    string
	AllowPrivate bool // let feeds on the local network through (RSS-Bridge, a NAS)
	Timeout      time.Duration
	client       *http.Client
}

// New creates a fetcher.
func New(userAgent string, allowPrivate bool, timeout time.Duration) *Fetcher {
	f := &Fetcher{UserAgent: userAgent, AllowPrivate: allowPrivate, Timeout: timeout}
	d := &net.Dialer{Timeout: 10 * time.Second}
	if !allowPrivate {
		d.Control = func(network, address string, _ syscall.RawConn) error {
			host, _, err := net.SplitHostPort(address)
			if err != nil {
				return err
			}
			if ip := net.ParseIP(host); ip != nil && privateIP(ip) {
				return ErrBlocked
			}
			return nil
		}
	}
	t := &http.Transport{Proxy: http.ProxyFromEnvironment, DialContext: d.DialContext, TLSHandshakeTimeout: 10 * time.Second,
		ResponseHeaderTimeout: 25 * time.Second, MaxIdleConns: 16, MaxIdleConnsPerHost: 2, IdleConnTimeout: 60 * time.Second, ForceAttemptHTTP2: true}
	if timeout == 0 {
		timeout = 40 * time.Second
	}
	f.client = &http.Client{Transport: t, Timeout: timeout, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 8 {
			return errors.New("too many redirects")
		}
		return nil
	}}
	return f
}

var cgnat = &net.IPNet{IP: net.IPv4(100, 64, 0, 0), Mask: net.CIDRMask(10, 32)}

func privateIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast() || cgnat.Contains(ip)
}

// CheckURL accepts http(s) addresses; a bare "example.com" gets https://.
func CheckURL(raw string) (*url.URL, error) {
	raw = strings.TrimSpace(raw)
	if raw != "" && !strings.Contains(raw, "://") {
		raw = "https://" + raw
	}
	if strings.HasPrefix(raw, "feed://") {
		raw = "https://" + strings.TrimPrefix(raw, "feed://")
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || strings.ContainsAny(u.Host, " <>\"") {
		return nil, errors.New("that is not a web address")
	}
	u.Fragment = ""
	return u, nil
}

// Response is a downloaded document.
type Response struct {
	Body         []byte
	ContentType  string // media type only
	URL          string // after redirects
	ETag         string
	LastModified string
}

// Get downloads raw. With etag / lastModified set it asks for changes only
// and returns ErrNotModified when there are none.
func (f *Fetcher) Get(ctx context.Context, raw, accept, etag, lastModified string, limit int64) (*Response, error) {
	u, err := CheckURL(raw)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	ua := f.UserAgent
	if ua == "" {
		ua = "Mozilla/5.0 (compatible; PocketReader/1.0)"
	}
	req.Header.Set("User-Agent", ua)
	req.Header.Set("Accept", accept)
	req.Header.Set("Accept-Language", "en,*;q=0.5")
	if etag != "" {
		req.Header.Set("If-None-Match", etag)
	}
	if lastModified != "" {
		req.Header.Set("If-Modified-Since", lastModified)
	}
	res, err := f.client.Do(req)
	if err != nil {
		if errors.Is(err, ErrBlocked) {
			return nil, ErrBlocked
		}
		var ne net.Error
		if errors.As(err, &ne) && ne.Timeout() {
			return nil, errors.New("the site took too long to answer")
		}
		var dnsErr *net.DNSError
		if errors.As(err, &dnsErr) {
			return nil, fmt.Errorf("there is no site at %s", u.Host)
		}
		return nil, err
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusNotModified {
		return nil, ErrNotModified
	}
	if res.StatusCode >= 400 {
		switch res.StatusCode {
		case 401, 403:
			return nil, fmt.Errorf("the site refused (HTTP %d)", res.StatusCode)
		case 404, 410:
			return nil, fmt.Errorf("nothing at that address any more (HTTP %d)", res.StatusCode)
		case 429:
			return nil, errors.New("the site asks to slow down (HTTP 429)")
		}
		return nil, fmt.Errorf("the site answered HTTP %d", res.StatusCode)
	}
	if limit <= 0 {
		limit = 10 << 20
	}
	b, err := io.ReadAll(io.LimitReader(res.Body, limit))
	if err != nil {
		return nil, err
	}
	mt, _, _ := mime.ParseMediaType(res.Header.Get("Content-Type"))
	return &Response{Body: b, ContentType: strings.ToLower(mt), URL: res.Request.URL.String(), ETag: res.Header.Get("ETag"), LastModified: res.Header.Get("Last-Modified")}, nil
}

const acceptFeed = "application/rss+xml, application/atom+xml, application/feed+json, application/json;q=0.9, application/xml;q=0.9, text/xml;q=0.9, text/html;q=0.7, */*;q=0.5"
const acceptPage = "text/html,application/xhtml+xml;q=0.9,*/*;q=0.5"
