package admin

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// adminProxyPath is where the editor's related-entries filter fetches
// its list (PLAN §8, §9 A09).
const adminProxyPath = "/admin/proxy"

// proxyMaxEntries is admin/proxy.cfm's `maxEntries = 200`, which the
// as-is screen told the author about in so many words.
const proxyMaxEntries = 200

// entryProxy is GET /admin/proxy?text=&category=&entry=: the entries
// matching a filter, as JSON, for the related-entries picker.
//
// The as-is hand-built its JSON with a StringBuffer and left a trailing
// comma in it, so the response was not valid JSON at all and only
// jQuery's forgiving parser ever read it (proxy.cfm; PLAN §8 "valid
// JSON this time"). This writes an array of objects with encoding/json.
// `entry` is the entry being edited and is left out of the result: an
// entry is never related to itself (store.SetRelatedEntries drops it
// too, but the picker should not offer it).
//
// With neither filter the answer is an empty array, as the as-is
// returned nothing at all until one of the two was given.
func (m *Module) entryProxy(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	text := strings.TrimSpace(q.Get("text"))
	category := strings.TrimSpace(q.Get("category"))
	exclude := strings.TrimSpace(q.Get("entry"))

	out := []entryRef{}
	if text != "" || category != "" {
		f := store.EntryFilter{
			Keywords: text,
			Sort:     "posted",
			Desc:     true,
			Limit:    proxyMaxEntries,
		}
		if category != "" {
			f.CategoryIDs = []string{category}
		}
		entries, _, err := m.store.ListEntries(r.Context(), f)
		if err != nil {
			m.serverError(w, r, err)
			return
		}
		for _, e := range entries {
			if e.ID == exclude {
				continue
			}
			out = append(out, entryRef{ID: e.ID, Title: e.Title})
		}
	}

	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	if err := json.NewEncoder(w).Encode(out); err != nil {
		slog.Error("admin: proxy encode", "error", err)
	}
}
