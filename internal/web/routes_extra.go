package web

import "net/http"

// routesExtra registers the rest of the public map: search (/search),
// the contact form (/contact), email-this-entry (/send/{id}), the
// slideshow (/slideshow/{name}) and enclosure downloads
// (/download/{id}/{file}). The extras package of M2/M3 fills it in;
// Routes already calls it so nothing else moves then.
func (m *Module) routesExtra(mux *http.ServeMux) {}
