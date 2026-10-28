package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"html"
	"regexp"
	"strings"
	"time"
)

const articleCols = `a.id, a.user_id, a.feed_id, a.guid, a.url, a.title, a.author, a.site_name, a.excerpt, a.image_url, a.media_url, a.media_type,
	a.lang, a.words, a.published_at, a.added_at, a.read, a.read_at, a.starred, a.starred_at, a.saved, a.saved_at, a.archived, a.archived_at,
	a.progress, a.extract_state, a.extract_error, a.tags, a.note, a.ai_summary, a.notes_note_id, a.notes_synced_at, a.source, a.external_id,
	a.created_at, a.updated_at, COALESCE(NULLIF(f.custom_title, ''), NULLIF(f.title, ''), f.url, ''), COALESCE(f.icon_url, ''),
	(SELECT COUNT(1) FROM highlights h WHERE h.article_id = a.id)`

const articleFrom = ` FROM articles a LEFT JOIN feeds f ON f.id = a.feed_id `

func scanArticle(sc interface{ Scan(...any) error }, withContent bool) (*Article, error) {
	var a Article
	var feed sql.NullInt64
	dst := []any{&a.ID, &a.UserID, &feed, &a.GUID, &a.URL, &a.Title, &a.Author, &a.SiteName, &a.Excerpt, &a.ImageURL, &a.MediaURL, &a.MediaType,
		&a.Lang, &a.Words, &a.PublishedAt, &a.AddedAt, &a.Read, &a.ReadAt, &a.Starred, &a.StarredAt, &a.Saved, &a.SavedAt, &a.Archived, &a.ArchivedAt,
		&a.Progress, &a.ExtractState, &a.ExtractError, &a.Tags, &a.Note, &a.AISummary, &a.NotesNoteID, &a.NotesSyncedAt, &a.Source, &a.ExternalID,
		&a.CreatedAt, &a.UpdatedAt, &a.FeedTitle, &a.FeedIcon, &a.Highlights}
	if withContent {
		dst = append(dst, &a.Content)
	}
	if err := sc.Scan(dst...); err != nil {
		return nil, err
	}
	if feed.Valid {
		v := feed.Int64
		a.FeedID = &v
	}
	if a.SiteName == "" {
		a.SiteName = a.FeedTitle
	}
	return &a, nil
}

// ---- writing ----

var (
	reTag   = regexp.MustCompile(`(?s)<[^>]*>`)
	reSpace = regexp.MustCompile(`\s+`)
	reWord  = regexp.MustCompile(`[\p{L}\p{N}]+`)
)

// PlainText turns HTML into one line of text.
func PlainText(h string) string {
	h = strings.NewReplacer("</p>", " </p>", "<br", " <br", "</li>", " </li>", "</h", " </h", "</div>", " </div>").Replace(h)
	return strings.TrimSpace(reSpace.ReplaceAllString(html.UnescapeString(reTag.ReplaceAllString(h, " ")), " "))
}

// Excerpt is the first n runes of text, cut at a word.
func Excerpt(text string, n int) string {
	r := []rune(text)
	if len(r) <= n {
		return text
	}
	cut := string(r[:n])
	if i := strings.LastIndexAny(cut, "  "); i > n/2 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,.;:-–—") + "…"
}

// CountWords counts the words of a text.
func CountWords(text string) int { return len(reWord.FindAllStringIndex(text, -1)) }

// prepare fills the derived fields of an article from its content.
func (a *Article) prepare() string {
	text := PlainText(a.Content)
	if a.Excerpt == "" {
		a.Excerpt = Excerpt(text, 300)
	} else {
		a.Excerpt = Excerpt(PlainText(a.Excerpt), 300)
	}
	if text == "" {
		text = a.Excerpt
	}
	if a.Words == 0 {
		a.Words = CountWords(text)
	}
	a.Title = strings.TrimSpace(html.UnescapeString(reSpace.ReplaceAllString(a.Title, " ")))
	if a.PublishedAt == "" {
		a.PublishedAt = Now()
	}
	return text
}

func index(ctx context.Context, ex execer, id int64, title, body, tags string) error {
	if len(body) > 200_000 {
		body = body[:200_000]
	}
	_, err := ex.ExecContext(ctx, `INSERT OR REPLACE INTO articles_fts (rowid, title, body, tags) VALUES (?, ?, ?, ?)`, id, title, body, tags)
	return err
}

type execer interface {
	ExecContext(ctx context.Context, q string, args ...any) (sql.Result, error)
}

// IsGone reports whether a feed post was cleaned up before.
func (s *Store) IsGone(ctx context.Context, feedID int64, guid string) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM gone WHERE feed_id = ? AND guid = ?`, feedID, guid).Scan(&n)
	return n > 0
}

// HasPost reports whether a feed post is stored.
func (s *Store) HasPost(ctx context.Context, uid, feedID int64, guid string) bool {
	var n int
	_ = s.DB.QueryRowContext(ctx, `SELECT COUNT(1) FROM articles WHERE user_id = ? AND feed_id = ? AND guid = ?`, uid, feedID, guid).Scan(&n)
	return n > 0
}

// InsertPost stores a new feed post; false when it is already there (or was cleaned up).
func (s *Store) InsertPost(ctx context.Context, uid int64, a *Article) (bool, error) {
	if a.FeedID == nil {
		return false, errors.New("a post needs a feed")
	}
	if s.IsGone(ctx, *a.FeedID, a.GUID) {
		return false, nil
	}
	text := a.prepare()
	if a.Source == "" {
		a.Source = "feed"
	}
	now := Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO articles (user_id, feed_id, guid, url, title, author, site_name, excerpt, content, image_url, media_url, media_type,
		lang, words, published_at, added_at, read, read_at, starred, starred_at, saved, saved_at, tags, extract_state, source, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) ON CONFLICT DO NOTHING`,
		uid, *a.FeedID, a.GUID, a.URL, a.Title, a.Author, a.SiteName, a.Excerpt, a.Content, a.ImageURL, a.MediaURL, a.MediaType,
		a.Lang, a.Words, a.PublishedAt, now, boolInt(a.Read), stamp(a.Read, a.ReadAt), boolInt(a.Starred), stamp(a.Starred, a.StarredAt),
		boolInt(a.Saved), stamp(a.Saved, a.SavedAt), JoinTags(a.Tags), a.ExtractState, a.Source, now, now)
	if err != nil {
		return false, err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return false, nil
	}
	a.ID, _ = res.LastInsertId()
	a.UserID, a.AddedAt, a.CreatedAt, a.UpdatedAt = uid, now, now, now
	return true, index(ctx, s.DB, a.ID, a.Title, text, a.Tags)
}

func stamp(on bool, at string) string {
	if !on {
		return ""
	}
	if at == "" {
		return Now()
	}
	return at
}

// SavePage puts a page on the read-later list. An article with the same
// address (a feed post, or a page saved before) is reused: it is saved again
// and brought to the top. The bool reports whether a new row was made.
func (s *Store) SavePage(ctx context.Context, uid int64, a *Article) (*Article, bool, error) {
	if a.URL == "" {
		return nil, false, errors.New("an address is needed")
	}
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM articles WHERE user_id = ? AND url = ? ORDER BY feed_id IS NOT NULL, id LIMIT 1`, uid, a.URL).Scan(&id)
	if err == nil {
		now := Now()
		if _, err := s.DB.ExecContext(ctx, `UPDATE articles SET saved = 1, saved_at = ?, archived = 0, archived_at = '',
			tags = CASE WHEN ? <> '' AND tags = '' THEN ? ELSE tags END, updated_at = ? WHERE user_id = ? AND id = ?`,
			now, JoinTags(a.Tags), JoinTags(a.Tags), now, uid, id); err != nil {
			return nil, false, err
		}
		got, err := s.GetArticle(ctx, uid, id)
		return got, false, err
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return nil, false, err
	}
	text := a.prepare()
	if a.Source == "" {
		a.Source = "app"
	}
	now := Now()
	savedAt := firstNonEmpty(a.SavedAt, now)
	res, err := s.DB.ExecContext(ctx, `INSERT INTO articles (user_id, url, title, author, site_name, excerpt, content, image_url, lang, words, published_at,
		added_at, saved, saved_at, archived, archived_at, read, read_at, starred, starred_at, tags, note, extract_state, source, external_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 1, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, a.URL, a.Title, a.Author, a.SiteName, a.Excerpt, a.Content, a.ImageURL, a.Lang, a.Words, a.PublishedAt,
		savedAt, savedAt, boolInt(a.Archived), stamp(a.Archived, a.ArchivedAt), boolInt(a.Read), stamp(a.Read, a.ReadAt),
		boolInt(a.Starred), stamp(a.Starred, a.StarredAt), JoinTags(a.Tags), a.Note, a.ExtractState, a.Source, a.ExternalID, now, now)
	if err != nil {
		return nil, false, conflict(err)
	}
	id, _ = res.LastInsertId()
	if err := index(ctx, s.DB, id, a.Title, text, JoinTags(a.Tags)); err != nil {
		return nil, false, err
	}
	got, err := s.GetArticle(ctx, uid, id)
	return got, true, err
}

// SetExtracted stores the full article fetched for a post or page.
func (s *Store) SetExtracted(ctx context.Context, uid, id int64, x *Article, state, msg string) error {
	if state != "done" {
		_, err := s.DB.ExecContext(ctx, `UPDATE articles SET extract_state = ?, extract_error = ?, updated_at = ? WHERE user_id = ? AND id = ?`, state, msg, Now(), uid, id)
		return err
	}
	cur, err := s.GetArticle(ctx, uid, id)
	if err != nil {
		return err
	}
	x.Excerpt = ""
	x.Words = 0
	text := x.prepare()
	_, err = s.DB.ExecContext(ctx, `UPDATE articles SET content = ?, excerpt = ?, words = ?,
		title = CASE WHEN title = '' OR title = url THEN ? ELSE title END, author = CASE WHEN author = '' THEN ? ELSE author END,
		site_name = CASE WHEN site_name = '' THEN ? ELSE site_name END, image_url = CASE WHEN image_url = '' THEN ? ELSE image_url END,
		lang = CASE WHEN lang = '' THEN ? ELSE lang END, extract_state = 'done', extract_error = '', updated_at = ? WHERE user_id = ? AND id = ?`,
		x.Content, x.Excerpt, x.Words, x.Title, x.Author, x.SiteName, x.ImageURL, x.Lang, Now(), uid, id)
	if err != nil {
		return err
	}
	title := cur.Title
	if title == "" || title == cur.URL {
		title = x.Title
	}
	return index(ctx, s.DB, id, title, text, cur.Tags)
}

// PendingExtracts lists articles (anyone's) waiting for their full text.
func (s *Store) PendingExtracts(ctx context.Context, n int) ([]Article, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+articleCols+articleFrom+`WHERE a.extract_state = 'pending' ORDER BY a.id LIMIT ?`, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Article
	for rows.Next() {
		a, err := scanArticle(rows, false)
		if err != nil {
			return nil, err
		}
		out = append(out, *a)
	}
	return out, rows.Err()
}

// ---- reading ----

// GetArticle returns one of a person's articles with its content.
func (s *Store) GetArticle(ctx context.Context, uid, id int64) (*Article, error) {
	a, err := scanArticle(s.DB.QueryRowContext(ctx, `SELECT `+articleCols+`, a.content`+articleFrom+`WHERE a.user_id = ? AND a.id = ?`, uid, id), true)
	return a, notFound(err)
}

// ArticleByURL finds a person's article by address.
func (s *Store) ArticleByURL(ctx context.Context, uid int64, url string) (*Article, error) {
	var id int64
	if err := s.DB.QueryRowContext(ctx, `SELECT id FROM articles WHERE user_id = ? AND url = ? ORDER BY feed_id IS NOT NULL, id LIMIT 1`, uid, url).Scan(&id); err != nil {
		return nil, notFound(err)
	}
	return s.GetArticle(ctx, uid, id)
}

// ArticleFilter selects a list of articles.
type ArticleFilter struct {
	View     string // unread | all | today | saved | archive | starred | history | highlights
	FeedID   int64
	FolderID int64
	Tag      string
	Query    string // full-text
	From, To string // added_at / read_at window (RFC3339)
	Oldest   bool
	Limit    int
	Before   string // keyset paging: the sort key of the last row seen
	BeforeID int64
	FeedOnly bool // only posts from feeds
	IDs      []int64
	Unread   bool   // only unread
	ReadOnly bool   // only read
	Newer    string // published after (RFC3339)
	Older    string // published before (RFC3339)
}

// ListArticles returns a page of articles (without content) and the paging cursor.
func (s *Store) ListArticles(ctx context.Context, uid int64, f ArticleFilter) ([]Article, string, error) {
	where := []string{"a.user_id = ?"}
	args := []any{uid}
	sortCol := "a.published_at"
	switch f.View {
	case "unread":
		where = append(where, "a.read = 0", "a.feed_id IS NOT NULL")
	case "today":
		where = append(where, "a.feed_id IS NOT NULL", "a.added_at >= ?")
		args = append(args, firstNonEmpty(f.From, time.Now().Add(-24*time.Hour).UTC().Format(time.RFC3339)))
	case "saved":
		where = append(where, "a.saved = 1", "a.archived = 0")
		sortCol = "a.saved_at"
	case "archive":
		where = append(where, "a.saved = 1", "a.archived = 1")
		sortCol = "a.archived_at"
	case "starred":
		where = append(where, "a.starred = 1")
		sortCol = "a.starred_at"
	case "history":
		where = append(where, "a.read = 1", "a.read_at <> ''")
		sortCol = "a.read_at"
	case "highlights":
		where = append(where, "EXISTS (SELECT 1 FROM highlights h WHERE h.article_id = a.id)")
		sortCol = "a.updated_at"
	}
	if f.FeedOnly {
		where = append(where, "a.feed_id IS NOT NULL")
	}
	if f.Unread {
		where = append(where, "a.read = 0")
	}
	if f.ReadOnly {
		where = append(where, "a.read = 1")
	}
	if f.Newer != "" {
		where = append(where, "a.published_at >= ?")
		args = append(args, f.Newer)
	}
	if f.Older != "" {
		where = append(where, "a.published_at < ?")
		args = append(args, f.Older)
	}
	if f.FeedID > 0 {
		where = append(where, "a.feed_id = ?")
		args = append(args, f.FeedID)
	}
	if f.FolderID > 0 {
		where = append(where, "f.folder_id = ?")
		args = append(args, f.FolderID)
	}
	if t := strings.ToLower(strings.TrimSpace(f.Tag)); t != "" {
		where = append(where, "(', ' || a.tags || ',') LIKE ?")
		args = append(args, "%, "+t+",%")
	}
	if len(f.IDs) > 0 {
		ph := strings.TrimSuffix(strings.Repeat("?,", len(f.IDs)), ",")
		where = append(where, "a.id IN ("+ph+")")
		for _, id := range f.IDs {
			args = append(args, id)
		}
	}
	if f.View != "today" && f.From != "" {
		where = append(where, sortCol+" >= ?")
		args = append(args, f.From)
	}
	if f.To != "" {
		where = append(where, sortCol+" < ?")
		args = append(args, f.To)
	}
	if q := ftsQuery(f.Query); q != "" {
		where = append(where, "a.id IN (SELECT rowid FROM articles_fts WHERE articles_fts MATCH ?)")
		args = append(args, q)
	}
	asc := f.Oldest && (f.View == "unread" || f.View == "today" || f.View == "saved" || f.View == "" || f.View == "starred")
	if f.Before != "" {
		if asc {
			where = append(where, "("+sortCol+" > ? OR ("+sortCol+" = ? AND a.id > ?))")
		} else {
			where = append(where, "("+sortCol+" < ? OR ("+sortCol+" = ? AND a.id < ?))")
		}
		args = append(args, f.Before, f.Before, f.BeforeID)
	}
	dir := "DESC"
	if asc {
		dir = "ASC"
	}
	limit := f.Limit
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	q := `SELECT ` + articleCols + `, ` + sortCol + articleFrom + `WHERE ` + strings.Join(where, " AND ") +
		` ORDER BY ` + sortCol + ` ` + dir + `, a.id ` + dir + ` LIMIT ?`
	rows, err := s.DB.QueryContext(ctx, q, append(args, limit+1)...)
	if err != nil {
		return nil, "", err
	}
	defer rows.Close()
	out := []Article{}
	var keys []string
	for rows.Next() {
		var key string
		a, err := scanArticleKey(rows, &key)
		if err != nil {
			return nil, "", err
		}
		out = append(out, *a)
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, "", err
	}
	next := ""
	if len(out) > limit {
		out = out[:limit]
		next = fmt.Sprintf("%s|%d", keys[limit-1], out[limit-1].ID)
	}
	return out, next, nil
}

func scanArticleKey(rows *sql.Rows, key *string) (*Article, error) {
	var a Article
	var feed sql.NullInt64
	err := rows.Scan(&a.ID, &a.UserID, &feed, &a.GUID, &a.URL, &a.Title, &a.Author, &a.SiteName, &a.Excerpt, &a.ImageURL, &a.MediaURL, &a.MediaType,
		&a.Lang, &a.Words, &a.PublishedAt, &a.AddedAt, &a.Read, &a.ReadAt, &a.Starred, &a.StarredAt, &a.Saved, &a.SavedAt, &a.Archived, &a.ArchivedAt,
		&a.Progress, &a.ExtractState, &a.ExtractError, &a.Tags, &a.Note, &a.AISummary, &a.NotesNoteID, &a.NotesSyncedAt, &a.Source, &a.ExternalID,
		&a.CreatedAt, &a.UpdatedAt, &a.FeedTitle, &a.FeedIcon, &a.Highlights, key)
	if err != nil {
		return nil, err
	}
	if feed.Valid {
		v := feed.Int64
		a.FeedID = &v
	}
	if a.SiteName == "" {
		a.SiteName = a.FeedTitle
	}
	return &a, nil
}

// ParseCursor splits a cursor from ListArticles.
func ParseCursor(c string) (string, int64) {
	i := strings.LastIndex(c, "|")
	if i < 0 {
		return "", 0
	}
	var id int64
	_, _ = fmt.Sscan(c[i+1:], &id)
	return c[:i], id
}

var reFTSWord = regexp.MustCompile(`[\p{L}\p{N}_]+`)

// ftsQuery turns what a person typed into an FTS5 query: every word must
// appear, the last one may be a prefix; "quoted phrases" stay phrases.
func ftsQuery(q string) string {
	q = strings.TrimSpace(q)
	if q == "" {
		return ""
	}
	var parts []string
	for i, chunk := range strings.Split(q, `"`) {
		if i%2 == 1 {
			if ws := reFTSWord.FindAllString(chunk, -1); len(ws) > 0 {
				parts = append(parts, `"`+strings.Join(ws, " ")+`"`)
			}
			continue
		}
		for _, w := range reFTSWord.FindAllString(chunk, -1) {
			parts = append(parts, `"`+w+`"`)
		}
	}
	if len(parts) == 0 {
		return ""
	}
	if last := parts[len(parts)-1]; !strings.Contains(strings.Trim(last, `"`), " ") && !strings.HasSuffix(q, `"`) {
		parts[len(parts)-1] = last + "*"
	}
	return strings.Join(parts, " ")
}

// ArticleState is a change to how an article stands for the person.
type ArticleState struct {
	Read     *bool    `json:"read"`
	Starred  *bool    `json:"starred"`
	Saved    *bool    `json:"saved"`
	Archived *bool    `json:"archived"`
	Progress *float64 `json:"progress"`
	Tags     *string  `json:"tags"`
	Note     *string  `json:"note"`
	Title    *string  `json:"title"`
}

// UpdateArticle applies a state change to one or more of a person's articles.
func (s *Store) UpdateArticle(ctx context.Context, uid int64, ids []int64, st ArticleState) (int, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	now := Now()
	var sets []string
	var args []any
	flag := func(col string, v *bool) {
		if v == nil {
			return
		}
		sets = append(sets, col+" = ?", col+"_at = CASE WHEN ? = 1 THEN (CASE WHEN "+col+" = 1 AND "+col+"_at <> '' THEN "+col+"_at ELSE ? END) ELSE '' END")
		args = append(args, boolInt(*v), boolInt(*v), now)
	}
	flag("read", st.Read)
	flag("starred", st.Starred)
	flag("saved", st.Saved)
	flag("archived", st.Archived)
	if st.Saved != nil && !*st.Saved {
		sets = append(sets, "archived = 0", "archived_at = ''")
	}
	if st.Progress != nil {
		p := *st.Progress
		if p < 0 {
			p = 0
		}
		if p > 1 {
			p = 1
		}
		sets = append(sets, "progress = ?")
		args = append(args, p)
	}
	if st.Tags != nil {
		sets = append(sets, "tags = ?")
		args = append(args, JoinTags(*st.Tags))
	}
	if st.Note != nil {
		sets = append(sets, "note = ?")
		args = append(args, strings.TrimSpace(*st.Note))
	}
	if st.Title != nil && strings.TrimSpace(*st.Title) != "" {
		sets = append(sets, "title = ?")
		args = append(args, strings.TrimSpace(*st.Title))
	}
	if len(sets) == 0 {
		return 0, nil
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, now, uid)
	ph := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	for _, id := range ids {
		args = append(args, id)
	}
	res, err := s.DB.ExecContext(ctx, `UPDATE articles SET `+strings.Join(sets, ", ")+` WHERE user_id = ? AND id IN (`+ph+`)`, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	if st.Tags != nil || (st.Title != nil && strings.TrimSpace(*st.Title) != "") {
		for _, id := range ids {
			_ = s.Reindex(ctx, uid, id)
		}
	}
	return int(n), nil
}

// Reindex refreshes an article's search entry.
func (s *Store) Reindex(ctx context.Context, uid, id int64) error {
	a, err := s.GetArticle(ctx, uid, id)
	if err != nil {
		return err
	}
	text := PlainText(a.Content)
	if text == "" {
		text = a.Excerpt
	}
	return index(ctx, s.DB, id, a.Title, text+" "+a.Note, a.Tags)
}

// SetAISummary keeps the AI's summary of an article.
func (s *Store) SetAISummary(ctx context.Context, uid, id int64, text string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE articles SET ai_summary = ?, updated_at = ? WHERE user_id = ? AND id = ?`, text, Now(), uid, id)
	return err
}

// SetNotesNote remembers which Pocket Notes note holds an article's highlights.
func (s *Store) SetNotesNote(ctx context.Context, uid, id, noteID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE articles SET notes_note_id = ?, notes_synced_at = ? WHERE user_id = ? AND id = ?`, noteID, Now(), uid, id)
	return err
}

// MarkAllRead marks a person's unread posts read: everything, one feed or
// one folder, only those added before `before` (RFC3339; "" = now).
func (s *Store) MarkAllRead(ctx context.Context, uid int64, feedID, folderID int64, before string) (int, error) {
	q := `UPDATE articles SET read = 1, read_at = ?, updated_at = ? WHERE user_id = ? AND read = 0 AND feed_id IS NOT NULL`
	now := Now()
	args := []any{now, now, uid}
	if feedID > 0 {
		q += ` AND feed_id = ?`
		args = append(args, feedID)
	}
	if folderID > 0 {
		q += ` AND feed_id IN (SELECT id FROM feeds WHERE user_id = ? AND folder_id = ?)`
		args = append(args, uid, folderID)
	}
	if before != "" {
		q += ` AND added_at <= ?`
		args = append(args, before)
	}
	res, err := s.DB.ExecContext(ctx, q, args...)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return int(n), nil
}

// DeleteArticle removes one of a person's articles. A feed post leaves a
// tombstone so the feed does not bring it back.
func (s *Store) DeleteArticle(ctx context.Context, uid, id int64) error {
	a, err := s.GetArticle(ctx, uid, id)
	if err != nil {
		return err
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if a.FeedID != nil && a.GUID != "" {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO gone (user_id, feed_id, guid, at) VALUES (?, ?, ?, ?)`, uid, *a.FeedID, a.GUID, Now()); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM articles_fts WHERE rowid = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM articles WHERE user_id = ? AND id = ?`, uid, id); err != nil {
		return err
	}
	return tx.Commit()
}

// Prune removes a person's old feed posts: read ones after readDays, unread
// ones after unreadDays (0 = never). Starred, saved, noted or highlighted
// posts are kept. Tombstones older than a year are dropped.
func (s *Store) Prune(ctx context.Context, uid int64, readDays, unreadDays int) (int, error) {
	total := 0
	run := func(cond string, days int) error {
		if days <= 0 {
			return nil
		}
		cut := time.Now().AddDate(0, 0, -days).UTC().Format(time.RFC3339)
		keep := `a.starred = 0 AND a.saved = 0 AND a.note = '' AND NOT EXISTS (SELECT 1 FROM highlights h WHERE h.article_id = a.id)`
		sel := `SELECT a.id FROM articles a WHERE a.user_id = ? AND a.feed_id IS NOT NULL AND ` + cond + ` AND ` + keep
		tx, err := s.DB.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer func() { _ = tx.Rollback() }()
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO gone (user_id, feed_id, guid, at) SELECT a.user_id, a.feed_id, a.guid, ? FROM articles a
			WHERE a.id IN (`+sel+`) AND a.guid <> ''`, Now(), uid, cut); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx, `DELETE FROM articles_fts WHERE rowid IN (`+sel+`)`, uid, cut); err != nil {
			return err
		}
		res, err := tx.ExecContext(ctx, `DELETE FROM articles WHERE id IN (`+sel+`)`, uid, cut)
		if err != nil {
			return err
		}
		n, _ := res.RowsAffected()
		total += int(n)
		return tx.Commit()
	}
	if err := run(`a.read = 1 AND a.read_at <> '' AND a.read_at < ?`, readDays); err != nil {
		return total, err
	}
	if err := run(`a.read = 0 AND a.added_at < ?`, unreadDays); err != nil {
		return total, err
	}
	_, _ = s.DB.ExecContext(ctx, `DELETE FROM gone WHERE user_id = ? AND at < ?`, uid, time.Now().AddDate(-1, 0, 0).UTC().Format(time.RFC3339))
	return total, nil
}

// Tags lists a person's tags with how many articles carry each.
func (s *Store) Tags(ctx context.Context, uid int64) (map[string]int, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT tags FROM articles WHERE user_id = ? AND tags <> ''`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]int{}
	for rows.Next() {
		var t string
		if err := rows.Scan(&t); err != nil {
			return nil, err
		}
		for _, tag := range SplitTags(t) {
			out[tag]++
		}
	}
	return out, rows.Err()
}

// DayActivity is what a person read, saved and highlighted on one day.
type DayActivity struct {
	Date        string      `json:"date"`
	Read        []Article   `json:"read"`
	Saved       []Article   `json:"saved"`
	Highlights  []Highlight `json:"highlights"`
	ReadCount   int         `json:"read_count"`
	SavedCount  int         `json:"saved_count"`
	ReadMinutes int         `json:"read_minutes"`
}

// Day returns a person's reading on one local day (from/to are its bounds in UTC).
func (s *Store) Day(ctx context.Context, uid int64, date, from, to string) (*DayActivity, error) {
	d := &DayActivity{Date: date, Read: []Article{}, Saved: []Article{}, Highlights: []Highlight{}}
	read, _, err := s.ListArticles(ctx, uid, ArticleFilter{View: "history", From: from, To: to, Limit: 200})
	if err != nil {
		return nil, err
	}
	for _, a := range read {
		// a post counts as read when the person opened it and got some way in, or it was a saved page
		if a.Progress >= 0.3 || a.Saved {
			d.Read = append(d.Read, a)
			d.ReadMinutes += a.Minutes()
		}
	}
	saved, _, err := s.ListArticles(ctx, uid, ArticleFilter{View: "saved", From: from, To: to, Limit: 200})
	if err != nil {
		return nil, err
	}
	arch, _, _ := s.ListArticles(ctx, uid, ArticleFilter{View: "archive", Limit: 200})
	for _, a := range append(saved, arch...) {
		if a.SavedAt >= from && a.SavedAt < to {
			d.Saved = append(d.Saved, a)
		}
	}
	hs, err := s.ListHighlights(ctx, uid, HighlightFilter{From: from, To: to, Limit: 200})
	if err != nil {
		return nil, err
	}
	d.Highlights = hs
	d.ReadCount, d.SavedCount = len(d.Read), len(d.Saved)
	return d, nil
}

func firstNonEmpty(v ...string) string {
	for _, s := range v {
		if s != "" {
			return s
		}
	}
	return ""
}
