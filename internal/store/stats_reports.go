package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"
)

// The stats screen's queries (PLAN §9 A28, admin/stats.cfm and
// admin/statsbyyear.cfm). The as-is ran seven queries on one page and a
// near-copy of each on the by-year page; here every report takes a date
// range, an open range is the whole blog, and the by-year screen is the
// same seven calls with the year's bounds.
//
// Comments are counted the way the rest of the code counts them:
// moderated only, subscribe-only rows excluded (PLAN §9 C06, C09). The
// as-is only added `moderated = 1` when moderation was switched on,
// which made its totals disagree with the site's own counts.

// statsTopDefault is BlogCFC's `top 10` / `limit 10` on every table of
// the stats screen.
const statsTopDefault = 10

// StatsSummary is the general-stats block: the totals for a range, plus
// the first and last entry in it. Views are the entries' own counters,
// so they are the views of the entries posted in the range.
type StatsSummary struct {
	Entries     int
	Views       int
	Comments    int
	Subscribers int
	// FirstPosted and LastPosted are zero when the range holds no entry.
	FirstPosted time.Time
	LastPosted  time.Time
}

// EntryCount is one entry with a number beside it: views, or comments.
type EntryCount struct {
	ID    string
	Title string
	Count int
}

// CategoryCount is one category with a number beside it: entries, or
// comments on its entries.
type CategoryCount struct {
	ID    string
	Name  string
	Count int
}

// CommenterCount is one commenter and how often they commented. The as-is
// grouped by email and name both, so two names on one address are two
// rows; that is kept.
type CommenterCount struct {
	Name  string
	Email string
	Count int
}

// rangeClause builds the bounds for one column. A zero bound is open.
func rangeClause(col string, from, to time.Time) (string, []any) {
	var conds []string
	var args []any
	if !from.IsZero() {
		conds = append(conds, col+" >= ?")
		args = append(args, from.UTC())
	}
	if !to.IsZero() {
		conds = append(conds, col+" <= ?")
		args = append(args, to.UTC())
	}
	if len(conds) == 0 {
		return "", nil
	}
	return strings.Join(conds, " AND "), args
}

// where wraps a set of conditions, any of which may be empty.
func where(parts ...string) string {
	var kept []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			kept = append(kept, p)
		}
	}
	if len(kept) == 0 {
		return ""
	}
	return " WHERE " + strings.Join(kept, " AND ")
}

// StatsSummary returns the general-stats totals for a date range
// (admin/stats.cfm's getTotalEntries, getTotalViews, getTotalComments
// and getTotalSubscribers). The subscriber count is the whole verified
// list whatever the range: a subscriber has no date to filter on.
func (s *Store) StatsSummary(ctx context.Context, from, to time.Time) (StatsSummary, error) {
	var out StatsSummary

	cond, args := rangeClause("posted", from, to)
	var first, last sql.NullTime
	err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*), COALESCE(SUM(views),0), MIN(posted), MAX(posted) FROM entries"+where(cond),
		args...).Scan(&out.Entries, &out.Views, &first, &last)
	if err != nil {
		return out, fmt.Errorf("store: stats summary: %w", err)
	}
	if first.Valid {
		out.FirstPosted = first.Time.UTC()
	}
	if last.Valid {
		out.LastPosted = last.Time.UTC()
	}

	cond, args = rangeClause("e.posted", from, to)
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM comments c
		JOIN entries e ON e.id = c.entry_id`+
		where("c.subscribeonly = 0", "c.moderated = 1", cond), args...).Scan(&out.Comments); err != nil {
		return out, fmt.Errorf("store: stats comments: %w", err)
	}

	if err := s.db.QueryRowContext(ctx,
		"SELECT COUNT(*) FROM subscribers WHERE verified = 1").Scan(&out.Subscribers); err != nil {
		return out, fmt.Errorf("store: stats subscribers: %w", err)
	}
	return out, nil
}

// TopEntriesByViewsBetween is stats.cfm's getTopViews: the most-viewed
// entries posted in the range, highest first.
func (s *Store) TopEntriesByViewsBetween(ctx context.Context, from, to time.Time, limit int) ([]EntryCount, error) {
	cond, args := rangeClause("posted", from, to)
	args = append(args, statsLimit(limit))
	return s.entryCounts(ctx,
		"SELECT id, title, views FROM entries"+where(cond)+" ORDER BY views DESC, posted DESC, id LIMIT ?",
		args...)
}

// TopEntriesByComments is stats.cfm's topCommentedEntries.
func (s *Store) TopEntriesByComments(ctx context.Context, from, to time.Time, limit int) ([]EntryCount, error) {
	cond, args := rangeClause("e.posted", from, to)
	args = append(args, statsLimit(limit))
	return s.entryCounts(ctx, `SELECT e.id, e.title, COUNT(c.id) AS total
		FROM entries e JOIN comments c ON c.entry_id = e.id`+
		where("c.subscribeonly = 0", "c.moderated = 1", cond)+`
		GROUP BY e.id, e.title ORDER BY total DESC, e.title ASC LIMIT ?`, args...)
}

func (s *Store) entryCounts(ctx context.Context, q string, args ...any) ([]EntryCount, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: entry counts: %w", err)
	}
	defer rows.Close()
	var out []EntryCount
	for rows.Next() {
		var ec EntryCount
		if err := rows.Scan(&ec.ID, &ec.Title, &ec.Count); err != nil {
			return nil, fmt.Errorf("store: entry counts: %w", err)
		}
		out = append(out, ec)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: entry counts: %w", err)
	}
	return out, nil
}

// CategoryEntryCounts is stats.cfm's getCategoryCount: how many entries
// each category holds, commonest first. A category with no entry in the
// range does not appear, as the as-is inner join did not show it either.
func (s *Store) CategoryEntryCounts(ctx context.Context, from, to time.Time) ([]CategoryCount, error) {
	cond, args := rangeClause("e.posted", from, to)
	return s.categoryCounts(ctx, `SELECT cat.id, cat.name, COUNT(ec.entry_id) AS total
		FROM categories cat
		JOIN entry_categories ec ON ec.category_id = cat.id
		JOIN entries e ON e.id = ec.entry_id`+where(cond)+`
		GROUP BY cat.id, cat.name ORDER BY total DESC, cat.name ASC`, args...)
}

// TopCategoriesByComments is stats.cfm's topCommentedCategories: the
// categories whose entries drew the most comments.
func (s *Store) TopCategoriesByComments(ctx context.Context, from, to time.Time, limit int) ([]CategoryCount, error) {
	cond, args := rangeClause("e.posted", from, to)
	args = append(args, statsLimit(limit))
	return s.categoryCounts(ctx, `SELECT cat.id, cat.name, COUNT(c.id) AS total
		FROM categories cat
		JOIN entry_categories ec ON ec.category_id = cat.id
		JOIN entries e ON e.id = ec.entry_id
		JOIN comments c ON c.entry_id = e.id`+
		where("c.subscribeonly = 0", "c.moderated = 1", cond)+`
		GROUP BY cat.id, cat.name ORDER BY total DESC, cat.name ASC LIMIT ?`, args...)
}

func (s *Store) categoryCounts(ctx context.Context, q string, args ...any) ([]CategoryCount, error) {
	rows, err := s.db.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, fmt.Errorf("store: category counts: %w", err)
	}
	defer rows.Close()
	var out []CategoryCount
	for rows.Next() {
		var cc CategoryCount
		if err := rows.Scan(&cc.ID, &cc.Name, &cc.Count); err != nil {
			return nil, fmt.Errorf("store: category counts: %w", err)
		}
		out = append(out, cc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: category counts: %w", err)
	}
	return out, nil
}

// TopSearchTermsBetween is stats.cfm's topSearchTerms, bounded by when
// the search was made rather than by an entry's date.
func (s *Store) TopSearchTermsBetween(ctx context.Context, from, to time.Time, limit int) ([]TermCount, error) {
	cond, args := rangeClause("searched", from, to)
	args = append(args, statsLimit(limit))
	rows, err := s.db.QueryContext(ctx, "SELECT term, COUNT(*) AS total FROM search_stats"+
		where(cond)+" GROUP BY term ORDER BY total DESC, term ASC LIMIT ?", args...)
	if err != nil {
		return nil, fmt.Errorf("store: top search terms: %w", err)
	}
	defer rows.Close()
	var out []TermCount
	for rows.Next() {
		var tc TermCount
		if err := rows.Scan(&tc.Term, &tc.Count); err != nil {
			return nil, fmt.Errorf("store: top search terms: %w", err)
		}
		out = append(out, tc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: top search terms: %w", err)
	}
	return out, nil
}

// TopCommenters is stats.cfm's topCommenters: who comments most, by
// address and name together as the as-is grouped them.
func (s *Store) TopCommenters(ctx context.Context, from, to time.Time, limit int) ([]CommenterCount, error) {
	cond, args := rangeClause("e.posted", from, to)
	args = append(args, statsLimit(limit))
	rows, err := s.db.QueryContext(ctx, `SELECT c.name, c.email, COUNT(*) AS total
		FROM comments c JOIN entries e ON e.id = c.entry_id`+
		where("c.subscribeonly = 0", "c.moderated = 1", cond)+`
		GROUP BY c.email, c.name ORDER BY total DESC, c.name ASC LIMIT ?`, args...)
	if err != nil {
		return nil, fmt.Errorf("store: top commenters: %w", err)
	}
	defer rows.Close()
	var out []CommenterCount
	for rows.Next() {
		var cc CommenterCount
		if err := rows.Scan(&cc.Name, &cc.Email, &cc.Count); err != nil {
			return nil, fmt.Errorf("store: top commenters: %w", err)
		}
		out = append(out, cc)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: top commenters: %w", err)
	}
	return out, nil
}

// EntryYears lists the years that hold an entry, newest first, in the
// blog's zone: the by-year screen's links (admin/statsbyyear.cfm, which
// looped from the first entry's year to this one).
func (s *Store) EntryYears(ctx context.Context, loc *time.Location) ([]int, error) {
	if loc == nil {
		loc = time.UTC
	}
	rows, err := s.db.QueryContext(ctx, "SELECT posted FROM entries ORDER BY posted DESC")
	if err != nil {
		return nil, fmt.Errorf("store: entry years: %w", err)
	}
	defer rows.Close()
	var out []int
	seen := map[int]bool{}
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("store: entry years: %w", err)
		}
		y := t.UTC().In(loc).Year()
		if !seen[y] {
			seen[y] = true
			out = append(out, y)
		}
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: entry years: %w", err)
	}
	return out, nil
}

func statsLimit(n int) int {
	if n <= 0 {
		return statsTopDefault
	}
	return n
}
