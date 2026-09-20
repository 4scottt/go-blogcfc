package admin

import (
	"context"
	"log/slog"
	"strconv"

	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/store"
)

// MenuLink is one link in the admin's left menu. Role, when set, is the
// role a user must hold for the link to appear at all.
type MenuLink struct {
	Label string
	Href  string
	Role  string
}

// MenuGroup is one `<ul>` in #menu. A group whose links are all hidden is
// dropped with them.
type MenuGroup struct {
	Label string
	Links []MenuLink
}

// menuGroups is the menu of PLAN §12, in that order. The as-is menu is a
// flat pile of lists (client/tags/adminlayout.cfm); the plan groups it,
// keeping #menu and the same destinations.
var menuGroups = []MenuGroup{
	{Label: "Blog", Links: []MenuLink{
		{Label: "Entries", Href: "/admin/entries"},
		{Label: "Categories", Href: "/admin/categories", Role: auth.RoleManageCategories},
		{Label: "Comments", Href: "/admin/comments"},
		{Label: "Moderate", Href: "/admin/moderate"},
		{Label: "Subscribers", Href: "/admin/subscribers"},
		{Label: "Mail Subscribers", Href: "/admin/subscribers/mail"},
	}},
	{Label: "Users", Links: []MenuLink{
		{Label: "Users", Href: "/admin/users", Role: auth.RoleManageUsers},
	}},
	{Label: "Content", Links: []MenuLink{
		{Label: "Pages", Href: "/admin/pages", Role: auth.RolePageAdmin},
		{Label: "Textblocks", Href: "/admin/textblocks"},
		{Label: "Pods", Href: "/admin/pods"},
		{Label: "Slideshows", Href: "/admin/slideshows"},
		{Label: "Files", Href: "/admin/files"},
	}},
	{Label: "Reports", Links: []MenuLink{
		{Label: "Stats", Href: "/admin/stats"},
		{Label: "Downloads", Href: "/admin/downloads"},
	}},
	{Label: "Settings", Links: []MenuLink{
		{Label: "Settings", Href: "/admin/settings"},
	}},
	{Label: "Update Password", Links: []MenuLink{
		{Label: "Update Password", Href: "/admin/password"},
	}},
	{Label: "Logout", Links: []MenuLink{
		{Label: "Logout", Href: "/admin/logout"},
	}},
}

// moderateHref is the link whose label carries the moderation count.
const moderateHref = "/admin/moderate"

// menuFor returns the menu as this user may see it, with the moderation
// queue's length in its label — "Moderate (3)", as the as-is menu read
// "Moderate Comments (3)" (client/tags/adminlayout.cfm). The count is one
// small query per admin page render, which is what the as-is did too; a
// failed count drops the number rather than the page.
func (m *Module) menuFor(ctx context.Context, u *store.User) []MenuGroup {
	if u == nil {
		// The login page and the signed-out 403 have no menu, and no
		// business running a query.
		return nil
	}
	groups := menuFor(u)
	n, err := m.store.CountUnmoderated(ctx)
	if err != nil {
		slog.Error("admin: count unmoderated comments", "error", err)
		return groups
	}
	for gi, g := range groups {
		for li, l := range g.Links {
			if l.Href == moderateHref {
				groups[gi].Links[li].Label = l.Label + " (" + strconv.Itoa(n) + ")"
			}
		}
	}
	return groups
}

// menuFor is the role filter on its own: a link whose role the user
// lacks is left out, and an emptied group with it. The result is a fresh
// slice each time, so the caller may relabel a link.
func menuFor(u *store.User) []MenuGroup {
	if u == nil {
		return nil
	}
	var out []MenuGroup
	for _, g := range menuGroups {
		var links []MenuLink
		for _, l := range g.Links {
			if l.Role != "" && !auth.HasRole(u, l.Role) {
				continue
			}
			links = append(links, l)
		}
		if len(links) == 0 {
			continue
		}
		out = append(out, MenuGroup{Label: g.Label, Links: links})
	}
	return out
}
