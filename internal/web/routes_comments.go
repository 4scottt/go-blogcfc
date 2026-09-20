package web

import "net/http"

// routesComments registers the comment pages (PLAN §8, §9 C01-C11):
// addcomment.cfm's form and addsub.cfm's thread subscription, each of
// which renders on GET and takes its own POST. The owner's one-click
// Delete and Approve links need no route of their own: they are query
// forms on `/`, which handleHome answers (C11).
//
// Both pages were popup windows off the entry page in the as-is and
// render full-page just as well, which is why they answer a plain GET
// (PLAN §12 "Popups").
func (m *Module) routesComments(mux *http.ServeMux) {
	mux.HandleFunc("GET /comments/add/{id}", m.handleAddComment)
	mux.HandleFunc("POST /comments/add/{id}", m.handleAddComment)
	mux.HandleFunc("GET /comments/subscribe/{id}", m.handleSubscribeComment)
	mux.HandleFunc("POST /comments/subscribe/{id}", m.handleSubscribeComment)
}
