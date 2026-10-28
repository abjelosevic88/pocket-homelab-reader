package server

import (
	"context"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"time"

	"github.com/abjelosevic88/pocket-homelab-reader/internal/store"
)

// demoFeed is one made-up feed of the demo.
type demoFeed struct {
	title, site, folder, kind string
	topic                     string
}

var demoFeeds = []demoFeed{
	{"The Slow Kitchen", "https://slowkitchen.example", "Cooking", "web", "food"},
	{"Northern Trails", "https://northerntrails.example", "Outdoors", "web", "outdoors"},
	{"Field Notes on Software", "https://fieldnotes.example", "Tech", "web", "tech"},
	{"Small Web Weekly", "https://smallweb.example", "Tech", "web", "web"},
	{"City Desk", "https://citydesk.example", "News", "web", "city"},
	{"Workshop Videos", "https://www.youtube.com/@workshop-demo", "YouTube", "youtube", "making"},
	{"Release notes: Pocket apps", "https://github.com/abjelosevic88", "", "web", "releases"},
}

var demoTitles = map[string][]string{
	"food": {"A week of one-pot dinners", "Why your bread needs a longer rest", "The case for cooking beans from dry", "Five things to do with stale bread", "Making stock from what you would throw away",
		"Ajvar, slowly", "A simple guide to fermenting cabbage", "How to season a cast iron pan, properly this time", "Weeknight pasta without a recipe", "What I learned baking every day for a month",
		"The quiet joy of soup", "Plums, three ways", "Knife skills that actually matter"},
	"outdoors": {"Three days on the ridge in October", "Packing light without freezing", "The best hut walks you can do in a weekend", "Reading the weather from the clouds", "Our first winter camp",
		"Where the river trail ends", "A beginner's map and compass morning", "Boots: what to look for and what to ignore", "Rain, and why I stopped minding it", "Foraging mushrooms safely"},
	"tech": {"SQLite is enough for most apps", "What I wish I knew about time zones", "One binary, one file: deploying without the drama", "Notes on writing boring software", "The hidden cost of a dependency",
		"Testing the thing, not the mock", "A small guide to backups you will actually restore", "Readable errors are a feature", "Why we deleted our message queue", "Keyset paging, explained slowly",
		"On choosing defaults", "Profiling a slow request, step by step"},
	"web": {"Personal sites are back", "RSS never left", "A directory of small, handmade websites", "How to make a feed for a site that has none", "The newsletter that became a blog again",
		"Webrings, revisited", "Ten blogs worth following this autumn", "Writing for the web without a platform"},
	"city": {"New tram line opens next spring", "Library extends weekend opening hours", "The market square gets more trees", "Cycle lanes: what changes on the main road", "Old cinema to reopen as a culture centre",
		"Water works on the east side this week", "Night buses return for the winter", "Council approves the riverside park plan", "Schools add a second language from first grade", "Record turnout at the autumn book fair"},
	"making": {"Building a workbench from one sheet of plywood", "Sharpening chisels in ten minutes", "Restoring an old hand plane", "Dovetails for people who hate dovetails", "A shelf with no screws",
		"Finishing oak: oil or wax?", "My small shop tour", "Fixing a wobbly chair the right way"},
	"releases": {"Pocket Reader 0.1.0", "Pocket Notes 1.8.0", "Pocket Core 0.19.2", "Pocket Kitchen 0.9.0", "Pocket Music 1.3.1"},
}

var demoSentences = map[string][]string{
	"food": {"Start with a heavy pot and more onions than seems reasonable.", "Time does most of the work here; your job is to leave it alone.", "Salt early, taste often, and add the acid at the very end.",
		"A spoonful of the cooking water turns a sauce from thin to glossy.", "Dry beans need a night in water and a pinch of salt, nothing more.", "Most recipes ask for too little resting time.",
		"Roast the peppers until the skins are black, then let them steam in a covered bowl.", "Leftovers are an ingredient, not a problem.", "Good bread needs flour, water, salt and patience."},
	"outdoors": {"We left the car at first light and reached the saddle by ten.", "The forecast said showers; the mountain said otherwise.", "Pack the warm layer you think you will not need.",
		"A map tells you where the trail goes; the ground tells you how long it takes.", "The hut warden had soup ready and a stove that roared.", "Wet feet are fine; cold wet feet are a choice.",
		"From the top you can see three valleys and, on a clear day, the sea.", "Leave the route plan with someone who will notice if you are late."},
	"tech": {"The simplest thing that could work usually does, for longer than you expect.", "A single SQLite file handles more traffic than most teams will ever see.", "Every dependency is a promise someone else has to keep.",
		"Write the error message for the person who will read it at 3 a.m.", "Backups you have never restored are a hope, not a plan.", "Defaults are the decisions most people will never revisit.",
		"Measure first: the slow part is rarely where you think it is.", "Boring technology leaves room for interesting problems."},
	"web": {"A feed is still the easiest way to follow someone without an algorithm in between.", "Small sites load fast, last long and belong to the people who write them.", "You do not need a platform to publish; you need a page and a link.",
		"Most blogs already have a feed; look for the little orange icon or add /feed.", "Webrings were the original recommendation engine.", "Writing in public gets easier the less you think about the audience."},
	"city": {"The council said work would begin in March and take about a year.", "Residents can comment on the plans until the end of the month.", "The change takes effect from Monday.",
		"Buses will run every twenty minutes between midnight and four.", "The project is funded jointly by the city and the region.", "Opening hours on Saturdays and Sundays will be ten to six."},
	"making": {"Measure twice, cut once, and keep the offcuts.", "A sharp chisel is safer than a blunt one.", "The secret is a flat reference surface; everything else follows.",
		"Glue alone is stronger than you think if the joint fits well.", "Oil brings out the grain; wax gives a softer sheen."},
	"releases": {"This release brings a faster search and fewer surprises.", "Fixed: a crash when the database was locked.", "New: a light and dark theme that follows your system.", "Thanks to everyone who reported issues."},
}

// seedDemo fills the owner's account with a few weeks of plausible reading
// (DEMO_DATA=true, for screenshots and trying the app). Nothing is fetched:
// the demo feeds are never checked and the pictures are drawn by the app.
func (s *Server) seedDemo(ctx context.Context, owner int64) error {
	key := fmt.Sprintf("seeded:%d:demo", owner)
	if _, _, done := s.store.CacheGet(ctx, key); done {
		return nil
	}
	rng := rand.New(rand.NewSource(42))
	now := time.Now().UTC()
	para := func(topic string, n int) string {
		pool := demoSentences[topic]
		var b strings.Builder
		for p := 0; p < n; p++ {
			b.WriteString("<p>")
			k := min(3+rng.Intn(3), len(pool))
			for i, j := range rng.Perm(len(pool))[:k] {
				if i > 0 {
					b.WriteString(" ")
				}
				b.WriteString(pool[j])
			}
			b.WriteString("</p>")
			if p == 1 && n > 3 {
				b.WriteString("<h2>What we learned</h2>")
			}
			if p == 2 && n > 4 {
				b.WriteString("<blockquote><p>" + pool[rng.Intn(len(pool))] + "</p></blockquote>")
			}
		}
		return b.String()
	}
	img := 0
	nextImg := func() string {
		img++
		return fmt.Sprintf("/demo/img/%d.svg", img%12)
	}
	var postIDs []int64
	for fi, d := range demoFeeds {
		var folder *int64
		if d.folder != "" {
			f, err := s.store.FolderByName(ctx, owner, d.folder, true)
			if err != nil {
				return err
			}
			folder = &f.ID
		}
		url := d.site + "/feed.xml"
		if d.kind == "youtube" {
			url = fmt.Sprintf("https://www.youtube.com/feeds/videos.xml?channel_id=UCdemo%016d", fi)
		}
		f := &store.Feed{URL: url, SiteURL: d.site, Title: d.title, FolderID: folder, Kind: d.kind, Source: "demo", Notify: d.topic == "releases", FullText: d.topic == "city"}
		if err := s.store.CreateFeed(ctx, owner, f); err != nil {
			return err
		}
		// never checked: the addresses are made up
		next := now.AddDate(10, 0, 0)
		f.NewestAt = now.Add(-time.Duration(1+fi*3) * time.Hour).Format(time.RFC3339)
		if err := s.store.FeedFetched(ctx, f, next); err != nil {
			return err
		}
		titles := demoTitles[d.topic]
		for i, t := range titles {
			age := time.Duration(i)*time.Duration(20+rng.Intn(40))*time.Hour + time.Duration(rng.Intn(300))*time.Minute
			pub := now.Add(-age)
			fid := f.ID
			a := store.Article{FeedID: &fid, GUID: fmt.Sprintf("demo-%d-%d", fi, i), URL: fmt.Sprintf("%s/posts/%s", d.site, slugify(t)), Title: t,
				Author:  []string{"Ana Kovač", "Marko Petrović", "Lena Horvat", "Sam Rivers", "Ida Berg", ""}[rng.Intn(6)],
				Content: para(d.topic, 3+rng.Intn(5)), PublishedAt: pub.Format(time.RFC3339), Source: "demo"}
			if d.topic != "releases" && rng.Intn(4) > 0 {
				a.ImageURL = nextImg()
				a.Content = `<figure><img src="` + a.ImageURL + `" alt=""></figure>` + a.Content
			}
			if d.kind == "youtube" {
				a.ImageURL = nextImg()
				a.Content = `<p><img src="` + a.ImageURL + `" alt=""></p><p>A new video from the workshop.</p>` + para(d.topic, 1)
			}
			if age > 30*time.Hour && rng.Intn(10) < 7 {
				a.Read = true
				a.ReadAt = pub.Add(time.Duration(2+rng.Intn(20)) * time.Hour).Format(time.RFC3339)
			}
			if rng.Intn(9) == 0 {
				a.Starred = true
				a.StarredAt = a.PublishedAt
			}
			ok, err := s.store.InsertPost(ctx, owner, &a)
			if err != nil {
				return err
			}
			if ok {
				postIDs = append(postIDs, a.ID)
				if a.Read {
					p := 0.4 + rng.Float64()*0.6
					_, _ = s.store.UpdateArticle(ctx, owner, []int64{a.ID}, store.ArticleState{Progress: &p})
					_, _ = s.store.DB.ExecContext(ctx, `UPDATE articles SET read_at = ?, added_at = ? WHERE id = ?`, a.ReadAt, a.PublishedAt, a.ID)
				} else {
					_, _ = s.store.DB.ExecContext(ctx, `UPDATE articles SET added_at = ? WHERE id = ?`, a.PublishedAt, a.ID)
				}
			}
		}
	}
	// pages saved to read later
	saved := []struct{ title, site, topic, tags string }{
		{"The long, slow death of the bookmark", "essays.example", "web", "web, reading"},
		{"How to read more without trying harder", "habits.example", "web", "reading"},
		{"A field guide to autumn mushrooms", "forage.example", "outdoors", "outdoors, food"},
		{"The best way to store fresh herbs", "kitchen.example", "food", "food"},
		{"What makes a good README", "devnotes.example", "tech", "writing, tech"},
		{"Twelve hikes within an hour of the city", "trails.example", "outdoors", "outdoors, weekend"},
		{"Why we still print photos", "photo.example", "web", "photos"},
		{"Designing for calm", "design.example", "tech", "design"},
		{"An honest review of standing desks", "office.example", "making", "home"},
	}
	var savedIDs []int64
	for i, p := range saved {
		at := now.Add(-time.Duration(i*19+rng.Intn(12)) * time.Hour)
		a := &store.Article{URL: fmt.Sprintf("https://%s/%s", p.site, slugify(p.title)), Title: p.title, SiteName: p.site, Content: para(p.topic, 5+rng.Intn(4)),
			ImageURL: nextImg(), Tags: p.tags, SavedAt: at.Format(time.RFC3339), PublishedAt: at.Add(-72 * time.Hour).Format(time.RFC3339), ExtractState: "done", Source: "demo",
			Author: []string{"Nora Vidić", "Tom Ashby", "Mira Lund", ""}[rng.Intn(4)]}
		a.Content = `<p><img src="` + a.ImageURL + `" alt=""></p>` + a.Content
		if i >= 6 {
			a.Archived = true
			a.ArchivedAt = at.Add(30 * time.Hour).Format(time.RFC3339)
			a.Read = true
			a.ReadAt = a.ArchivedAt
		}
		got, _, err := s.store.SavePage(ctx, owner, a)
		if err != nil {
			return err
		}
		savedIDs = append(savedIDs, got.ID)
		switch i {
		case 0:
			p := 0.62
			_, _ = s.store.UpdateArticle(ctx, owner, []int64{got.ID}, store.ArticleState{Progress: &p})
		case 1:
			p := 0.2
			_, _ = s.store.UpdateArticle(ctx, owner, []int64{got.ID}, store.ArticleState{Progress: &p})
		}
	}
	// highlights in a few articles
	for i, id := range append(savedIDs[:4], postIDs[:3]...) {
		a, err := s.store.GetArticle(ctx, owner, id)
		if err != nil {
			continue
		}
		text := store.PlainText(a.Content)
		sents := strings.SplitAfter(text, ". ")
		for k := 0; k < 1+rng.Intn(3) && k < len(sents); k++ {
			pick := strings.TrimSpace(sents[(k*3+1)%len(sents)])
			if pick == "" {
				continue
			}
			h := &store.Highlight{ArticleID: id, Text: pick, Color: []string{"yellow", "green", "blue", "pink"}[(i+k)%4], Position: float64(k) / 4,
				CreatedAt: now.Add(-time.Duration(i*13+k) * time.Hour).Format(time.RFC3339)}
			if k == 0 && i%2 == 0 {
				h.Note = []string{"Try this next weekend.", "Worth quoting in the team doc.", "This is the whole point.", "Ask Lena about this."}[i%4]
			}
			_ = s.store.CreateHighlight(ctx, owner, h)
		}
	}
	// rules
	for _, r := range []store.Rule{
		{Name: "Hide sponsored posts", Field: "any", Words: "sponsored, advertisement, partner content", Action: "read", Enabled: true},
		{Name: "Tag tram news", Field: "title", Words: "tram, bus, cycle", Action: "tag", Tag: "transport", Enabled: true},
		{Name: "Tell me about Pocket releases", Field: "title", Words: "pocket", Action: "notify", Enabled: true},
	} {
		r := r
		_ = s.store.CreateRule(ctx, owner, &r)
	}
	s.log.Info("demo data seeded", "posts", len(postIDs), "saved", len(savedIDs))
	return s.store.CachePut(ctx, key, "1")
}

func slugify(t string) string {
	var b strings.Builder
	dash := false
	for _, r := range strings.ToLower(t) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
			dash = false
		} else if !dash && b.Len() > 0 {
			b.WriteByte('-')
			dash = true
		}
	}
	return strings.Trim(b.String(), "-")
}

// demo pictures: soft landscapes drawn as SVG, so the demo needs no network
var demoPalettes = [][3]string{
	{"#f6d365", "#fda085", "#7a4b2a"}, {"#a1c4fd", "#c2e9fb", "#30577a"}, {"#d4fc79", "#96e6a1", "#2f6b3a"}, {"#fbc2eb", "#a6c1ee", "#5b4a7a"},
	{"#ffecd2", "#fcb69f", "#8a4b33"}, {"#84fab0", "#8fd3f4", "#246b66"}, {"#e0c3fc", "#8ec5fc", "#3e4f8a"}, {"#fddb92", "#d1fdff", "#5f6b2f"},
	{"#c1dfc4", "#deecdd", "#3d5c42"}, {"#f5efef", "#feada6", "#7a3c3c"}, {"#accbee", "#e7f0fd", "#3a5677"}, {"#ffe29f", "#ffa99f", "#7a4a3a"},
}

func (s *Server) handleDemoImage(w http.ResponseWriter, r *http.Request) {
	n := int(idParam(r, "n")) % len(demoPalettes)
	if n < 0 {
		n = 0
	}
	p := demoPalettes[n]
	rng := rand.New(rand.NewSource(int64(n) + 7))
	hill := func(y, amp int) string {
		var b strings.Builder
		fmt.Fprintf(&b, "M0 %d", y)
		for x := 0; x <= 800; x += 100 {
			fmt.Fprintf(&b, " Q%d %d %d %d", x+50, y-amp+rng.Intn(amp*2), x+100, y-rng.Intn(amp))
		}
		b.WriteString(" L800 450 L0 450 Z")
		return b.String()
	}
	svg := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 800 450" width="800" height="450">
<defs><linearGradient id="g" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stop-color="%s"/><stop offset="1" stop-color="%s"/></linearGradient></defs>
<rect width="800" height="450" fill="url(#g)"/><circle cx="%d" cy="%d" r="%d" fill="#fff" opacity=".55"/>
<path d="%s" fill="%s" opacity=".35"/><path d="%s" fill="%s" opacity=".6"/><path d="%s" fill="%s"/></svg>`,
		p[0], p[1], 120+rng.Intn(560), 90+rng.Intn(80), 36+rng.Intn(30), hill(300, 60), p[2], hill(340, 50), p[2], hill(390, 30), p[2])
	w.Header().Set("Content-Type", "image/svg+xml")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write([]byte(svg))
}
