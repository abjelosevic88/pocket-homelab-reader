package store

import (
	"context"
	"database/sql"
	"strings"
	"time"
)

// ---- folders ----

// ListFolders returns a person's folders with their unread counts.
func (s *Store) ListFolders(ctx context.Context, uid int64) ([]Folder, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT f.id, f.name, f.sort, f.created_at,
		(SELECT COUNT(1) FROM articles a JOIN feeds d ON d.id = a.feed_id WHERE a.user_id = f.user_id AND d.folder_id = f.id AND a.read = 0)
		FROM folders f WHERE f.user_id = ? ORDER BY f.sort, f.name COLLATE NOCASE`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Folder{}
	for rows.Next() {
		var f Folder
		if err := rows.Scan(&f.ID, &f.Name, &f.Sort, &f.CreatedAt, &f.Unread); err != nil {
			return nil, err
		}
		f.UserID = uid
		out = append(out, f)
	}
	return out, rows.Err()
}

// GetFolder returns one of a person's folders.
func (s *Store) GetFolder(ctx context.Context, uid, id int64) (*Folder, error) {
	var f Folder
	err := s.DB.QueryRowContext(ctx, `SELECT id, name, sort, created_at FROM folders WHERE user_id = ? AND id = ?`, uid, id).Scan(&f.ID, &f.Name, &f.Sort, &f.CreatedAt)
	if err != nil {
		return nil, notFound(err)
	}
	f.UserID = uid
	return &f, nil
}

// FolderByName finds a folder by name (any case), creating it when create is set.
func (s *Store) FolderByName(ctx context.Context, uid int64, name string, create bool) (*Folder, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrNotFound
	}
	var id int64
	err := s.DB.QueryRowContext(ctx, `SELECT id FROM folders WHERE user_id = ? AND name = ? COLLATE NOCASE`, uid, name).Scan(&id)
	if err == nil {
		return s.GetFolder(ctx, uid, id)
	}
	if err != sql.ErrNoRows || !create {
		return nil, notFound(err)
	}
	f := &Folder{Name: name}
	if err := s.CreateFolder(ctx, uid, f); err != nil {
		return nil, err
	}
	return f, nil
}

// CreateFolder adds a folder at the end.
func (s *Store) CreateFolder(ctx context.Context, uid int64, f *Folder) error {
	if f.Sort == 0 {
		_ = s.DB.QueryRowContext(ctx, `SELECT COALESCE(MAX(sort), 0) + 1 FROM folders WHERE user_id = ?`, uid).Scan(&f.Sort)
	}
	res, err := s.DB.ExecContext(ctx, `INSERT INTO folders (user_id, name, sort) VALUES (?, ?, ?)`, uid, strings.TrimSpace(f.Name), f.Sort)
	if err != nil {
		return conflict(err)
	}
	f.ID, _ = res.LastInsertId()
	f.UserID = uid
	f.CreatedAt = Now()
	return nil
}

// UpdateFolder renames or moves a folder.
func (s *Store) UpdateFolder(ctx context.Context, uid int64, f *Folder) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE folders SET name = ?, sort = ? WHERE user_id = ? AND id = ?`, strings.TrimSpace(f.Name), f.Sort, uid, f.ID)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFolder removes a folder; its feeds stay, outside any folder.
func (s *Store) DeleteFolder(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM folders WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- feeds ----

const feedCols = `f.id, f.user_id, f.folder_id, f.url, f.site_url, f.title, f.custom_title, f.description, f.icon_url, f.kind,
	f.full_text, f.notify, f.paused, f.refresh_min, f.etag, f.last_modified, f.fetched_at, f.next_fetch_at, f.newest_at,
	f.error, f.error_count, f.source, f.created_at, f.updated_at`

func scanFeed(sc interface{ Scan(...any) error }, extra ...any) (*Feed, error) {
	var f Feed
	var folder sql.NullInt64
	dst := []any{&f.ID, &f.UserID, &folder, &f.URL, &f.SiteURL, &f.Title, &f.CustomTitle, &f.Description, &f.IconURL, &f.Kind,
		&f.FullText, &f.Notify, &f.Paused, &f.RefreshMin, &f.ETag, &f.LastModified, &f.FetchedAt, &f.NextFetchAt, &f.NewestAt,
		&f.Error, &f.ErrorCount, &f.Source, &f.CreatedAt, &f.UpdatedAt}
	if err := sc.Scan(append(dst, extra...)...); err != nil {
		return nil, err
	}
	if folder.Valid {
		v := folder.Int64
		f.FolderID = &v
	}
	return &f, nil
}

// ListFeeds returns a person's feeds with unread and total counts.
func (s *Store) ListFeeds(ctx context.Context, uid int64) ([]Feed, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+feedCols+`,
		(SELECT COUNT(1) FROM articles a WHERE a.feed_id = f.id AND a.read = 0),
		(SELECT COUNT(1) FROM articles a WHERE a.feed_id = f.id)
		FROM feeds f WHERE f.user_id = ? ORDER BY COALESCE(NULLIF(f.custom_title, ''), NULLIF(f.title, ''), f.url) COLLATE NOCASE`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Feed{}
	for rows.Next() {
		var unread, total int
		f, err := scanFeed(rows, &unread, &total)
		if err != nil {
			return nil, err
		}
		f.Unread, f.Total = unread, total
		out = append(out, *f)
	}
	return out, rows.Err()
}

// GetFeed returns one of a person's feeds.
func (s *Store) GetFeed(ctx context.Context, uid, id int64) (*Feed, error) {
	f, err := scanFeed(s.DB.QueryRowContext(ctx, `SELECT `+feedCols+` FROM feeds f WHERE f.user_id = ? AND f.id = ?`, uid, id))
	return f, notFound(err)
}

// FeedByURL finds a person's feed by its address.
func (s *Store) FeedByURL(ctx context.Context, uid int64, url string) (*Feed, error) {
	f, err := scanFeed(s.DB.QueryRowContext(ctx, `SELECT `+feedCols+` FROM feeds f WHERE f.user_id = ? AND f.url = ?`, uid, url))
	return f, notFound(err)
}

// CreateFeed subscribes; it is due for its first fetch at once.
func (s *Store) CreateFeed(ctx context.Context, uid int64, f *Feed) error {
	if f.Kind == "" {
		f.Kind = "web"
	}
	if f.Source == "" {
		f.Source = "app"
	}
	now := Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO feeds (user_id, folder_id, url, site_url, title, custom_title, description, icon_url, kind,
		full_text, notify, paused, refresh_min, next_fetch_at, source, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, nullI(f.FolderID), f.URL, f.SiteURL, f.Title, f.CustomTitle, f.Description, f.IconURL, f.Kind,
		boolInt(f.FullText), boolInt(f.Notify), boolInt(f.Paused), f.RefreshMin, "", f.Source, now, now)
	if err != nil {
		return conflict(err)
	}
	f.ID, _ = res.LastInsertId()
	f.UserID = uid
	f.CreatedAt, f.UpdatedAt = now, now
	return nil
}

// UpdateFeed saves what the person can change about a feed.
func (s *Store) UpdateFeed(ctx context.Context, uid int64, f *Feed) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE feeds SET folder_id = ?, url = ?, custom_title = ?, full_text = ?, notify = ?, paused = ?, refresh_min = ?, updated_at = ?
		WHERE user_id = ? AND id = ?`, nullI(f.FolderID), f.URL, f.CustomTitle, boolInt(f.FullText), boolInt(f.Notify), boolInt(f.Paused), f.RefreshMin, Now(), uid, f.ID)
	if err != nil {
		return conflict(err)
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteFeed unsubscribes. Posts the person kept (starred, saved, highlighted
// or with a note) stay as saved pages; the rest goes with the feed.
func (s *Store) DeleteFeed(ctx context.Context, uid, id int64) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	var n int
	if err := tx.QueryRowContext(ctx, `SELECT COUNT(1) FROM feeds WHERE user_id = ? AND id = ?`, uid, id).Scan(&n); err != nil {
		return err
	}
	if n == 0 {
		return ErrNotFound
	}
	// keep what the person kept, as saved pages (one per address)
	if _, err := tx.ExecContext(ctx, `UPDATE articles SET feed_id = NULL, guid = '', saved = 1, saved_at = CASE WHEN saved_at = '' THEN ? ELSE saved_at END
		WHERE user_id = ? AND feed_id = ? AND url <> '' AND (starred = 1 OR saved = 1 OR note <> '' OR EXISTS (SELECT 1 FROM highlights h WHERE h.article_id = articles.id))
		AND NOT EXISTS (SELECT 1 FROM articles b WHERE b.user_id = articles.user_id AND b.feed_id IS NULL AND b.url = articles.url)
		AND articles.id = (SELECT MIN(c.id) FROM articles c WHERE c.user_id = articles.user_id AND c.feed_id = articles.feed_id AND c.url = articles.url)`, Now(), uid, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM articles_fts WHERE rowid IN (SELECT id FROM articles WHERE user_id = ? AND feed_id = ?)`, uid, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM articles WHERE user_id = ? AND feed_id = ?`, uid, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM feeds WHERE user_id = ? AND id = ?`, uid, id); err != nil {
		return err
	}
	return tx.Commit()
}

// DueFeeds returns up to n feeds (anyone's) whose next check is due.
func (s *Store) DueFeeds(ctx context.Context, n int) ([]Feed, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT `+feedCols+` FROM feeds f WHERE f.paused = 0 AND (f.next_fetch_at = '' OR f.next_fetch_at <= ?)
		ORDER BY f.next_fetch_at LIMIT ?`, Now(), n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Feed
	for rows.Next() {
		f, err := scanFeed(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// FeedFetched records a successful check. Empty values keep what is stored.
func (s *Store) FeedFetched(ctx context.Context, f *Feed, next time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE feeds SET title = CASE WHEN ? <> '' THEN ? ELSE title END,
		site_url = CASE WHEN ? <> '' THEN ? ELSE site_url END, description = CASE WHEN ? <> '' THEN ? ELSE description END,
		icon_url = CASE WHEN ? <> '' THEN ? ELSE icon_url END, kind = ?, etag = ?, last_modified = ?, fetched_at = ?, next_fetch_at = ?,
		newest_at = CASE WHEN ? > newest_at THEN ? ELSE newest_at END, error = '', error_count = 0, updated_at = ? WHERE id = ?`,
		f.Title, f.Title, f.SiteURL, f.SiteURL, f.Description, f.Description, f.IconURL, f.IconURL, f.Kind, f.ETag, f.LastModified,
		Now(), next.UTC().Format(time.RFC3339), f.NewestAt, f.NewestAt, Now(), f.ID)
	return err
}

// FeedFailed records a failed check; next is the retry time.
func (s *Store) FeedFailed(ctx context.Context, id int64, msg string, next time.Time) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE feeds SET error = ?, error_count = error_count + 1, fetched_at = ?, next_fetch_at = ? WHERE id = ?`,
		msg, Now(), next.UTC().Format(time.RFC3339), id)
	return err
}

// FeedDueNow makes a person's feeds (one, or all with id 0) due at once.
func (s *Store) FeedDueNow(ctx context.Context, uid, id int64) error {
	q := `UPDATE feeds SET next_fetch_at = '' WHERE user_id = ? AND paused = 0`
	args := []any{uid}
	if id > 0 {
		q += ` AND id = ?`
		args = append(args, id)
	}
	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}

// FeedCounts are the sidebar numbers.
type FeedCounts struct {
	Unread      int `json:"unread"`
	Today       int `json:"today"`
	Saved       int `json:"saved"`
	Starred     int `json:"starred"`
	Highlights  int `json:"highlights"`
	Feeds       int `json:"feeds"`
	Failing     int `json:"failing"`
	ReadToday   int `json:"read_today"`
	SavedToday  int `json:"saved_today"`
	ReadMinutes int `json:"read_minutes_today"`
}

// Counts returns a person's sidebar numbers; since is the start of today (UTC RFC3339).
func (s *Store) FeedCountsFor(ctx context.Context, uid int64, since string) (FeedCounts, error) {
	var c FeedCounts
	err := s.DB.QueryRowContext(ctx, `SELECT
		(SELECT COUNT(1) FROM articles WHERE user_id = ? AND feed_id IS NOT NULL AND read = 0),
		(SELECT COUNT(1) FROM articles WHERE user_id = ? AND feed_id IS NOT NULL AND read = 0 AND added_at >= ?),
		(SELECT COUNT(1) FROM articles WHERE user_id = ? AND saved = 1 AND archived = 0),
		(SELECT COUNT(1) FROM articles WHERE user_id = ? AND starred = 1),
		(SELECT COUNT(1) FROM highlights WHERE user_id = ?),
		(SELECT COUNT(1) FROM feeds WHERE user_id = ?),
		(SELECT COUNT(1) FROM feeds WHERE user_id = ? AND error <> '' AND error_count >= 3),
		(SELECT COUNT(1) FROM articles WHERE user_id = ? AND read = 1 AND read_at >= ?),
		(SELECT COUNT(1) FROM articles WHERE user_id = ? AND saved = 1 AND saved_at >= ?),
		(SELECT COALESCE(SUM((words + 229) / 230), 0) FROM articles WHERE user_id = ? AND read = 1 AND read_at >= ? AND progress >= 0.5)`,
		uid, uid, since, uid, uid, uid, uid, uid, uid, since, uid, since, uid, since).Scan(
		&c.Unread, &c.Today, &c.Saved, &c.Starred, &c.Highlights, &c.Feeds, &c.Failing, &c.ReadToday, &c.SavedToday, &c.ReadMinutes)
	return c, err
}
