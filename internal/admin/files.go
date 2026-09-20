package admin

import (
	"errors"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// dataDirs are the directories an operator expects to find in the file
// manager: the enclosure tree the feeds link to and the image tree the
// popups fill (PLAN §6). They are made on the way in so the screen is
// never an empty list on a fresh volume.
var dataDirs = []string{"enclosures", "images"}

// filesPage is files.html's own data.
type filesPage struct {
	Dir       string
	ParentURL string
	Rows      []fileRow
	Errors    []string
}

// fileRow is one line: a directory that links deeper, or a file with its
// Download and Delete forms.
type fileRow struct {
	Name     string
	IsDir    bool
	Size     int64
	Modified time.Time
	DirURL   string
}

// filesList is GET /admin/files (PLAN §9 A25, admin/filemanager.cfm).
// The as-is browsed the webroot, which is how a file manager becomes a
// way to write a .cfm; this one cannot leave DATA_DIR (PLAN §7).
func (m *Module) filesList(w http.ResponseWriter, r *http.Request) {
	if !m.fileBrowseAllowed(w, r) {
		return
	}
	dir, ok := m.requestDir(w, r, r.URL.Query().Get("dir"))
	if !ok {
		return
	}
	m.renderFiles(w, r, http.StatusOK, dir, nil)
}

// renderFiles draws the listing of one directory inside DATA_DIR.
func (m *Module) renderFiles(w http.ResponseWriter, r *http.Request, status int, dir string, errs []string) {
	root, err := m.dataRoot()
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	defer root.Close()

	name := "."
	if dir != "" {
		name = dir
	}
	f, err := root.Open(name)
	if err != nil {
		m.notFound(w, r)
		return
	}
	defer f.Close()
	entries, err := f.ReadDir(-1)
	if err != nil {
		m.notFound(w, r)
		return
	}

	p := filesPage{Dir: dir, Errors: errs}
	if dir != "" {
		parent := path.Dir(dir)
		if parent == "." {
			parent = ""
		}
		p.ParentURL = "/admin/files?dir=" + url.QueryEscape(parent)
	}
	for _, e := range entries {
		row := fileRow{Name: e.Name(), IsDir: e.IsDir()}
		if row.IsDir {
			row.DirURL = "/admin/files?dir=" + url.QueryEscape(path.Join(dir, e.Name()))
		}
		if info, err := e.Info(); err == nil {
			row.Size, row.Modified = info.Size(), info.ModTime()
		}
		p.Rows = append(p.Rows, row)
	}
	// Directories first, then files, each by name: `sort="type asc"`.
	sort.SliceStable(p.Rows, func(i, j int) bool {
		if p.Rows[i].IsDir != p.Rows[j].IsDir {
			return p.Rows[i].IsDir
		}
		return p.Rows[i].Name < p.Rows[j].Name
	})

	data := m.newPageData(r, "Files")
	data.Flash = filesFlash(r)
	data.Page = p
	renderStatus(w, status, "files.html", data)
}

// filesFlash is the banner a redirect asks for.
func filesFlash(r *http.Request) string {
	q := r.URL.Query()
	switch {
	case q.Has("uploaded"):
		return "File uploaded" + fileSuffix(q.Get("uploaded")) + "."
	case q.Has("deleted"):
		return "File deleted."
	default:
		return ""
	}
}

// filesUpload is POST /admin/files/upload: one file into the directory
// the form came from. A name already taken is not overwritten - the
// as-is did overwrite it, without asking - but saved beside it.
func (m *Module) filesUpload(w http.ResponseWriter, r *http.Request) {
	if !m.fileBrowseAllowed(w, r) {
		return
	}
	// The directory travels with the upload, so ParseMultipartForm has to
	// run before the field is read; openUpload does that.
	f, header, err := openUpload(r, "file")
	if err != nil {
		dir, ok := m.requestDir(w, r, r.FormValue("dir"))
		if !ok {
			return
		}
		if errors.Is(err, errUploadFailed) {
			logError(r, err)
		}
		m.renderFiles(w, r, http.StatusOK, dir, []string{uploadMessage(err)})
		return
	}
	defer f.Close()
	dir, ok := m.requestDir(w, r, r.FormValue("dir"))
	if !ok {
		return
	}

	saved, err := writeUpload(filepath.Join(m.cfg.DataDir, filepath.FromSlash(dir)), header.Filename, "", nil, f)
	if err != nil {
		if errors.Is(err, errUploadFailed) {
			logError(r, err)
		}
		m.renderFiles(w, r, http.StatusOK, dir, []string{uploadMessage(err)})
		return
	}
	m.flush()
	http.Redirect(w, r, "/admin/files?dir="+url.QueryEscape(dir)+"&uploaded="+url.QueryEscape(saved), http.StatusFound)
}

// filesDownload is GET /admin/files/download: the file itself, as an
// attachment.
func (m *Module) filesDownload(w http.ResponseWriter, r *http.Request) {
	if !m.fileBrowseAllowed(w, r) {
		return
	}
	q := r.URL.Query()
	dir, ok := m.requestDir(w, r, q.Get("dir"))
	if !ok {
		return
	}
	name := strings.TrimSpace(q.Get("file"))
	if !plainFileName(name) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	root, err := m.dataRoot()
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	defer root.Close()

	f, err := root.Open(path.Join(dir, name))
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
	w.Header().Set("Content-Disposition", "attachment; filename="+quoteFileName(name))
	w.Header().Set("Content-Type", "application/octet-stream")
	http.ServeContent(w, r, name, info.ModTime(), f)
}

// quoteFileName quotes a file name for a header value.
func quoteFileName(name string) string {
	return `"` + strings.NewReplacer(`"`, "", "\\", "", "\r", "", "\n", "").Replace(name) + `"`
}

// filesDelete is POST /admin/files/delete: one file, never a directory.
func (m *Module) filesDelete(w http.ResponseWriter, r *http.Request) {
	if !m.fileBrowseAllowed(w, r) {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	dir, ok := m.requestDir(w, r, r.PostFormValue("dir"))
	if !ok {
		return
	}
	name := strings.TrimSpace(r.PostFormValue("file"))
	if !plainFileName(name) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	root, err := m.dataRoot()
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	defer root.Close()

	rel := path.Join(dir, name)
	info, err := root.Stat(rel)
	if err != nil {
		m.notFound(w, r)
		return
	}
	if info.IsDir() {
		m.renderFiles(w, r, http.StatusOK, dir, []string{name + " is a directory. The file manager deletes files only."})
		return
	}
	if err := root.Remove(rel); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.flush()
	http.Redirect(w, r, "/admin/files?dir="+url.QueryEscape(dir)+"&deleted=1", http.StatusFound)
}

// dataRoot opens DATA_DIR as a root: every name the file manager uses is
// resolved inside it, so not even a symlink in the tree reaches out.
func (m *Module) dataRoot() (*os.Root, error) {
	if err := os.MkdirAll(m.cfg.DataDir, 0o755); err != nil {
		return nil, err
	}
	for _, d := range dataDirs {
		if err := os.MkdirAll(filepath.Join(m.cfg.DataDir, d), 0o755); err != nil {
			return nil, err
		}
	}
	return os.OpenRoot(m.cfg.DataDir)
}

// requestDir validates the `dir` a request carries. A `..`, an absolute
// path, a backslash or an empty element is a 400 and never a directory:
// the as-is silently rewrote `..` to `/` and went on (filemanager.cfm).
func (m *Module) requestDir(w http.ResponseWriter, r *http.Request, raw string) (string, bool) {
	dir, ok := cleanAdminDir(raw)
	if !ok {
		http.Error(w, "bad request", http.StatusBadRequest)
		return "", false
	}
	return dir, true
}

// cleanAdminDir is that check on its own: it returns the directory
// relative to DATA_DIR, with the root as the empty string.
func cleanAdminDir(raw string) (string, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "/" || raw == "." {
		return "", true
	}
	// A backslash, a null and a percent are all refused: the first two
	// are separators somewhere, and a percent in a directory that has
	// already been unescaped once is a double-encoded `..` and nothing
	// a file in DATA_DIR needs.
	if strings.ContainsAny(raw, "\\\x00%") || strings.HasPrefix(raw, "/") {
		return "", false
	}
	if filepath.IsAbs(raw) || filepath.VolumeName(raw) != "" {
		return "", false
	}
	raw = strings.TrimSuffix(raw, "/")
	for _, part := range strings.Split(raw, "/") {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
	}
	return raw, true
}

// fileBrowseAllowed honours the `filebrowse` setting: with it off the
// file manager is not there at all, which is the as-is behaviour
// (filemanager.cfm sent the operator back to the dashboard).
func (m *Module) fileBrowseAllowed(w http.ResponseWriter, r *http.Request) bool {
	if m.settings.FileBrowse() {
		return true
	}
	data := m.newPageData(r, "Files")
	data.Page = errorPage{Message: "The file manager is switched off. Turn `filebrowse` on in Settings to use it."}
	renderStatus(w, http.StatusForbidden, "error.html", data)
	return false
}
