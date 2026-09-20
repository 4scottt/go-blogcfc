package admin

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// subscribersPage is subscribers.html's own data.
type subscribersPage struct {
	Rows     []store.Subscriber
	Total    int
	Verified int
}

// subscribersList is GET /admin/subscribers: who is on the list, how many
// of them are verified, and the buttons that change that (PLAN §9 A15,
// admin/subscribers.cfm).
func (m *Module) subscribersList(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	subs, err := m.store.ListSubscribers(ctx)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	total, verified, err := m.store.CountSubscribers(ctx)
	if err != nil {
		m.serverError(w, r, err)
		return
	}

	data := m.newPageData(r, "Subscribers")
	data.Flash = subscribersFlash(r.URL.Query())
	data.Page = subscribersPage{Rows: subs, Total: total, Verified: verified}
	render(w, "subscribers.html", data)
}

// subscribersFlash is the banner a redirect asks for.
func subscribersFlash(q url.Values) string {
	switch {
	case q.Has("verified"):
		return "Subscriber verified."
	case q.Has("deleted"):
		return "Subscriber removed."
	}
	if n, err := strconv.Atoi(q.Get("removed")); err == nil {
		if n == 1 {
			return "1 unverified subscriber removed."
		}
		return strconv.Itoa(n) + " unverified subscribers removed."
	}
	return ""
}

// subscribersAction is POST /admin/subscribers: the screen's four buttons
// on one handler, as the as-is page had them, each its own form with its
// own named submit (PLAN §8 "Form rules").
//
// The as-is verified through a `subscribers.cfm?verify=` link, which any
// crawler following links in the admin could fire; here every change is a
// POST.
func (m *Module) subscribersAction(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	email := strings.TrimSpace(r.PostFormValue("email"))

	switch {
	case r.PostForm.Has("nukeunverified"):
		n, err := m.store.DeleteUnverified(ctx)
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/admin/subscribers?removed="+strconv.Itoa(n), http.StatusFound)

	case r.PostForm.Has("verify"):
		err := m.store.VerifySubscriber(ctx, email)
		if err != nil && !errors.Is(err, store.ErrNotFound) {
			m.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/admin/subscribers?verified=1", http.StatusFound)

	case r.PostForm.Has("delete"):
		if err := m.store.DeleteSubscriber(ctx, email); err != nil {
			m.serverError(w, r, err)
			return
		}
		http.Redirect(w, r, "/admin/subscribers?deleted=1", http.StatusFound)

	default:
		http.Redirect(w, r, "/admin/subscribers", http.StatusFound)
	}
}
