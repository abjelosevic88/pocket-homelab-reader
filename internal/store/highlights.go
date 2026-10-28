package store

import (
	"context"
	"database/sql"
	"strings"
)

const hlCols = `h.id, h.user_id, h.article_id, h.text, h.note, h.color, h.prefix, h.suffix, h.position, h.created_at, h.updated_at,
	a.title, a.url, COALESCE(NULLIF(a.site_name, ''), NULLIF(f.custom_title, ''), NULLIF(f.title, ''), '')`

const hlFrom = ` FROM highlights h JOIN articles a ON a.id = h.article_id LEFT JOIN feeds f ON f.id = a.feed_id `

func scanHighlight(sc interface{ Scan(...any) error }) (*Highlight, error) {
	var h Highlight
	err := sc.Scan(&h.ID, &h.UserID, &h.ArticleID, &h.Text, &h.Note, &h.Color, &h.Prefix, &h.Suffix, &h.Position, &h.CreatedAt, &h.UpdatedAt,
		&h.ArticleTitle, &h.ArticleURL, &h.SiteName)
	return &h, err
}

// HighlightFilter selects highlights.
type HighlightFilter struct {
	ArticleID int64
	Query     string
	From, To  string
	Limit     int
	Offset    int
}

// ListHighlights returns a person's highlights, newest first (in article order for one article).
func (s *Store) ListHighlights(ctx context.Context, uid int64, f HighlightFilter) ([]Highlight, error) {
	where := []string{"h.user_id = ?"}
	args := []any{uid}
	order := "h.created_at DESC, h.id DESC"
	if f.ArticleID > 0 {
		where = append(where, "h.article_id = ?")
		args = append(args, f.ArticleID)
		order = "h.position, h.id"
	}
	if q := strings.TrimSpace(f.Query); q != "" {
		where = append(where, "(h.text LIKE ? OR h.note LIKE ? OR a.title LIKE ?)")
		like := "%" + q + "%"
		args = append(args, like, like, like)
	}
	if f.From != "" {
		where = append(where, "h.created_at >= ?")
		args = append(args, f.From)
	}
	if f.To != "" {
		where = append(where, "h.created_at < ?")
		args = append(args, f.To)
	}
	limit := f.Limit
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT `+hlCols+hlFrom+`WHERE `+strings.Join(where, " AND ")+` ORDER BY `+order+` LIMIT ? OFFSET ?`, append(args, limit, f.Offset)...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Highlight{}
	for rows.Next() {
		h, err := scanHighlight(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *h)
	}
	return out, rows.Err()
}

// GetHighlight returns one of a person's highlights.
func (s *Store) GetHighlight(ctx context.Context, uid, id int64) (*Highlight, error) {
	h, err := scanHighlight(s.DB.QueryRowContext(ctx, `SELECT `+hlCols+hlFrom+`WHERE h.user_id = ? AND h.id = ?`, uid, id))
	if err == sql.ErrNoRows {
		return nil, ErrNotFound
	}
	return h, err
}

// CreateHighlight marks a passage in one of the person's articles.
func (s *Store) CreateHighlight(ctx context.Context, uid int64, h *Highlight) error {
	if _, err := s.GetArticle(ctx, uid, h.ArticleID); err != nil {
		return err
	}
	now := Now()
	res, err := s.DB.ExecContext(ctx, `INSERT INTO highlights (user_id, article_id, text, note, color, prefix, suffix, position, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`, uid, h.ArticleID, h.Text, h.Note, h.Color, h.Prefix, h.Suffix, h.Position, firstNonEmpty(h.CreatedAt, now), now)
	if err != nil {
		return err
	}
	h.ID, _ = res.LastInsertId()
	h.UserID = uid
	_, _ = s.DB.ExecContext(ctx, `UPDATE articles SET updated_at = ? WHERE id = ?`, now, h.ArticleID)
	got, err := s.GetHighlight(ctx, uid, h.ID)
	if err == nil {
		*h = *got
	}
	return err
}

// UpdateHighlight changes a highlight's note or colour.
func (s *Store) UpdateHighlight(ctx context.Context, uid int64, h *Highlight) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE highlights SET note = ?, color = ?, updated_at = ? WHERE user_id = ? AND id = ?`, h.Note, h.Color, Now(), uid, h.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteHighlight removes a highlight.
func (s *Store) DeleteHighlight(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM highlights WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// ---- rules ----

// ListRules returns a person's rules.
func (s *Store) ListRules(ctx context.Context, uid int64) ([]Rule, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, name, feed_id, folder_id, field, words, action, tag, enabled, hits, last_hit_at, created_at
		FROM rules WHERE user_id = ? ORDER BY id`, uid)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Rule{}
	for rows.Next() {
		var r Rule
		var feed, folder sql.NullInt64
		if err := rows.Scan(&r.ID, &r.Name, &feed, &folder, &r.Field, &r.Words, &r.Action, &r.Tag, &r.Enabled, &r.Hits, &r.LastHitAt, &r.CreatedAt); err != nil {
			return nil, err
		}
		if feed.Valid {
			v := feed.Int64
			r.FeedID = &v
		}
		if folder.Valid {
			v := folder.Int64
			r.FolderID = &v
		}
		r.UserID = uid
		out = append(out, r)
	}
	return out, rows.Err()
}

// CreateRule adds a rule.
func (s *Store) CreateRule(ctx context.Context, uid int64, r *Rule) error {
	res, err := s.DB.ExecContext(ctx, `INSERT INTO rules (user_id, name, feed_id, folder_id, field, words, action, tag, enabled) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		uid, r.Name, nullI(r.FeedID), nullI(r.FolderID), r.Field, r.Words, r.Action, r.Tag, boolInt(r.Enabled))
	if err != nil {
		return err
	}
	r.ID, _ = res.LastInsertId()
	r.UserID = uid
	r.CreatedAt = Now()
	return nil
}

// UpdateRule saves a rule.
func (s *Store) UpdateRule(ctx context.Context, uid int64, r *Rule) error {
	res, err := s.DB.ExecContext(ctx, `UPDATE rules SET name = ?, feed_id = ?, folder_id = ?, field = ?, words = ?, action = ?, tag = ?, enabled = ? WHERE user_id = ? AND id = ?`,
		r.Name, nullI(r.FeedID), nullI(r.FolderID), r.Field, r.Words, r.Action, r.Tag, boolInt(r.Enabled), uid, r.ID)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// DeleteRule removes a rule.
func (s *Store) DeleteRule(ctx context.Context, uid, id int64) error {
	res, err := s.DB.ExecContext(ctx, `DELETE FROM rules WHERE user_id = ? AND id = ?`, uid, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

// RuleHit counts a rule's matches.
func (s *Store) RuleHit(ctx context.Context, id int64, n int) {
	_, _ = s.DB.ExecContext(ctx, `UPDATE rules SET hits = hits + ?, last_hit_at = ? WHERE id = ?`, n, Now(), id)
}
