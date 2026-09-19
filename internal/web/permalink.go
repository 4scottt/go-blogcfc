package web

import (
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// EntryURL is BlogCFC's makeLink: an entry with an alias gets the SES
// permalink `{base}/{year}/{month}/{day}/{alias}` with the date read in
// the blog's zone and no zero padding, and one without an alias falls back
// to `{base}/?mode=entry&entry={id}` (PLAN §8, §9 P05).
//
// The base is always cfg.BlogBaseURL: no URL on a page, in a feed or in a
// mail is ever derived from the request's Host (PLAN §6, FP O05).
func EntryURL(base string, e store.Entry, loc *time.Location) string {
	base = strings.TrimRight(base, "/")
	if e.Alias == "" {
		return base + "/?mode=entry&entry=" + url.QueryEscape(e.ID)
	}
	if loc == nil {
		loc = time.UTC
	}
	t := e.Posted.In(loc)
	return base + "/" + strconv.Itoa(t.Year()) + "/" + strconv.Itoa(int(t.Month())) +
		"/" + strconv.Itoa(t.Day()) + "/" + url.PathEscape(e.Alias)
}

// CategoryURL is BlogCFC's makeCategoryLink: the alias when there is one,
// the id form otherwise.
func CategoryURL(base string, c store.Category) string {
	base = strings.TrimRight(base, "/")
	if c.Alias == "" {
		return base + "/?mode=cat&catid=" + url.QueryEscape(c.ID)
	}
	return base + "/" + url.PathEscape(c.Alias)
}

// UserURL is BlogCFC's makeUserLink. BlogCFC linked the author's display
// name with its spaces turned into underscores; the rewrite's route takes
// the username, which the admin keeps free of spaces (PLAN §8).
func UserURL(base, username string) string {
	return strings.TrimRight(base, "/") + "/postedby/" + url.PathEscape(username)
}
