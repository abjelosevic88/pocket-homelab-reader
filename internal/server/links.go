package server

import (
	"context"
	"errors"
	"net/http"
	"regexp"
	"strings"

	core "github.com/abjelosevic88/pocket-homelab-core/sdk"
	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// Pocket Core app links (docs/links.md in pocket-homelab-core), Reader as the
// target: another Pocket app the person allowed in Core may act here for
// them with a pcx_ token, only within these scopes. Every route not listed
// in linkRoutes, portable or capabilities stays closed to it (403).

var linkOffers = []core.Offer{
	{Scope: "reading:read", Name: "See what you read", Description: "Read what you read, saved and highlighted (a day at a time for your journal), your read-later list and your feeds — for your journal, notes and dashboards. Never your own notes on articles."},
	{Scope: "reading:write", Name: "Save to read later", Description: "Put pages on your read-later list (a link copied in another app, a page from a project) and follow feeds. It may remove only what it saved itself."},
}

// portable is what Pocket Move may copy out and bring in for a person
// (docs/move.md in pocket-homelab-core); Register adds move:export/import.
var portable = &core.Portable{
	Exports: []core.PortableExport{
		{Path: "/api/v1/export.json", File: "reader.json", Kind: core.KindPocket, Title: "Your feeds and folders, every page you kept with its text, your highlights and rules"},
		{Path: "/api/v1/export.opml", File: "feeds.opml", Kind: core.KindOPMLFeeds, Title: "The feeds you follow, in their folders, as OPML for any feed reader"},
		{Path: "/api/v1/export.csv", File: "saved.csv", Kind: core.KindCSV, Title: "Your read-later list and archive as a table"},
		{Path: "/api/v1/highlights/export.md", File: "highlights.md", Kind: core.KindMarkdown, Title: "Your highlights and notes, article by article"},
	},
	Imports: []core.PortableImport{
		{Path: "/api/v1/import", Kind: core.KindPocket, Form: "file", Title: "Adds the feeds, kept pages, highlights and rules from another box; what you already have is left alone"},
		{Path: "/api/v1/import/opml", Kind: core.KindOPMLFeeds, ContentType: "text/x-opml", Title: "Follows every feed in the OPML file, in its folder; feeds you already follow are skipped"},
	},
}

// capabilities are what Pocket Search, Pocket Day and Pocket Hub may ask
// Reader (docs/capabilities.md in pocket-homelab-core); Register adds the
// pocket:* offers. The routes are in capabilities.go.
var capabilities = core.Capabilities{
	Search: &core.Searchable{What: "The pages you saved and the posts from your feeds, by title and text",
		Kinds: []string{"article"}, Bangs: []string{"reader", "read"}},
	Day: &core.DayFeed{Label: "Reader", One: "article", Many: "articles", Unit: "count",
		What: "The articles you saved, read or finished that day"},
	Widget: &core.Widget{Title: "Read later", What: "How many articles wait on your read-later list, and the ones you saved last"},
	Capture: &core.QuickCapture{Kinds: []core.CaptureKind{{Kind: "article", Label: "Article",
		What: "A web address saved to your read-later list; any other words become your note on it", Fields: []string{"title"}}}},
}

type linkRoute struct {
	methods string
	path    *regexp.Regexp
	scope   string // "" = any linked app
}

var linkRoutes = []linkRoute{
	{"GET", regexp.MustCompile(`^/api/v1/auth/me$`), ""},
	{"GET", regexp.MustCompile(`^/api/v1/(dashboard|articles|feeds|tags|highlights)$`), "reading:read"},
	{"GET", regexp.MustCompile(`^/api/v1/articles/\d+$`), "reading:read"},
	{"GET", regexp.MustCompile(`^/api/v1/day/\d{4}-\d{2}-\d{2}$`), "reading:read"},
	{"POST", regexp.MustCompile(`^/api/v1/(saved|feeds)$`), "reading:write"},
	{"DELETE", regexp.MustCompile(`^/api/v1/articles/\d+$`), "reading:write"},
}

var errLinkScope = errors.New("outside the link's scope")

// linkAllows reports whether a link token with these scopes may make this request.
func linkAllows(scopes []string, r *http.Request) bool {
	if portable.LinkAllows(scopes, r) || capabilities.LinkAllows(scopes, r) {
		return true
	}
	for _, lr := range linkRoutes {
		if !strings.Contains(" "+lr.methods+" ", " "+r.Method+" ") || !lr.path.MatchString(r.URL.Path) {
			continue
		}
		if lr.scope == "" {
			return true
		}
		for _, sc := range scopes {
			if sc == lr.scope {
				return true
			}
		}
	}
	return false
}

// linkUser validates a pcx_ token, checks the route against its scopes and
// maps it to the person it acts for. The second value is the acting app's slug.
func (s *Server) linkUser(ctx context.Context, tok string, r *http.Request) (*store.User, string, error) {
	in, err := s.core.Introspect(ctx, tok)
	if err != nil {
		return nil, "", err
	}
	if !in.Active || !in.Delegated() || in.Audience != "reader" || in.User == nil || in.User.Email == "" || in.App == nil || in.App.Slug == "" {
		return nil, "", http.ErrNoCookie
	}
	if !linkAllows(strings.Fields(in.Scope), r) {
		return nil, "", errLinkScope
	}
	u, err := s.coreIdentityUser(ctx, in)
	return u, in.App.Slug, err
}

const linkAppKey ctxKey = 2

// linkApp is the acting app's slug when the request came in on a link token.
func linkApp(ctx context.Context) string {
	a, _ := ctx.Value(linkAppKey).(string)
	return a
}
