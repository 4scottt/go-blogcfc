package pods

import (
	"html/template"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/web"
)

// The tag cloud pod: the categories big enough to be a tag, sized by how
// many entries they hold (PLAN §9 D08, client/includes/pods/tagcloud.cfm).

// tagMinimum is tagcloud.cfm's `WHERE entrycount >= 10`.
const tagMinimum = 10

// tagScaleFactor is tagcloud.cfm's scaleFactor: the spread between the
// smallest and the largest count is cut into this many steps, and the
// class boundaries sit one and two steps above the smallest.
const tagScaleFactor = 25

// The five size classes, styled in internal/static/css/site.css at
// 9/11/13/16/20px as the as-is stylesheet had them (PLAN §12).
const (
	smallestTag = "smallestTag"
	smallTag    = "smallTag"
	mediumTag   = "mediumTag"
	largeTag    = "largeTag"
	largestTag  = "largestTag"
)

// tagView is one tag in the cloud.
type tagView struct {
	Name  string
	URL   string
	Class string
}

// tagClass is tagcloud.cfm's ladder, in its order: the smallest and the
// largest counts take the ends, and everything between is measured in
// twenty-fifths of the spread. The comparisons are `>` on a floating
// distribution, as the as-is is, so a count exactly one step above the
// minimum is still a small tag.
func tagClass(count, min, max int) string {
	distribution := float64(max-min) / tagScaleFactor
	switch {
	case count == min:
		return smallestTag
	case count == max:
		return largestTag
	case float64(count) > float64(min)+distribution*2:
		return largeTag
	case float64(count) > float64(min)+distribution:
		return mediumTag
	default:
		return smallTag
	}
}

// tagCloudPod is tagcloud.cfm. Its title is the as-is literal: BlogCFC
// never put this pod's heading in a bundle.
func (m *Module) tagCloudPod(c *podCtx) (string, template.HTML) {
	title := text(c.bundle, "tags", "Tags")
	cats, err := m.store.ListCategories(c.ctx)
	if err != nil {
		return title, fail(TagCloud, err)
	}
	min, max := 0, 0
	var kept []int
	for i, cat := range cats {
		if cat.EntryCount < tagMinimum {
			continue
		}
		if len(kept) == 0 || cat.EntryCount < min {
			min = cat.EntryCount
		}
		if len(kept) == 0 || cat.EntryCount > max {
			max = cat.EntryCount
		}
		kept = append(kept, i)
	}
	var tags []tagView
	for _, i := range kept {
		cat := cats[i]
		tags = append(tags, tagView{
			Name:  strings.ToLower(cat.Name),
			URL:   web.CategoryURL(c.base, cat),
			Class: tagClass(cat.EntryCount, min, max),
		})
	}
	return title, m.exec("tagcloud", tags)
}
