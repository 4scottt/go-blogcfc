package store

import (
	"context"
	"fmt"
	"sort"
	"time"
)

// MonthCount is one month of the monthly-archives pod: the year, the
// month and how many live entries it holds (PLAN §9 D03).
type MonthCount struct {
	Year  int
	Month time.Month
	Count int
}

// MonthlyArchives groups the live entries by year and month, newest month
// first. `years` is BlogCFC's archiveYears: entries from the calendar year
// (this year - years) onwards, or every year when it is zero or less.
//
// The grouping happens in Go, not in SQL. `posted` is stored in UTC and
// the months a reader sees are the blog's zone's, and a fixed offset in
// SQL is wrong on either side of a DST change; a blog has few enough
// entries to bucket in memory.
func (s *Store) MonthlyArchives(ctx context.Context, years int, loc *time.Location) ([]MonthCount, error) {
	if loc == nil {
		loc = time.UTC
	}
	posted, err := s.livePostedTimes(ctx, "")
	if err != nil {
		return nil, err
	}
	cutoff := 0
	if years > 0 {
		cutoff = time.Now().In(loc).Year() - years
	}
	counts := map[MonthCount]int{}
	for _, t := range posted {
		local := t.In(loc)
		if years > 0 && local.Year() < cutoff {
			continue
		}
		counts[MonthCount{Year: local.Year(), Month: local.Month()}]++
	}
	out := make([]MonthCount, 0, len(counts))
	for k, n := range counts {
		k.Count = n
		out = append(out, k)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Year != out[j].Year {
			return out[i].Year > out[j].Year
		}
		return out[i].Month > out[j].Month
	})
	return out, nil
}

// DaysWithEntries returns the days of that month, in the blog's zone, that
// have a live entry: the calendar pod's active days (PLAN §9 D01).
func (s *Store) DaysWithEntries(ctx context.Context, year int, month time.Month, loc *time.Location) ([]int, error) {
	if loc == nil {
		loc = time.UTC
	}
	// Bound the query by the month in the blog's zone, so a busy blog does
	// not read its whole history to draw one calendar.
	start := time.Date(year, month, 1, 0, 0, 0, 0, loc)
	end := start.AddDate(0, 1, 0)
	posted, err := s.livePostedTimes(ctx, " AND e.posted >= ? AND e.posted < ?", start.UTC(), end.UTC())
	if err != nil {
		return nil, err
	}
	seen := map[int]bool{}
	for _, t := range posted {
		local := t.In(loc)
		if local.Year() == year && local.Month() == month {
			seen[local.Day()] = true
		}
	}
	out := make([]int, 0, len(seen))
	for d := range seen {
		out = append(out, d)
	}
	sort.Ints(out)
	return out, nil
}

// livePostedTimes reads `posted` for every live entry, with an optional
// extra condition on top.
func (s *Store) livePostedTimes(ctx context.Context, extra string, args ...any) ([]time.Time, error) {
	q := "SELECT e.posted FROM entries e WHERE e.released = 1 AND e.posted <= ?" + extra
	all := append([]any{time.Now().UTC()}, args...)
	rows, err := s.db.QueryContext(ctx, q, all...)
	if err != nil {
		return nil, fmt.Errorf("store: archives: %w", err)
	}
	defer rows.Close()
	var out []time.Time
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			return nil, fmt.Errorf("store: archives: %w", err)
		}
		out = append(out, t.UTC())
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("store: archives: %w", err)
	}
	return out, nil
}

// EntriesForSitemap returns every live entry, newest first: what
// /sitemap.xml lists (PLAN §9 P25).
func (s *Store) EntriesForSitemap(ctx context.Context) ([]Entry, error) {
	entries, _, err := s.ListEntries(ctx, EntryFilter{LiveOnly: true, Sort: "posted", Desc: true})
	if err != nil {
		return nil, err
	}
	return entries, nil
}
