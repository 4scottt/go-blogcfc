package web

import "net/http"

// routesComments is filled by the M3 comments package: the add-comment
// form (/comments/add/{id}), the thread subscription (/comments/subscribe/{id})
// and the one-click kill/approve links.
func (m *Module) routesComments(mux *http.ServeMux) {}
