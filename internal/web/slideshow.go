package web

import (
	"encoding/xml"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// slideshowName is the only shape a slideshow directory may have. The
// name reaches the file system, so it is checked before anything is
// opened; BlogCFC took the last path segment as it came.
var slideshowName = regexp.MustCompile(`^[A-Za-z0-9_-]+$`)

// slideshowExts are the pictures a slideshow shows. BlogCFC's
// slideshow.cfc listed `.jpg` and `.gif` only; `.jpeg` and `.png` join
// them, since the admin's uploader accepts both (PLAN §9 P23).
var slideshowExts = map[string]bool{".gif": true, ".jpg": true, ".jpeg": true, ".png": true}

// slideshowMetadata is the `metadata.xml` slideshow.cfc reads from the
// slideshow's own directory: a display name and a caption per file.
type slideshowMetadata struct {
	XMLName    xml.Name `xml:"slideshow"`
	FormalName string   `xml:"formalname"`
	Captions   struct {
		Images []struct {
			Name    string `xml:"name,attr"`
			Caption string `xml:"caption,attr"`
		} `xml:",any"`
	} `xml:"captions"`
}

// slidePage is /slideshow/{name}.
type slidePage struct {
	pageData

	Heading  string
	ImageURL string
	Alt      string
	Caption  string
	Index    int
	Count    int
	PrevURL  string
	NextURL  string
}

// handleSlideshow is `GET /slideshow/{name}?slide=N` (PLAN §9 P23): one
// picture a page, in file-name order, with its caption and prev/next
// links. An unknown slideshow, an empty one and a name that is not a
// plain directory name are all 404s; slideshow.cfm sent each of them to
// the home page instead.
func (m *Module) handleSlideshow(w http.ResponseWriter, r *http.Request) {
	name := strings.TrimSpace(r.PathValue("name"))
	if !slideshowName.MatchString(name) {
		m.notFound(w, r)
		return
	}
	dir := filepath.Join(m.slideshowsDir(), name)
	images, err := slideshowImages(dir)
	if err != nil {
		if !os.IsNotExist(err) {
			slog.Error("web: read slideshow", "name", name, "error", err)
		}
		m.notFound(w, r)
		return
	}
	if len(images) == 0 {
		m.notFound(w, r)
		return
	}

	// The index is clamped into [1, n]: a slide past the end shows the
	// last picture rather than bouncing back to the first, which is what
	// slideshow.cfm did with any out-of-range value.
	slide := 1
	if raw := strings.TrimSpace(r.URL.Query().Get("slide")); raw != "" {
		if n, err := strconv.Atoi(raw); err == nil {
			slide = n
		}
	}
	if slide < 1 {
		slide = 1
	}
	if slide > len(images) {
		slide = len(images)
	}

	meta := readSlideshowMetadata(dir)
	title := "Slideshow"
	if fn := strings.TrimSpace(meta.FormalName); fn != "" {
		title = "Slideshow - " + fn
	}
	file := images[slide-1]

	page := slidePage{
		pageData: m.newPage(r, title),
		Heading:  title + " - Picture " + strconv.Itoa(slide) + " of " + strconv.Itoa(len(images)),
		ImageURL: m.base() + "/images/uploads/slideshows/" + name + "/" + url.PathEscape(file),
		Alt:      file,
		Index:    slide,
		Count:    len(images),
	}
	for _, img := range meta.Captions.Images {
		if img.Name == file {
			page.Caption = img.Caption
			break
		}
	}
	if slide > 1 {
		page.PrevURL = m.base() + "/slideshow/" + name + "?slide=" + strconv.Itoa(slide-1)
	}
	if slide < len(images) {
		page.NextURL = m.base() + "/slideshow/" + name + "?slide=" + strconv.Itoa(slide+1)
	}
	m.renderExtra(w, "slideshow.html", http.StatusOK, page)
}

// slideshowsDir is where a slideshow's pictures live: DATA_DIR/images is
// the tree /images/uploads serves, and the slideshows sit in it under
// their own name (PLAN §6, §8).
func (m *Module) slideshowsDir() string {
	return filepath.Join(m.cfg.DataDir, "images", "slideshows")
}

// slideshowImages lists a slideshow's pictures, sorted by name.
func slideshowImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		if slideshowExts[strings.ToLower(filepath.Ext(e.Name()))] {
			out = append(out, e.Name())
		}
	}
	sort.Strings(out)
	return out, nil
}

// readSlideshowMetadata reads metadata.xml, if the slideshow has one. A
// missing or broken file is not an error: the slideshow shows without a
// display name and without captions, as slideshow.cfc's getInfo did.
func readSlideshowMetadata(dir string) slideshowMetadata {
	var meta slideshowMetadata
	raw, err := os.ReadFile(filepath.Join(dir, "metadata.xml"))
	if err != nil {
		return meta
	}
	if err := xml.Unmarshal(raw, &meta); err != nil {
		slog.Warn("web: slideshow metadata is not valid XML", "dir", dir, "error", err)
		return slideshowMetadata{}
	}
	return meta
}
