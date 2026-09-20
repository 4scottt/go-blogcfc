package pods

import (
	"strings"
	"testing"
)

// TestFP_D08_TagCloudScale is tagcloud.cfm's 25-step scale: the ends take
// the smallest and largest classes, and the three middle classes are cut
// at one and two twenty-fifths of the spread above the smallest count
// (PLAN §9 D08).
func TestFP_D08_TagCloudScale(t *testing.T) {
	cases := []struct {
		name            string
		count, min, max int
		want            string
	}{
		{"the only tag is both ends", 12, 12, 12, smallestTag},
		{"the smallest", 10, 10, 300, smallestTag},
		{"the largest", 300, 10, 300, largestTag},
		// spread 290, one step 11.6: 21 is small, 22 is medium.
		{"one step is still small", 21, 10, 300, smallTag},
		{"just over one step is medium", 22, 10, 300, mediumTag},
		// two steps is 33.2: 33 is medium, 34 is large.
		{"two steps is still medium", 33, 10, 300, mediumTag},
		{"just over two steps is large", 34, 10, 300, largeTag},
		{"the top of the middle is large", 299, 10, 300, largeTag},
		// A narrow spread: every step is a fraction of a count, so
		// anything above the minimum but below the maximum is large.
		{"a narrow spread", 11, 10, 12, largeTag},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := tagClass(tc.count, tc.min, tc.max); got != tc.want {
				t.Errorf("tagClass(%d, %d, %d) = %s, want %s", tc.count, tc.min, tc.max, got, tc.want)
			}
		})
	}
}

// TestFP_D08_TagCloudPod: only the categories with ten entries or more,
// lower-cased, each in its size class and linked to its listing
// (PLAN §9 D08, pods/tagcloud.cfm).
func TestFP_D08_TagCloudPod(t *testing.T) {
	s := newTestSite(t)
	s.only(TagCloud)

	s.categoryWithEntries("ColdFusion", "coldfusion", tagMinimum)
	s.categoryWithEntries("Java", "java", tagMinimum+20)
	s.categoryWithEntries("Small", "small", tagMinimum-1)

	got := s.sidebar("/")
	mustContain(t, got,
		`<h4>Tags</h4>`,
		`<a href="`+testBase+`/coldfusion"><span class="`+smallestTag+`">coldfusion</span></a>`,
		`<a href="`+testBase+`/java"><span class="`+largestTag+`">java</span></a>`,
	)
	// A category under the minimum is not a tag.
	if strings.Contains(got, ">small<") {
		t.Errorf("a category with fewer than %d entries is in the cloud:\n%s", tagMinimum, got)
	}
}
