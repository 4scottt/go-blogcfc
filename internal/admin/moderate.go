package admin

import (
	"errors"
	"net/http"
	"strconv"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// moderatePage is moderate.html's own data.
type moderatePage struct {
	Rows  []commentRow
	Total int
}

// moderateQueue is GET /admin/moderate: every comment still waiting, with
// an Approve link each and the same bulk-delete form the comments list
// has (PLAN §9 A14, admin/moderate.cfm).
//
// `?approve={id}` approves and then redirects to the bare queue, so a
// reload does not approve and re-notify a second time; the as-is rendered
// the queue in place and a refresh sent the mail again.
func (m *Module) moderateQueue(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	if id := strings.TrimSpace(r.URL.Query().Get("approve")); id != "" {
		c, err := m.store.GetComment(ctx, id)
		switch {
		case errors.Is(err, store.ErrNotFound):
			m.notFound(w, r)
			return
		case err != nil:
			m.serverError(w, r, err)
			return
		}
		if err := m.approveAndNotify(ctx, c); err != nil {
			m.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/admin/moderate?approved=1", http.StatusFound)
		return
	}

	rows, err := m.store.ListUnmoderated(ctx)
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	data := m.newPageData(r, "Moderate Comments")
	data.Flash = commentsFlash(r.URL.Query())
	data.Page = moderatePage{Rows: m.commentRows(rows), Total: len(rows)}
	render(w, "moderate.html", data)
}

// moderateDelete is POST /admin/moderate/delete: the marked rows go and
// the queue comes back, rather than the comments list (PLAN §9 A14).
func (m *Module) moderateDelete(w http.ResponseWriter, r *http.Request) {
	n, err := m.deleteMarked(r)
	if err != nil {
		if errors.Is(err, errBadForm) {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		m.serverError(w, r, err)
		return
	}
	http.Redirect(w, r, "/admin/moderate?deleted="+strconv.Itoa(n), http.StatusFound)
}
