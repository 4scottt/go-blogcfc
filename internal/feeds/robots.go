package feeds

import (
	"net/http"
	"net/url"
)

// handleRobots answers the as-is robots.txt minus its `/mobile/`
// disallow, which has nothing to disallow any more, plus the Sitemap line
// BlogCFC never had (PLAN §8).
func (m *Sitemap) handleRobots(w http.ResponseWriter, r *http.Request) {
	body := "User-agent: *\n" +
		"Disallow:\n" +
		"\n" +
		"Sitemap: " + m.base() + "/sitemap.xml\n"
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	_, _ = w.Write([]byte(body))
}

// pathEscape escapes one path segment, the same way the permalink
// builder in internal/web does.
func pathEscape(segment string) string { return url.PathEscape(segment) }
