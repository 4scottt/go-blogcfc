package admin

import (
	"errors"
	"fmt"
	"html/template"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
)

// maxUploadBytes caps a single upload. BlogCFC left the limit to the
// server's; the rewrite says it, so a truncated body is an error the
// operator sees rather than a half-written file.
const maxUploadBytes = 32 << 20

// uploadFormMemory is how much of a multipart body is parsed in memory
// before the rest spills to a temporary file.
const uploadFormMemory = 8 << 20

// imageExts are the pictures the popups accept, by the type sniffed
// from the bytes rather than the name the browser sent: imgwin.cfm
// trusted `cffile.serverFileExt`, so a .gif that was not one got in
// (PLAN §9 A26).
var imageExts = map[string]string{
	"image/gif":  ".gif",
	"image/jpeg": ".jpg",
	"image/png":  ".png",
}

// popupTemplates are the two windows the entry editor opens. They are
// whole documents with `body#popUpFormBody` (PLAN §12), not pages of the
// admin frame, so they are parsed on their own rather than through
// layout.html.
var popupTemplates = template.Must(template.ParseFS(templateFS,
	"templates/imgwin.html", "templates/imgbrowse.html"))

// imgwinPage is imgwin.html's data: the upload form, what went wrong, and
// - after a good upload - the tag to insert and where the picture landed.
type imgwinPage struct {
	Error string
	Name  string
	URL   string
	Tag   string
}

// imgbrowsePage is imgbrowse.html's data.
type imgbrowsePage struct {
	Images []browseImage
	Error  string
}

// browseImage is one picture in the browser.
type browseImage struct {
	Name string
	URL  string
	Tag  string
	Size int64
}

// imagesDir is where an uploaded picture goes: DATA_DIR/images, the tree
// /images/uploads serves (PLAN §6, §8). The `imageroot` setting is kept
// for the settings page but no longer picks a directory: the tree is the
// platform's, not the webroot's.
func (m *Module) imagesDir() string { return filepath.Join(m.cfg.DataDir, "images") }

// imageUploadForm is GET /admin/images/upload (PLAN §9 A26).
func (m *Module) imageUploadForm(w http.ResponseWriter, r *http.Request) {
	renderPopup(w, http.StatusOK, "imgwin.html", imgwinPage{})
}

// imageUploadSave is POST /admin/images/upload: the picture is saved
// under a unique name and the window hands an `<img>` tag back to the
// editor that opened it.
func (m *Module) imageUploadSave(w http.ResponseWriter, r *http.Request) {
	name, err := m.saveUploadedImage(r, "image", m.imagesDir(), "")
	if err != nil {
		status := http.StatusBadRequest
		if errors.Is(err, errUploadFailed) {
			status = http.StatusInternalServerError
			logError(r, err)
		}
		renderPopup(w, status, "imgwin.html", imgwinPage{Error: uploadMessage(err)})
		return
	}
	m.flush()
	u := m.imageURL(name)
	renderPopup(w, http.StatusOK, "imgwin.html", imgwinPage{Name: name, URL: u, Tag: imgTag(u)})
}

// imageBrowse is GET /admin/images/browse: the pictures already in
// DATA_DIR/images, each with an Insert link that does what a fresh
// upload does (A26).
func (m *Module) imageBrowse(w http.ResponseWriter, r *http.Request) {
	dir := m.imagesDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		logError(r, err)
		renderPopup(w, http.StatusInternalServerError, "imgbrowse.html",
			imgbrowsePage{Error: "The image directory could not be opened."})
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		logError(r, err)
		renderPopup(w, http.StatusInternalServerError, "imgbrowse.html",
			imgbrowsePage{Error: "The image directory could not be read."})
		return
	}
	var images []browseImage
	for _, e := range entries {
		if e.IsDir() || !isImageName(e.Name()) {
			continue
		}
		u := m.imageURL(e.Name())
		img := browseImage{Name: e.Name(), URL: u, Tag: imgTag(u)}
		if info, err := e.Info(); err == nil {
			img.Size = info.Size()
		}
		images = append(images, img)
	}
	sort.Slice(images, func(i, j int) bool { return images[i].Name < images[j].Name })
	renderPopup(w, http.StatusOK, "imgbrowse.html", imgbrowsePage{Images: images})
}

// imageURL is the public URL of a picture in DATA_DIR/images.
func (m *Module) imageURL(name string) string {
	return m.base() + "/images/uploads/" + url.PathEscape(name)
}

// imgTag is what the popup inserts into the body textarea, and shows as
// text so the page is useful full-screen as well.
func imgTag(u string) string { return `<img src="` + u + `" />` }

// isImageName reports whether a file name looks like one of the three
// picture types; the browser lists by name, the uploader sniffs bytes.
func isImageName(name string) bool {
	switch strings.ToLower(path.Ext(name)) {
	case ".gif", ".jpg", ".jpeg", ".png":
		return true
	}
	return false
}

// renderPopup writes one of the popup documents.
func renderPopup(w http.ResponseWriter, status int, name string, data any) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	if err := popupTemplates.ExecuteTemplate(w, name, data); err != nil {
		slog.Error("admin: popup render failed", "template", name, "error", err)
	}
}

// Upload errors. They are values rather than strings so a handler can
// tell a refused upload (the operator's problem) from a failed one (the
// blog's), and so every screen words them the same way.
var (
	errNoUpload     = errors.New("admin: no file was chosen")
	errNotAnImage   = errors.New("admin: the upload is not a gif, jpeg or png")
	errUploadFailed = errors.New("admin: the upload could not be saved")
	errUploadTooBig = errors.New("admin: the upload is too large")
)

// uploadMessage is what an upload error says on screen.
func uploadMessage(err error) string {
	switch {
	case errors.Is(err, errNoUpload):
		return "Choose a file first."
	case errors.Is(err, errNotAnImage):
		return "That file is not a GIF, JPEG or PNG."
	case errors.Is(err, errUploadTooBig):
		return "That file is larger than this blog accepts."
	default:
		return "The file could not be saved. The log has the detail."
	}
}

// saveUploadedImage takes the named file field, checks that its bytes
// really are a picture, and writes it into dir under a unique name,
// which it returns. `want` limits the accepted types to one sniffed
// content type; empty accepts all three.
func (m *Module) saveUploadedImage(r *http.Request, field, dir, want string) (string, error) {
	f, header, err := openUpload(r, field)
	if err != nil {
		return "", err
	}
	defer f.Close()

	head := make([]byte, 512)
	n, err := io.ReadFull(f, head)
	if err != nil && !errors.Is(err, io.EOF) && !errors.Is(err, io.ErrUnexpectedEOF) {
		return "", fmt.Errorf("%w: %v", errUploadFailed, err)
	}
	head = head[:n]
	ctype := http.DetectContentType(head)
	ext, ok := imageExts[ctype]
	if !ok || (want != "" && ctype != want) {
		return "", errNotAnImage
	}
	return writeUpload(dir, header.Filename, ext, head, f)
}

// openUpload is the multipart plumbing every uploading screen repeats:
// a body cap, the parse, and the one file field.
func openUpload(r *http.Request, field string) (multipart.File, *multipart.FileHeader, error) {
	r.Body = http.MaxBytesReader(nil, r.Body, maxUploadBytes)
	if err := r.ParseMultipartForm(uploadFormMemory); err != nil {
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return nil, nil, errUploadTooBig
		}
		return nil, nil, fmt.Errorf("%w: %v", errUploadFailed, err)
	}
	f, header, err := r.FormFile(field)
	if err != nil {
		return nil, nil, errNoUpload
	}
	if header.Filename == "" {
		f.Close()
		return nil, nil, errNoUpload
	}
	return f, header, nil
}

// writeUpload copies head+rest into dir under a safe, unique name.
func writeUpload(dir, filename, ext string, head []byte, rest io.Reader) (string, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("%w: %v", errUploadFailed, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errUploadFailed, err)
	}
	defer root.Close()

	name, err := uniqueName(root, safeFileName(filename, ext))
	if err != nil {
		return "", err
	}
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", fmt.Errorf("%w: %v", errUploadFailed, err)
	}
	defer out.Close()
	if _, err := out.Write(head); err != nil {
		return "", fmt.Errorf("%w: %v", errUploadFailed, err)
	}
	if _, err := io.Copy(out, rest); err != nil {
		_ = root.Remove(name)
		var tooBig *http.MaxBytesError
		if errors.As(err, &tooBig) {
			return "", errUploadTooBig
		}
		return "", fmt.Errorf("%w: %v", errUploadFailed, err)
	}
	return name, nil
}

// safeFileName reduces whatever the browser sent to one path element of
// letters, digits, dots, hyphens and underscores. `ext`, when given, is
// the extension the sniffed type calls for and replaces whatever the
// name claimed: a PNG named `photo.gif` is saved as `photo.png`.
func safeFileName(filename, ext string) string {
	base := path.Base(filepath.ToSlash(strings.TrimSpace(filename)))
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= '0' && r <= '9', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z',
			r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('-')
		}
	}
	name := strings.Trim(b.String(), ".-")
	if ext != "" {
		if cur := strings.ToLower(path.Ext(name)); cur != ext && !(ext == ".jpg" && cur == ".jpeg") {
			name = strings.TrimSuffix(name, path.Ext(name)) + ext
		}
	}
	if name == "" || name == ext {
		name = "upload" + ext
	}
	if len(name) > 100 {
		name = name[len(name)-100:]
	}
	return name
}

// uniqueName is `nameconflict="makeunique"`: the name as given when it is
// free, and `name-1.ext`, `name-2.ext` and so on when it is not.
func uniqueName(root *os.Root, name string) (string, error) {
	ext := path.Ext(name)
	stem := strings.TrimSuffix(name, ext)
	for i := 0; i < 1000; i++ {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s-%d%s", stem, i, ext)
		}
		_, err := root.Stat(candidate)
		if errors.Is(err, os.ErrNotExist) {
			return candidate, nil
		}
		if err != nil {
			return "", fmt.Errorf("%w: %v", errUploadFailed, err)
		}
	}
	return "", fmt.Errorf("%w: too many files named %s", errUploadFailed, name)
}
