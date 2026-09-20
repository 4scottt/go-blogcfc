package web

import "net/http"

// routesSubscriptions registers the two links a subscription mail
// carries: the double opt-in confirmation (/confirmsubscription?t=) and
// the unsubscribe, in both its thread and its blog form
// (/unsubscribe?email=&commentID= | &token=) -- BlogCFC's
// confirmsubscription.cfm and unsubscribe.cfm (PLAN §8, §9 C13, C14).
//
// Both are GET only: they are links in an email, and both were GET in the
// as-is.
func (m *Module) routesSubscriptions(mux *http.ServeMux) {
	mux.HandleFunc("GET /confirmsubscription", m.handleConfirmSubscription)
	mux.HandleFunc("GET /unsubscribe", m.handleUnsubscribe)
}
