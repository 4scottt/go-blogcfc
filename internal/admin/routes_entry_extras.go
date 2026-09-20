package admin

import "net/http"

// routesEntryExtras registers the screens that hang off the entry
// editor: the related-entries JSON the picker fetches (A09), the
// downloads report (A27) and the stats screen (A28). The enclosure
// upload, the preview and the crash-recovery draft (A07, A10, A11) are
// the editor's own POST and need no route of their own.
//
// All three need a session and nothing more, as the as-is admin did:
// downloads.cfm, stats.cfm and proxy.cfm sat behind the admin login
// with no role check.
func (m *Module) routesEntryExtras(mux *http.ServeMux) {
	mux.Handle("GET "+adminProxyPath, m.sessions.RequireLogin(http.HandlerFunc(m.entryProxy)))
	mux.Handle("GET /admin/downloads", m.sessions.RequireLogin(http.HandlerFunc(m.downloadsReport)))
	mux.Handle("GET /admin/stats", m.sessions.RequireLogin(http.HandlerFunc(m.statsReport)))
	mux.Handle("GET /admin/stats/{year}", m.sessions.RequireLogin(http.HandlerFunc(m.statsReport)))
}
