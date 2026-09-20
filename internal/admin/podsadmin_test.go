package admin_test

import (
	"context"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/pods"
)

// TestFP_A23_PodManagerShowAndOrder covers PLAN §9 A23
// (admin/pods.cfm): the ten pods with a show box and a sort order each,
// a save that writes them in order, and a name in the stored setting
// that is not a pod any more dropped rather than kept. BlogCFC's pod
// manager also uploaded, edited and deleted `.cfm` files; the rewrite
// ships a fixed widget set (PLAN §7).
func TestFP_A23_PodManagerShowAndOrder(t *testing.T) {
	h := newHarness(t)
	h.user("admin", "Admin")
	h.login("admin")
	ctx := context.Background()
	flushes := countFlushes(h)

	// A stored setting from an older blog: a pod that no longer exists
	// (BlogCFC's feed.cfm, PLAN §9 D) beside one that does.
	if err := h.settings.SetPods(ctx, []config.Pod{
		{Name: "feed", Show: true, Order: 1},
		{Name: pods.Calendar, Show: true, Order: 2},
	}); err != nil {
		t.Fatalf("SetPods: %v", err)
	}

	resp, body := h.get("/admin/pods")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("pods: status %d, want 200", resp.StatusCode)
	}
	if !strings.Contains(body, "<h1>Pods</h1>") {
		t.Error("the pod manager's heading is not Pods")
	}
	every := []string{pods.Calendar, pods.Archives, pods.MonthlyArchives, pods.Recent,
		pods.RecentComments, pods.Search, pods.Subscribe, pods.TagCloud, pods.RSS, pods.Pages}
	for _, name := range every {
		for _, want := range []string{`name="show_` + name + `"`, `name="order_` + name + `"`} {
			if !strings.Contains(body, want) {
				t.Errorf("the pod manager has no %s", want)
			}
		}
	}
	if !strings.Contains(body, `value="Save"`) {
		t.Error("the pod manager has no Save button")
	}
	if !strings.Contains(body, "feed") {
		t.Error("the pod manager does not mention the stored name it is about to drop")
	}
	// The stored row's state is what the screen opens with.
	if !strings.Contains(body, `name="show_`+pods.Calendar+`" value="1" checked`) {
		t.Error("the calendar pod is stored as shown but the box is not ticked")
	}

	// Save: two pods on, in an order that is not the screen's.
	form := url.Values{"save": {"Save"}}
	for i, name := range every {
		form.Set("order_"+name, strconv.Itoa(i+10))
	}
	form.Set("show_"+pods.Search, "1")
	form.Set("order_"+pods.Search, "1")
	form.Set("show_"+pods.Calendar, "1")
	form.Set("order_"+pods.Calendar, "5")
	resp, _ = h.postForm("/admin/pods", form)
	redirectedTo(t, resp, "/admin/pods?saved=1")
	if *flushes != 1 {
		t.Errorf("the flush hook ran %d times on a save, want 1", *flushes)
	}

	stored := h.settings.Pods()
	if len(stored) != len(every) {
		t.Fatalf("the setting holds %d pods, want the %d known ones", len(stored), len(every))
	}
	if stored[0].Name != pods.Search || stored[0].Order != 1 || !stored[0].Show {
		t.Errorf("the first stored pod is %+v, want search shown with order 1", stored[0])
	}
	shown := map[string]config.Pod{}
	for i, p := range stored {
		if p.Name == "feed" {
			t.Error("the unknown pod `feed` survived the save")
		}
		if i > 0 && stored[i-1].Order > p.Order {
			t.Errorf("the stored list is not sorted by order: %+v then %+v", stored[i-1], p)
		}
		if p.Show {
			shown[p.Name] = p
		}
	}
	if len(shown) != 2 {
		t.Errorf("%d pods are shown, want the two that were ticked", len(shown))
	}
	if cal, ok := shown[pods.Calendar]; !ok || cal.Order != 5 {
		t.Errorf("the calendar pod is %+v, want shown with order 5", cal)
	}
	if _, ok := shown[pods.Recent]; ok {
		t.Error("an unticked pod was saved as shown")
	}

	// A sort order that is not a number is refused with the form back,
	// and nothing is written.
	before := h.settings.Pods()
	form.Set("order_"+pods.RSS, "soon")
	resp, body = h.postForm("/admin/pods", form)
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "has to be a number") {
		t.Errorf("a non-numeric sort order: status %d, want the form back with a message", resp.StatusCode)
	}
	if after := h.settings.Pods(); len(after) != len(before) || after[0].Name != before[0].Name {
		t.Error("a refused save changed the setting")
	}
	if *flushes != 1 {
		t.Errorf("a refused save flushed the caches (%d flushes)", *flushes)
	}
}
