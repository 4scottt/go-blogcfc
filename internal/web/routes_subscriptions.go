package web

import "net/http"

// routesSubscriptions is filled by the M3 subscriptions package: the
// double opt-in confirmation (/confirmsubscription) and /unsubscribe.
func (m *Module) routesSubscriptions(mux *http.ServeMux) {}
