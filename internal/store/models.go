package store

import (
	"strings"
)

// User is an account on this box.
type User struct {
	ID           int64  `json:"id"`
	Email        string `json:"email"`
	Name         string `json:"name"`
	PasswordHash string `json:"-"`
	Role         string `json:"role"` // admin | member
	CreatedAt    string `json:"created_at"`
}

// APIToken is a personal access token (hash stored, plaintext shown once).
type APIToken struct {
	ID         int64   `json:"id"`
	UserID     int64   `json:"user_id"`
	Name       string  `json:"name"`
	Prefix     string  `json:"prefix"`
	LastUsedAt *string `json:"last_used_at"`
	CreatedAt  string  `json:"created_at"`
}

// Folder groups feeds.
type Folder struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Name      string `json:"name"`
	Sort      int    `json:"sort"`
	CreatedAt string `json:"created_at"`
	Unread    int    `json:"unread"`
}

// Feed is one subscription.
type Feed struct {
	ID           int64  `json:"id"`
	UserID       int64  `json:"-"`
	FolderID     *int64 `json:"folder_id"`
	URL          string `json:"url"`
	SiteURL      string `json:"site_url"`
	Title        string `json:"title"`
	CustomTitle  string `json:"custom_title"`
	Description  string `json:"description"`
	IconURL      string `json:"icon_url"`
	Kind         string `json:"kind"`
	FullText     bool   `json:"full_text"`
	Notify       bool   `json:"notify"`
	Paused       bool   `json:"paused"`
	RefreshMin   int    `json:"refresh_min"`
	ETag         string `json:"-"`
	LastModified string `json:"-"`
	FetchedAt    string `json:"fetched_at"`
	NextFetchAt  string `json:"next_fetch_at"`
	NewestAt     string `json:"newest_at"`
	Error        string `json:"error"`
	ErrorCount   int    `json:"error_count"`
	Source       string `json:"source"`
	CreatedAt    string `json:"created_at"`
	UpdatedAt    string `json:"updated_at"`

	// Filled by list queries.
	Unread int `json:"unread"`
	Total  int `json:"total"`
}

// Name is what the person sees: their own title, else the feed's, else the address.
func (f Feed) Name() string {
	if f.CustomTitle != "" {
		return f.CustomTitle
	}
	if f.Title != "" {
		return f.Title
	}
	return f.URL
}

// Article is a post from a feed or a page saved to read later.
type Article struct {
	ID            int64   `json:"id"`
	UserID        int64   `json:"-"`
	FeedID        *int64  `json:"feed_id"`
	GUID          string  `json:"-"`
	URL           string  `json:"url"`
	Title         string  `json:"title"`
	Author        string  `json:"author"`
	SiteName      string  `json:"site_name"`
	Excerpt       string  `json:"excerpt"`
	Content       string  `json:"content,omitempty"`
	ImageURL      string  `json:"image_url"`
	MediaURL      string  `json:"media_url"`
	MediaType     string  `json:"media_type"`
	Lang          string  `json:"lang"`
	Words         int     `json:"words"`
	PublishedAt   string  `json:"published_at"`
	AddedAt       string  `json:"added_at"`
	Read          bool    `json:"read"`
	ReadAt        string  `json:"read_at"`
	Starred       bool    `json:"starred"`
	StarredAt     string  `json:"starred_at"`
	Saved         bool    `json:"saved"`
	SavedAt       string  `json:"saved_at"`
	Archived      bool    `json:"archived"`
	ArchivedAt    string  `json:"archived_at"`
	Progress      float64 `json:"progress"`
	ExtractState  string  `json:"extract_state"`
	ExtractError  string  `json:"extract_error"`
	Tags          string  `json:"tags"`
	Note          string  `json:"note"`
	AISummary     string  `json:"ai_summary"`
	NotesNoteID   int64   `json:"notes_note_id"`
	NotesSyncedAt string  `json:"notes_synced_at"`
	Source        string  `json:"source"`
	ExternalID    string  `json:"external_id"`
	CreatedAt     string  `json:"created_at"`
	UpdatedAt     string  `json:"updated_at"`

	// Filled by list queries.
	FeedTitle  string `json:"feed_title"`
	FeedIcon   string `json:"feed_icon"`
	Highlights int    `json:"highlights"`
}

// Minutes is the reading time at 230 words a minute (at least one).
func (a Article) Minutes() int {
	m := (a.Words + 229) / 230
	if m < 1 {
		m = 1
	}
	return m
}

// Highlight is a marked passage.
type Highlight struct {
	ID        int64   `json:"id"`
	UserID    int64   `json:"-"`
	ArticleID int64   `json:"article_id"`
	Text      string  `json:"text"`
	Note      string  `json:"note"`
	Color     string  `json:"color"`
	Prefix    string  `json:"prefix"`
	Suffix    string  `json:"suffix"`
	Position  float64 `json:"position"`
	CreatedAt string  `json:"created_at"`
	UpdatedAt string  `json:"updated_at"`

	// Filled by the highlights list.
	ArticleTitle string `json:"article_title,omitempty"`
	ArticleURL   string `json:"article_url,omitempty"`
	SiteName     string `json:"site_name,omitempty"`
}

// Rule acts on new posts that mention given words.
type Rule struct {
	ID        int64  `json:"id"`
	UserID    int64  `json:"-"`
	Name      string `json:"name"`
	FeedID    *int64 `json:"feed_id"`
	FolderID  *int64 `json:"folder_id"`
	Field     string `json:"field"`  // title | any | author | url
	Words     string `json:"words"`  // comma-separated
	Action    string `json:"action"` // read | star | save | tag | notify
	Tag       string `json:"tag"`
	Enabled   bool   `json:"enabled"`
	Hits      int    `json:"hits"`
	LastHitAt string `json:"last_hit_at"`
	CreatedAt string `json:"created_at"`
}

// Settings are the box-wide values plus one person's preferences and their
// own connections. Connection modes: "core" (an app link through Pocket
// Core), "direct" (URL + token) or "off"; "" = not chosen yet.
type Settings struct {
	Title         string `json:"title"`
	SetupComplete bool   `json:"setup_complete"`

	DateFormat string `json:"date_format"`
	TimeFormat string `json:"time_format"`
	WeekStart  int    `json:"week_start"` // 1 = Monday, 7 = Sunday

	// Reading.
	Font           string `json:"font"`         // serif | sans | mono
	FontSize       int    `json:"font_size"`    // px, 14–26
	LineWidth      string `json:"line_width"`   // narrow | medium | wide
	LineHeight     string `json:"line_height"`  // tight | normal | loose
	ReaderTheme    string `json:"reader_theme"` // app | paper | sepia | night
	Justify        bool   `json:"justify"`
	MarkOnOpen     bool   `json:"mark_on_open"`   // opening a post marks it read
	MarkOnScroll   bool   `json:"mark_on_scroll"` // scrolling past posts in the list marks them read
	SortOldest     bool   `json:"sort_oldest"`    // oldest first in unread lists
	ListStyle      string `json:"list_style"`     // cards | compact | magazine
	ShowImages     bool   `json:"show_images"`    // pictures in lists
	OpenOriginal   bool   `json:"open_original"`  // links in posts open in a new tab
	HideRead       bool   `json:"hide_read"`      // lists show only unread
	ConfirmAll     bool   `json:"confirm_all"`    // ask before "mark all as read"
	AutoExtract    bool   `json:"auto_extract"`   // saved links: fetch the full article
	RefreshMin     int    `json:"refresh_min"`    // how often feeds are checked, minutes
	KeepReadDays   int    `json:"keep_read_days"` // read posts are removed after (starred, saved, highlighted kept); 0 = keep
	KeepUnreadDays int    `json:"keep_unread_days"`

	// Notifications.
	NotifyURL       string `json:"notify_url"`
	NotifyFormat    string `json:"notify_format"` // apprise | ntfy | json
	NotifyNewPosts  bool   `json:"notify_new_posts"`
	DigestEnabled   bool   `json:"digest_enabled"`
	DigestHour      int    `json:"digest_hour"`
	DigestDays      string `json:"digest_days"` // ISO weekdays "12345"
	NotifyFeedError bool   `json:"notify_feed_errors"`

	// Pocket Notes: highlights become a note per article.
	NotesMode      string `json:"notes_mode"`
	NotesURL       string `json:"notes_url"`
	NotesToken     string `json:"notes_token"`
	NotesPublicURL string `json:"notes_public_url"`
	NotesFolder    string `json:"notes_folder"`
	NotesAuto      bool   `json:"notes_auto"` // send highlights as they are made

	// AI for summaries, the catch-up digest, tags and questions: Pocket AI
	// through Core, or an OpenAI-compatible API with the person's own key.
	AIProvider    string `json:"ai_provider"` // auto | pocket | openai
	AIPocketModel string `json:"ai_pocket_model"`
	OpenAIKey     string `json:"openai_api_key"`
	OpenAIModel   string `json:"openai_model"`
	OpenAIBaseURL string `json:"openai_base_url"`
	AIAutoTag     bool   `json:"ai_auto_tag"` // suggest tags for saved pages
	AILanguage    string `json:"ai_language"` // the summaries' language, "" = the article's
}

// SplitTags turns "a, b ,c" into clean lowercase tags.
func SplitTags(s string) []string {
	var out []string
	seen := map[string]bool{}
	for _, t := range strings.Split(s, ",") {
		t = strings.ToLower(strings.TrimSpace(t))
		if t != "" && !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}

// JoinTags is the stored form of tags.
func JoinTags(s string) string { return strings.Join(SplitTags(s), ", ") }
