package admin

import (
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

// menuFor returns the menu as this user may see it: a link whose role the
// user lacks is left out, and an emptied group with it.
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
