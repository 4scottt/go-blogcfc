package admin

import (
	"encoding/xml"
	"errors"
	"net/http"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// slideshowName is the only shape a slideshow may have: the name is a
// directory under DATA_DIR and a path segment of /slideshow/{name}, so
// it is checked before anything is opened. The as-is allowed spaces and
// then built paths from the name as it came (admin/slideshow.cfm); this
// is the same pattern internal/web checks on the public side.
var slideshowName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// slideshowMetaFile is the file slideshow.cfc wrote beside the pictures
// and internal/web still reads: the formal name and the captions.
const slideshowMetaFile = "metadata.xml"

// slideshowMeta is metadata.xml. The shape is slideshow.cfc's, so a
// slideshow copied from a BlogCFC install keeps its captions and the
// public page reads what this screen writes (PLAN §9 A24).
type slideshowMeta struct {
	XMLName    xml.Name          `xml:"slideshow"`
	FormalName string            `xml:"formalname"`
	Captions   slideshowCaptions `xml:"captions"`
}

type slideshowCaptions struct {
	Images []slideshowCaption `xml:"image"`
}

type slideshowCaption struct {
	XMLName xml.Name `xml:"image"`
	Name    string   `xml:"name,attr"`
	Caption string   `xml:"caption,attr"`
}

// slideshowsPage is slideshows.html's own data.
type slideshowsPage struct {
	Rows   []slideshowRow
	Errors []string
	Name   string
}

// slideshowRow is one slideshow in the list.
type slideshowRow struct {
	Name       string
	FormalName string
	Pictures   int
	ViewURL    string
}

// slideshowPage is slideshow.html's own data.
type slideshowPage struct {
	Name       string
	FormalName string
	ViewURL    string
	Images     []slideshowImage
	Errors     []string
}

// slideshowImage is one picture of a slideshow.
type slideshowImage struct {
	Name    string
	URL     string
	Caption string
}

// slideshowsDir is DATA_DIR/images/slideshows, the tree the public
// /slideshow/{name} reads (PLAN §6; internal/web/slideshow.go).
func (m *Module) slideshowsDir() string {
	return filepath.Join(m.cfg.DataDir, "images", "slideshows")
}

// slideshowsList is GET /admin/slideshows (PLAN §9 A24).
func (m *Module) slideshowsList(w http.ResponseWriter, r *http.Request) {
	m.renderSlideshows(w, r, http.StatusOK, nil, "")
}

// renderSlideshows draws the list, with whatever the Add form was
// refused for.
func (m *Module) renderSlideshows(w http.ResponseWriter, r *http.Request, status int, errs []string, name string) {
	dir := m.slideshowsDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.serverError(w, r, err)
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	var rows []slideshowRow
	for _, e := range entries {
		if !e.IsDir() || !slideshowName.MatchString(e.Name()) {
			continue
		}
		images, _ := slideshowPictures(filepath.Join(dir, e.Name()))
		meta := readSlideshowMeta(filepath.Join(dir, e.Name()))
		rows = append(rows, slideshowRow{
			Name:       e.Name(),
			FormalName: meta.FormalName,
			Pictures:   len(images),
			ViewURL:    m.base() + "/slideshow/" + e.Name(),
		})
	}
	sort.Slice(rows, func(i, j int) bool { return rows[i].Name < rows[j].Name })

	data := m.newPageData(r, "Slideshows")
	data.Flash = slideshowFlash(r)
	data.Page = slideshowsPage{Rows: rows, Errors: errs, Name: name}
	renderStatus(w, status, "slideshows.html", data)
}

// slideshowFlash is the banner a redirect asks for.
func slideshowFlash(r *http.Request) string {
	q := r.URL.Query()
	switch {
	case q.Has("created"):
		return "Slideshow created."
	case q.Has("renamed"):
		return "Slideshow saved."
	case q.Has("uploaded"):
		return "Picture uploaded" + fileSuffix(q.Get("uploaded")) + "."
	case q.Has("deleted"):
		return "Deleted."
	default:
		return ""
	}
}

// fileSuffix names the file a flash is about, when there is one.
func fileSuffix(name string) string {
	if name = strings.TrimSpace(name); name != "" {
		return " as " + name
	}
	return ""
}

// slideshowCreate is POST /admin/slideshows: the `Add Slideshow` form.
func (m *Module) slideshowCreate(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	name := strings.TrimSpace(r.PostFormValue("name"))
	if !slideshowName.MatchString(name) {
		m.renderSlideshows(w, r, http.StatusOK,
			[]string{"A slideshow's name may only contain letters, numbers, hyphens and underscores."}, name)
		return
	}
	dir := filepath.Join(m.slideshowsDir(), name)
	if _, err := os.Stat(dir); err == nil {
		m.renderSlideshows(w, r, http.StatusOK, []string{"A slideshow called " + name + " already exists."}, name)
		return
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.flush()
	http.Redirect(w, r, "/admin/slideshows/"+name+"?created=1", http.StatusFound)
}

// slideshowShow is GET /admin/slideshows/{name}.
func (m *Module) slideshowShow(w http.ResponseWriter, r *http.Request) {
	name, ok := m.slideshowPath(w, r)
	if !ok {
		return
	}
	m.renderSlideshow(w, r, http.StatusOK, name, nil)
}

// renderSlideshow draws one slideshow's page.
func (m *Module) renderSlideshow(w http.ResponseWriter, r *http.Request, status int, name string, errs []string) {
	dir := filepath.Join(m.slideshowsDir(), name)
	pictures, err := slideshowPictures(dir)
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	meta := readSlideshowMeta(dir)
	captions := make(map[string]string, len(meta.Captions.Images))
	for _, c := range meta.Captions.Images {
		captions[c.Name] = c.Caption
	}
	p := slideshowPage{
		Name:       name,
		FormalName: meta.FormalName,
		ViewURL:    m.base() + "/slideshow/" + name,
	}
	for _, f := range pictures {
		p.Images = append(p.Images, slideshowImage{
			Name:    f,
			URL:     m.base() + "/images/uploads/slideshows/" + name + "/" + f,
			Caption: captions[f],
		})
	}
	p.Errors = errs
	data := m.newPageData(r, "Slideshow Editor")
	data.Flash = slideshowFlash(r)
	data.Page = p
	renderStatus(w, status, "slideshow.html", data)
}

// slideshowRename is POST /admin/slideshows/{name}/rename: the name and
// the formal name, which is the one thing metadata.xml carries besides
// the captions.
func (m *Module) slideshowRename(w http.ResponseWriter, r *http.Request) {
	name, ok := m.slideshowPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	newname := strings.TrimSpace(r.PostFormValue("newname"))
	formal := strings.TrimSpace(r.PostFormValue("formalname"))
	if newname == "" {
		newname = name
	}
	if !slideshowName.MatchString(newname) {
		m.renderSlideshow(w, r, http.StatusOK, name,
			[]string{"A slideshow's name may only contain letters, numbers, hyphens and underscores."})
		return
	}

	root, err := os.OpenRoot(m.slideshowsDir())
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	defer root.Close()

	if newname != name {
		if _, err := root.Stat(newname); err == nil {
			m.renderSlideshow(w, r, http.StatusOK, name,
				[]string{"A slideshow called " + newname + " already exists."})
			return
		}
		if err := root.Rename(name, newname); err != nil {
			m.serverError(w, r, err)
			return
		}
	}

	dir := filepath.Join(m.slideshowsDir(), newname)
	meta := readSlideshowMeta(dir)
	meta.FormalName = formal
	if err := writeSlideshowMeta(dir, meta); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.flush()
	http.Redirect(w, r, "/admin/slideshows/"+newname+"?renamed=1", http.StatusFound)
}

// slideshowUpload is POST /admin/slideshows/{name}/upload: one picture,
// accepted on its sniffed type and saved under a unique name.
func (m *Module) slideshowUpload(w http.ResponseWriter, r *http.Request) {
	name, ok := m.slideshowPath(w, r)
	if !ok {
		return
	}
	saved, err := m.saveUploadedImage(r, "image", filepath.Join(m.slideshowsDir(), name), "")
	if err != nil {
		if errors.Is(err, errUploadFailed) {
			logError(r, err)
		}
		m.renderSlideshow(w, r, http.StatusOK, name, []string{uploadMessage(err)})
		return
	}
	m.flush()
	http.Redirect(w, r, "/admin/slideshows/"+name+"?uploaded="+saved, http.StatusFound)
}

// slideshowImageDelete is POST /admin/slideshows/{name}/images/delete:
// one picture and its caption.
func (m *Module) slideshowImageDelete(w http.ResponseWriter, r *http.Request) {
	name, ok := m.slideshowPath(w, r)
	if !ok {
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	image := strings.TrimSpace(r.PostFormValue("image"))
	if !plainFileName(image) {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	dir := filepath.Join(m.slideshowsDir(), name)
	root, err := os.OpenRoot(dir)
	if err != nil {
		m.notFound(w, r)
		return
	}
	defer root.Close()
	if err := root.Remove(image); err != nil && !errors.Is(err, os.ErrNotExist) {
		m.serverError(w, r, err)
		return
	}

	// The caption goes with the picture, so metadata.xml does not keep
	// growing a list of files that are gone.
	meta := readSlideshowMeta(dir)
	kept := meta.Captions.Images[:0]
	for _, c := range meta.Captions.Images {
		if c.Name != image {
			kept = append(kept, c)
		}
	}
	meta.Captions.Images = kept
	if err := writeSlideshowMeta(dir, meta); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.flush()
	http.Redirect(w, r, "/admin/slideshows/"+name+"?deleted=1", http.StatusFound)
}

// slideshowDelete is POST /admin/slideshows/{name}/delete: the whole
// directory, as the as-is list's `mark` delete did.
func (m *Module) slideshowDelete(w http.ResponseWriter, r *http.Request) {
	name, ok := m.slideshowPath(w, r)
	if !ok {
		return
	}
	root, err := os.OpenRoot(m.slideshowsDir())
	if err != nil {
		m.serverError(w, r, err)
		return
	}
	defer root.Close()
	if err := root.RemoveAll(name); err != nil {
		m.serverError(w, r, err)
		return
	}
	m.flush()
	http.Redirect(w, r, "/admin/slideshows?deleted=1", http.StatusFound)
}

// slideshowPath reads {name} and checks that the slideshow is there. A
// name that is not a plain directory name is a 404, never a path.
func (m *Module) slideshowPath(w http.ResponseWriter, r *http.Request) (string, bool) {
	name := strings.TrimSpace(r.PathValue("name"))
	if !slideshowName.MatchString(name) {
		m.notFound(w, r)
		return "", false
	}
	info, err := os.Stat(filepath.Join(m.slideshowsDir(), name))
	if err != nil || !info.IsDir() {
		m.notFound(w, r)
		return "", false
	}
	return name, true
}

// slideshowPictures lists a slideshow's pictures by name.
func slideshowPictures(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !isImageName(e.Name()) {
			continue
		}
		out = append(out, e.Name())
	}
	sort.Strings(out)
	return out, nil
}

// readSlideshowMeta reads metadata.xml. A missing or broken file is a
// slideshow without a formal name and without captions, which is what
// slideshow.cfc's getInfo did.
func readSlideshowMeta(dir string) slideshowMeta {
	var meta slideshowMeta
	raw, err := os.ReadFile(filepath.Join(dir, slideshowMetaFile))
	if err != nil {
		return slideshowMeta{}
	}
	if err := xml.Unmarshal(raw, &meta); err != nil {
		return slideshowMeta{}
	}
	return meta
}

// writeSlideshowMeta writes metadata.xml in the shape slideshow.cfc
// wrote and internal/web reads.
func writeSlideshowMeta(dir string, meta slideshowMeta) error {
	meta.XMLName = xml.Name{Local: "slideshow"}
	body, err := xml.MarshalIndent(meta, "", "\t")
	if err != nil {
		return err
	}
	out := append([]byte(xml.Header), body...)
	out = append(out, '\n')
	return os.WriteFile(filepath.Join(dir, slideshowMetaFile), out, 0o644)
}

// plainFileName reports whether s is one file name and nothing else: no
// separators, no `..`, no leading dot.
func plainFileName(s string) bool {
	if s == "" || s == "." || s == ".." || strings.HasPrefix(s, ".") {
		return false
	}
	if strings.ContainsAny(s, "/\\\x00") {
		return false
	}
	return path.Base(s) == s
}
