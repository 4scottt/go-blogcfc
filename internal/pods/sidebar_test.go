package pods

import (
	"context"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/config"
)

// TestFP_D10_PodVisibilityAndOrder: the `pods` setting decides which pods
// show and in what order, an unknown name is ignored, and an empty
// setting is pods.xml's list (PLAN §9 D10, §10 "Pods").
func TestFP_D10_PodVisibilityAndOrder(t *testing.T) {
	s := newTestSite(t)
	ctx := context.Background()

	// Unset: the as-is default set and order.
	if err := s.settings.Set(ctx, map[string]string{"pods": ""}); err != nil {
		t.Fatalf("clear pods: %v", err)
	}
	want := []string{"Calendar", "Subscribe", "Recent Comments", "Recent Entries", "Archives by Subject"}
	if got := titles(s.sidebar("/")); !reflect.DeepEqual(got, want) {
		t.Errorf("the default sidebar is %v, want %v", got, want)
	}

	// Hidden and reordered, with an unknown name in the middle and the
	// list out of order: Pods() sorts by `order`, the hidden pod and the
	// unknown one are skipped.
	if err := s.settings.SetPods(ctx, []config.Pod{
		{Name: Archives, Show: true, Order: 30},
		{Name: "tweetbacks", Show: true, Order: 20},
		{Name: Calendar, Show: false, Order: 1},
		{Name: Recent, Show: true, Order: 10},
	}); err != nil {
		t.Fatalf("set pods: %v", err)
	}
	want = []string{"Recent Entries", "Archives by Subject"}
	if got := titles(s.sidebar("/")); !reflect.DeepEqual(got, want) {
		t.Errorf("the sidebar is %v, want %v", got, want)
	}

	// BlogCFC's own pods.xml spelling still resolves.
	if err := s.settings.SetPods(ctx, []config.Pod{{Name: "Calendar.cfm", Show: true, Order: 1}}); err != nil {
		t.Fatalf("set pods: %v", err)
	}
	if got := titles(s.sidebar("/")); !reflect.DeepEqual(got, []string{"Calendar"}) {
		t.Errorf("the sidebar is %v, want the calendar", got)
	}
}

// boxPattern is the sidebar's furniture (PLAN §12): every pod is one
// li.block holding a box with a titlewrap h4 and the-content. The pods'
// own lists use li too, so a block is matched from its opening tag
// rather than counted by its closing one.
var boxPattern = regexp.MustCompile(
	`(?s)^<div class="box"><div class="titlewrap"><h4>[^<]*</h4></div><div class="the-content">.*</div></div></li>$`)

// TestFP_D10_EveryPodIsABox: all ten pods render inside the same block,
// so the column looks the same whichever ones an operator shows.
func TestFP_D10_EveryPodIsABox(t *testing.T) {
	s := newTestSite(t)
	ctx := context.Background()

	all := []string{Calendar, Archives, MonthlyArchives, Recent, RecentComments,
		Search, Subscribe, TagCloud, RSS, Pages}
	var pods []config.Pod
	for i, name := range all {
		pods = append(pods, config.Pod{Name: name, Show: true, Order: i + 1})
	}
	if err := s.settings.SetPods(ctx, pods); err != nil {
		t.Fatalf("set pods: %v", err)
	}

	got := s.sidebar("/")
	if n := strings.Count(got, `<li class="block">`); n != len(all) {
		t.Fatalf("the sidebar holds %d blocks, want %d:\n%s", n, len(all), got)
	}
	blocks := strings.Split(got, `<li class="block">`)
	if len(blocks) != len(all)+1 {
		t.Fatalf("the sidebar splits into %d blocks, want %d", len(blocks)-1, len(all))
	}
	for _, block := range blocks[1:] {
		if !boxPattern.MatchString(strings.TrimSpace(block)) {
			t.Errorf("a pod is not in the sidebar's block:\n%s", block)
		}
	}
}
