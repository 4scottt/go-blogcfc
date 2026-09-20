package admin

import (
	"errors"
	"net/http"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// maxTextblockLabel is the label column's width (0001_schema.sql). The
// label is what a body's `<textblock label="...">` names, so it is the
// one field that has to stay unique (PLAN §9 A22).
const maxTextblockLabel = 255

// textblocksPage is textblocks.html's own data.
type textblocksPage struct {
	Rows []textblockRow
}

// textblockRow is one line of the list: the label, and the first of the
// body the as-is datatable showed with `left="50"`.
type textblockRow struct {
	ID      string
	Label   string
	Excerpt string
}

// textblocksList is GET /admin/textblocks (PLAN §9 A22).
func (m *Module) textblocksList(w http.ResponseWriter, r *http.Request) {
	blocks, err := m.store.ListTextblocks(r.Context())
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	rows := make([]textblockRow, 0, len(blocks))
	for _, b := range blocks {
		// `excerpt` is the comment list's cutter (comments.go); the as-is
		// datatable cut the body at 50 too.
		rows = append(rows, textblockRow{ID: b.ID, Label: b.Label, Excerpt: excerpt(b.Body, 50)})
	}
	data := m.newPageData(r, "Textblocks")
	data.Flash = savedDeletedFlash(r, "Textblock")
	data.Page = textblocksPage{Rows: rows}
	render(w, "textblocks.html", data)
}

// textblockFormPage is textblock.html's own data.
type textblockFormPage struct {
	IsNew     bool
	Action    string
	DeleteURL string
	Errors    []string
	Label     string
	Body      string
}

// textblockForm is GET /admin/textblocks/{id} and /admin/textblocks/new.
func (m *Module) textblockForm(w http.ResponseWriter, r *http.Request) {
	id := contentPathID(r)
	p := textblockFormPage{IsNew: id == "", Action: "/admin/textblocks/new"}
	if !p.IsNew {
		b, err := m.textblockByID(r, id)
		if err != nil {
			m.textblockError(w, r, err)
			return
		}
		p.Action = "/admin/textblocks/" + b.ID
		p.DeleteURL = "/admin/textblocks/" + b.ID + "/delete"
		p.Label, p.Body = b.Label, b.Body
	}
	data := m.newPageData(r, "Textblock Editor")
	data.Page = p
	render(w, "textblock.html", data)
}

// textblockByID reads one block. The store has no getter by id - the
// render pass wants them by label - so the list is the lookup, which is
// the same one query the as-is ran.
func (m *Module) textblockByID(r *http.Request, id string) (*store.Textblock, error) {
	blocks, err := m.store.ListTextblocks(r.Context())
	if err != nil {
		return nil, err
	}
	for i := range blocks {
		if blocks[i].ID == id {
			return &blocks[i], nil
		}
	}
	return nil, store.ErrNotFound
}

// textblockError turns a lookup failure into the right page.
func (m *Module) textblockError(w http.ResponseWriter, r *http.Request, err error) {
	if errors.Is(err, store.ErrNotFound) {
		m.notFound(w, r)
		return
	}
	m.serverError(w, r, err)
}

// textblockSave is POST /admin/textblocks/{id} and /admin/textblocks/new.
func (m *Module) textblockSave(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ctx := r.Context()
	id := contentPathID(r)

	var existing *store.Textblock
	if id != "" {
		b, err := m.textblockByID(r, id)
		if err != nil {
			m.textblockError(w, r, err)
			return
		}
		existing = b
	}

	p := textblockFormPage{
		IsNew:  existing == nil,
		Action: "/admin/textblocks/new",
		Label:  strings.TrimSpace(r.PostFormValue("label")),
		Body:   r.PostFormValue("body"),
	}
	if existing != nil {
		p.Action = "/admin/textblocks/" + existing.ID
		p.DeleteURL = "/admin/textblocks/" + existing.ID + "/delete"
	}

	var errs []string
	switch {
	case p.Label == "":
		errs = append(errs, "The label cannot be blank.")
	case len([]rune(p.Label)) > maxTextblockLabel:
		errs = append(errs, "The label may be at most 255 characters.")
	}
	if strings.TrimSpace(p.Body) == "" {
		errs = append(errs, "The body cannot be blank.")
	}

	if len(errs) == 0 {
		b := &store.Textblock{Label: p.Label, Body: p.Body}
		var err error
		if existing != nil {
			b.ID = existing.ID
			err = m.store.UpdateTextblock(ctx, b)
		} else {
			err = m.store.CreateTextblock(ctx, b)
		}
		switch {
		case errors.Is(err, store.ErrDuplicate):
			errs = append(errs, "A textblock with the label "+p.Label+" already exists.")
		case err != nil:
			m.serverError(w, r, err)
			return
		default:
			// A body that quotes this block is cached elsewhere, so the
			// caches go with the write (PLAN §9 A29).
			m.flush()
			m.reinit()
			http.Redirect(w, r, "/admin/textblocks?saved=1", http.StatusFound)
			return
		}
	}

	p.Errors = errs
	data := m.newPageData(r, "Textblock Editor")
	data.Page = p
	render(w, "textblock.html", data)
}

// textblockDelete is POST /admin/textblocks/{id}/delete.
func (m *Module) textblockDelete(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	if id == "" {
		m.notFound(w, r)
		return
	}
	if err := m.store.DeleteTextblock(r.Context(), id); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.flush()
	m.reinit()
	http.Redirect(w, r, "/admin/textblocks?deleted=1", http.StatusFound)
}
