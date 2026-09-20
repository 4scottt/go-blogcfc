package store

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TermCount is one row of the top search terms report (PLAN §9 A28).
type TermCount struct {
	Term  string
	Count int
}

// searchTermMax is the `term` column's width, in characters.
const searchTermMax = 255

// topSearchTermsDefault is BlogCFC's `limit 10` on the stats screen.
const topSearchTermsDefault = 10

// LogSearch records one search, as BlogCFC's logSearch does. A blank term
// is not a search and is not recorded; a term longer than the column is
// truncated rather than refused, so a stray long query never fails the
// search page.
func (s *Store) LogSearch(ctx context.Context, term string) error {
	term = strings.TrimSpace(term)
	if term == "" {
		return nil
	}
	if r := []rune(term); len(r) > searchTermMax {
		term = string(r[:searchTermMax])
	}
	if _, err := s.db.ExecContext(ctx,
		"INSERT INTO search_stats (term, searched) VALUES (?, ?)",
		term, time.Now().UTC().Truncate(time.Second)); err != nil {
		return fmt.Errorf("store: log search: %w", err)
	}
	return nil
}

// TopSearchTerms returns the most searched terms, commonest first and
// alphabetical within a tie.
func (s *Store) TopSearchTerms(ctx context.Context, n int) ([]TermCount, error) {
	if n <= 0 {
		n = topSearchTermsDefault
	}
	rows, err := s.db.QueryContext(ctx, `SELECT term, COUNT(*) AS total FROM search_stats
		GROUP BY term ORDER BY total DESC, term ASC LIMIT ?`, n)
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
