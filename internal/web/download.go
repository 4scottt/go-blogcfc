package web

import (
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/4scottt/go-blogcfc/internal/store"
)

// handleDownload is `GET /download/{id}/{file}?online=` (PLAN §9 P24):
// BlogCFC's download.cfm, which logged the fetch and then redirected to
// the file itself. The entry has to exist and the file has to be its
// enclosure; the entry need not be live, which is download.cfm's own
// `getEntry(id, true, 0)` - an enclosure stays fetchable while its entry
// is being edited.
func (m *Module) handleDownload(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(r.PathValue("id"))
	file := strings.TrimSpace(r.PathValue("file"))
	e, err := m.store.GetEntry(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		m.notFound(w, r)
		return
	}
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	name := enclosureName(e.Enclosure)
	if name == "" || name != file {
		m.notFound(w, r)
		return
	}

	// `online=1` is download.cfm's play-it-here flag: the same file, but
	// the report counts it apart from a real download.
	online := false
	switch strings.ToLower(strings.TrimSpace(r.URL.Query().Get("online"))) {
	case "1", "true", "yes":
		online = true
	}

	if err := m.store.LogDownload(r.Context(), &store.Download{
		EntryID:   e.ID,
		IP:        requestIP(r),
		Referrer:  r.Referer(),
		UserAgent: r.UserAgent(),
		Enclosure: name,
		Online:    online,
	}); err != nil {
		// A fetch that was not counted is still a fetch: the visitor gets
		// the file and the miss goes to the log.
		slog.Error("web: log download", "entry", e.ID, "error", err)
	}

	http.Redirect(w, r, m.base()+"/enclosures/"+url.PathEscape(name), http.StatusFound)
}

// enclosureName is the file name of a stored enclosure, which BlogCFC
// kept as a path and served by its last segment.
func enclosureName(enclosure string) string {
	name := path.Base(filepath.ToSlash(strings.TrimSpace(enclosure)))
	if enclosure == "" || name == "." || name == "/" {
		return ""
	}
	return name
}

// handleEnclosureFile is `GET /enclosures/{file}`: the enclosure tree
// under DATA_DIR (PLAN §6, §8).
func (m *Module) handleEnclosureFile(w http.ResponseWriter, r *http.Request) {
	m.serveDataFile(w, r, filepath.Join(m.cfg.DataDir, "enclosures"), r.PathValue("file"))
}

// handleUploadFile is `GET /images/uploads/{path...}`: the image tree
// under DATA_DIR, which is also where a slideshow's pictures live
// (DATA_DIR/images/slideshows/{name}/…).
func (m *Module) handleUploadFile(w http.ResponseWriter, r *http.Request) {
	m.serveDataFile(w, r, filepath.Join(m.cfg.DataDir, "images"), r.PathValue("path"))
}

// serveDataFile serves one file from a tree under DATA_DIR. The request
// path is refused outright if it is not a plain relative path - a `..`
// element, a leading slash, a backslash or an empty element is a 400 and
// never a file - and the open then goes through os.Root, which cannot
// follow a symlink out of the tree either. A directory is a 404: these
// trees never list themselves.
func (m *Module) serveDataFile(w http.ResponseWriter, r *http.Request, root, rel string) {
	rel, ok := cleanDataPath(rel)
	if !ok {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	dir, err := os.OpenRoot(root)
	if err != nil {
		m.notFound(w, r)
		return
	}
	defer dir.Close()

	f, err := dir.Open(filepath.FromSlash(rel))
	if err != nil {
		m.notFound(w, r)
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil || info.IsDir() {
		m.notFound(w, r)
		return
	}
	http.ServeContent(w, r, path.Base(rel), info.ModTime(), f)
}

// cleanDataPath checks a path from the URL before it reaches the file
// system. Everything the mux hands over is already unescaped, so `..`
// arrives as `..` whether it was written that way or as `%2e%2e`.
func cleanDataPath(rel string) (string, bool) {
	rel = strings.TrimSpace(rel)
	if rel == "" || strings.ContainsAny(rel, "\\\x00") || strings.HasPrefix(rel, "/") {
		return "", false
	}
	if filepath.IsAbs(rel) || filepath.VolumeName(rel) != "" {
		return "", false
	}
	for _, part := range strings.Split(rel, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return rel, true
}
