package store_test

import (
	"context"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// TestSearchStatsRankTerms: what the search page logs and what the stats
// screen reads back.
func TestSearchStatsRankTerms(t *testing.T) {
	st := testdb.New(t)
	ctx := context.Background()

	for _, term := range []string{"coldfusion", "coldfusion", " coldfusion ", "go", "go", "sqlite", ""} {
		if err := st.LogSearch(ctx, term); err != nil {
			t.Fatalf("LogSearch(%q): %v", term, err)
		}
	}
	// A term longer than the column is truncated, not refused.
	long := strings.Repeat("x", 400)
	if err := st.LogSearch(ctx, long); err != nil {
		t.Fatalf("LogSearch(long): %v", err)
	}

	top, err := st.TopSearchTerms(ctx, 3)
	if err != nil {
		t.Fatalf("TopSearchTerms: %v", err)
	}
	if len(top) != 3 {
		t.Fatalf("top = %+v, want three rows", top)
	}
	if top[0].Term != "coldfusion" || top[0].Count != 3 {
		t.Errorf("first = %+v, want coldfusion counted three times (the padded one is the same term)", top[0])
	}
	if top[1].Term != "go" || top[1].Count != 2 {
		t.Errorf("second = %+v, want go twice", top[1])
	}
	if top[2].Count != 1 {
		t.Errorf("third = %+v, want a single search", top[2])
	}

	all, err := st.TopSearchTerms(ctx, 0)
	if err != nil {
		t.Fatalf("TopSearchTerms(0): %v", err)
	}
	if len(all) != 4 {
		t.Fatalf("with the default limit, terms = %+v, want four (the blank search is not one)", all)
	}
	for _, tc := range all {
		if len([]rune(tc.Term)) > 255 {
			t.Errorf("a term of %d characters was stored", len([]rune(tc.Term)))
		}
	}
}
