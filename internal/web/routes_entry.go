package web

import "net/http"

// routesEntry registers what completes the entry page: the static pages
// (/page/{alias}), the print view (/print/{id}), the view counter and the
// comments a visitor reads on an entry. The entry-completion package of
// M2/M3 fills it in; Routes already calls it so nothing else moves then.
func (m *Module) routesEntry(mux *http.ServeMux) {}
