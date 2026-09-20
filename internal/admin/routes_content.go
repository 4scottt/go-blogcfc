package admin

import (
	"net/http"

	"github.com/4scottt/go-blogcfc/internal/auth"
)

// routesContent registers the content screens: pages, textblocks, the
// pod manager, slideshows, the file manager and the two image popups
// (PLAN §8 "Admin", §9 A21-A26).
//
// Pages sit behind PageAdmin, as isBlogAuthorized had them (A19); the
// rest need a session, which the mux's catch-all already insists on.
// Every write is its own POST with its own button, so the Pilot never
// has to guess which control a form obeys (PLAN §8 "Form rules").
func (m *Module) routesContent(mux *http.ServeMux) {
	pageAdmin := func(h http.HandlerFunc) http.Handler {
		return m.sessions.RequireRole(auth.RolePageAdmin, h)
	}
	mux.Handle("GET /admin/pages", pageAdmin(m.pagesList))
	mux.Handle("GET /admin/pages/new", pageAdmin(m.pageForm))
	mux.Handle("POST /admin/pages/new", pageAdmin(m.pageSave))
	mux.Handle("GET /admin/pages/{id}", pageAdmin(m.pageForm))
	mux.Handle("POST /admin/pages/{id}", pageAdmin(m.pageSave))
	mux.Handle("POST /admin/pages/{id}/delete", pageAdmin(m.pageDelete))

	login := func(h http.HandlerFunc) http.Handler { return m.sessions.RequireLogin(h) }

	mux.Handle("GET /admin/textblocks", login(m.textblocksList))
	mux.Handle("GET /admin/textblocks/new", login(m.textblockForm))
	mux.Handle("POST /admin/textblocks/new", login(m.textblockSave))
	mux.Handle("GET /admin/textblocks/{id}", login(m.textblockForm))
	mux.Handle("POST /admin/textblocks/{id}", login(m.textblockSave))
	mux.Handle("POST /admin/textblocks/{id}/delete", login(m.textblockDelete))

	// The pod manager is one form: show and order for the ten pods.
	mux.Handle("GET /admin/pods", login(m.podsForm))
	mux.Handle("POST /admin/pods", login(m.podsSave))

	mux.Handle("GET /admin/slideshows", login(m.slideshowsList))
	mux.Handle("POST /admin/slideshows", login(m.slideshowCreate))
	mux.Handle("GET /admin/slideshows/{name}", login(m.slideshowShow))
	mux.Handle("POST /admin/slideshows/{name}/rename", login(m.slideshowRename))
	mux.Handle("POST /admin/slideshows/{name}/upload", login(m.slideshowUpload))
	mux.Handle("POST /admin/slideshows/{name}/images/delete", login(m.slideshowImageDelete))
	mux.Handle("POST /admin/slideshows/{name}/delete", login(m.slideshowDelete))

	mux.Handle("GET /admin/files", login(m.filesList))
	mux.Handle("GET /admin/files/download", login(m.filesDownload))
	mux.Handle("POST /admin/files/upload", login(m.filesUpload))
	mux.Handle("POST /admin/files/delete", login(m.filesDelete))

	// The popups the entry editor opens (PLAN §12): whole documents with
	// `body#popUpFormBody`, which also work as full pages.
	mux.Handle("GET /admin/images/upload", login(m.imageUploadForm))
	mux.Handle("POST /admin/images/upload", login(m.imageUploadSave))
	mux.Handle("GET /admin/images/browse", login(m.imageBrowse))
}
