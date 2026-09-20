package admin_test

import (
	"bytes"
	"encoding/xml"
	"image"
	"image/color"
	"image/gif"
	"image/png"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// onePixelPNG is a real one-pixel PNG: the uploaders sniff the bytes, so a
// test cannot get away with a name that ends in .png.
func onePixelPNG(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Set(0, 0, color.RGBA{R: 0x33, G: 0x66, B: 0x99, A: 0xff})
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

// onePixelGIF is a real one-pixel GIF.
func onePixelGIF(t *testing.T) []byte {
	t.Helper()
	img := image.NewPaletted(image.Rect(0, 0, 1, 1), color.Palette{color.Black, color.White})
	var buf bytes.Buffer
	if err := gif.Encode(&buf, img, nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}
	return buf.Bytes()
}

// publicSlideshowMeta is metadata.xml read the way internal/web reads it
// (web/slideshow.go), so the test proves the admin writes what the
// public page can read back.
type publicSlideshowMeta struct {
	XMLName    xml.Name `xml:"slideshow"`
	FormalName string   `xml:"formalname"`
	Captions   struct {
		Images []struct {
			Name    string `xml:"name,attr"`
			Caption string `xml:"caption,attr"`
		} `xml:",any"`
	} `xml:"captions"`
}

// TestFP_A24_SlideshowsCreateRenameFormalNameUploadDelete covers PLAN §9
// A24 (admin/slideshows.cfm, admin/slideshow.cfm): a slideshow is a
// directory under DATA_DIR/images/slideshows with a validated name, a
// formal name in metadata.xml, pictures accepted on their sniffed type,
// and a rename that moves the directory and keeps the captions.
func TestFP_A24_SlideshowsCreateRenameFormalNameUploadDelete(t *testing.T) {
	h, dataDir := newDataHarness(t)
	h.user("admin", "Admin")
	h.login("admin")
	flushes := countFlushes(h)
	root := filepath.Join(dataDir, "images", "slideshows")

	resp, body := h.get("/admin/slideshows")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("slideshows: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{"<h1>Slideshows</h1>", `name="name"`, `value="Add Slideshow"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the slideshows list is missing %s", want)
		}
	}

	// A name that is not `^[A-Za-z0-9_-]+$` never reaches the file system.
	resp, body = h.postForm("/admin/slideshows", url.Values{"name": {"../escape"}, "add": {"Add Slideshow"}})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "letters, numbers, hyphens and underscores") {
		t.Errorf("a slideshow named ../escape: status %d, want the list back with a message", resp.StatusCode)
	}
	if entries, err := os.ReadDir(root); err == nil && len(entries) != 0 {
		t.Errorf("a refused name left %d directories behind", len(entries))
	}

	resp, _ = h.postForm("/admin/slideshows", url.Values{"name": {"holiday"}, "add": {"Add Slideshow"}})
	redirectedTo(t, resp, "/admin/slideshows/holiday?created=1")
	if info, err := os.Stat(filepath.Join(root, "holiday")); err != nil || !info.IsDir() {
		t.Fatalf("the slideshow directory was not made: %v", err)
	}
	if *flushes != 1 {
		t.Errorf("the flush hook ran %d times on a create, want 1", *flushes)
	}

	// The same name twice is refused rather than silently reused.
	resp, body = h.postForm("/admin/slideshows", url.Values{"name": {"holiday"}, "add": {"Add Slideshow"}})
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "already exists") {
		t.Errorf("a second slideshow called holiday: status %d, want a message", resp.StatusCode)
	}

	resp, body = h.get("/admin/slideshows/holiday")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the slideshow page: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		"<h1>Slideshow Editor</h1>", `name="newname"`, `name="formalname"`,
		`value="Rename"`, `name="image"`, `value="Upload"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the slideshow page is missing %s", want)
		}
	}

	// A file that is not a picture is refused on its bytes, whatever it
	// is called.
	resp, body = h.postMultipart("/admin/slideshows/holiday/upload", nil, "image", "sneaky.png", []byte("<?php echo 1; ?>"))
	if resp.StatusCode != http.StatusOK || !strings.Contains(body, "not a GIF, JPEG or PNG") {
		t.Errorf("a non-picture upload: status %d, want the page back with a message", resp.StatusCode)
	}
	if entries, _ := os.ReadDir(filepath.Join(root, "holiday")); len(entries) != 0 {
		t.Errorf("a refused upload left %d files behind", len(entries))
	}

	// Two pictures, the second under the same name: the second is kept
	// beside the first (`nameconflict="makeunique"`).
	resp, _ = h.postMultipart("/admin/slideshows/holiday/upload", nil, "image", "beach.png", onePixelPNG(t))
	redirectedTo(t, resp, "/admin/slideshows/holiday?uploaded=beach.png")
	resp, _ = h.postMultipart("/admin/slideshows/holiday/upload", nil, "image", "beach.png", onePixelGIF(t))
	loc := redirectedTo(t, resp, "/admin/slideshows/holiday?uploaded=")
	if !strings.Contains(loc, "beach.gif") {
		t.Errorf("the second upload landed as %q, want a gif named from its bytes", loc)
	}
	for _, name := range []string{"beach.png", "beach.gif"} {
		if _, err := os.Stat(filepath.Join(root, "holiday", name)); err != nil {
			t.Errorf("%s is not in the slideshow: %v", name, err)
		}
	}

	_, body = h.get("/admin/slideshows/holiday")
	for _, want := range []string{
		"beach.png",
		"http://127.0.0.1:8080/images/uploads/slideshows/holiday/beach.png",
		`value="Delete"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the slideshow page is missing %s", want)
		}
	}

	// A caption written by hand (or by an as-is install) survives a
	// rename, which moves the directory and writes the formal name.
	writeDataFile(t, dataDir, "images/slideshows/holiday/metadata.xml",
		`<?xml version="1.0" encoding="UTF-8"?><slideshow><formalname>Old</formalname>`+
			`<captions><image name="beach.png" caption="The beach" /></captions></slideshow>`)

	resp, _ = h.postForm("/admin/slideshows/holiday/rename", url.Values{
		"newname": {"holiday-2026"}, "formalname": {"Holiday 2026"}, "rename": {"Rename"},
	})
	redirectedTo(t, resp, "/admin/slideshows/holiday-2026?renamed=1")
	if _, err := os.Stat(filepath.Join(root, "holiday")); !os.IsNotExist(err) {
		t.Error("the old slideshow directory is still there after the rename")
	}
	meta := readPublicMeta(t, filepath.Join(root, "holiday-2026", "metadata.xml"))
	if meta.FormalName != "Holiday 2026" {
		t.Errorf("metadata.xml holds the formal name %q", meta.FormalName)
	}
	if len(meta.Captions.Images) != 1 || meta.Captions.Images[0].Caption != "The beach" {
		t.Errorf("the captions did not survive the rename: %+v", meta.Captions.Images)
	}
	if _, body = h.get("/admin/slideshows"); !strings.Contains(body, "Holiday 2026") {
		t.Error("the list does not show the formal name")
	}

	// Deleting a picture takes its caption with it.
	resp, _ = h.postForm("/admin/slideshows/holiday-2026/images/delete", url.Values{
		"image": {"beach.png"}, "deleteimage": {"Delete"},
	})
	redirectedTo(t, resp, "/admin/slideshows/holiday-2026?deleted=1")
	if _, err := os.Stat(filepath.Join(root, "holiday-2026", "beach.png")); !os.IsNotExist(err) {
		t.Error("the picture was not deleted")
	}
	meta = readPublicMeta(t, filepath.Join(root, "holiday-2026", "metadata.xml"))
	if len(meta.Captions.Images) != 0 {
		t.Errorf("the deleted picture's caption is still in metadata.xml: %+v", meta.Captions.Images)
	}

	// A picture name that is a path is refused.
	resp, _ = h.postForm("/admin/slideshows/holiday-2026/images/delete", url.Values{"image": {"../../secret"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a delete of ../../secret: status %d, want 400", resp.StatusCode)
	}

	// An unknown slideshow is a 404 on every verb.
	resp, _ = h.get("/admin/slideshows/nosuch")
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("an unknown slideshow: status %d, want 404", resp.StatusCode)
	}

	resp, _ = h.postForm("/admin/slideshows/holiday-2026/delete", url.Values{"delete": {"Delete Slideshow"}})
	redirectedTo(t, resp, "/admin/slideshows?deleted=1")
	if _, err := os.Stat(filepath.Join(root, "holiday-2026")); !os.IsNotExist(err) {
		t.Error("the slideshow directory survived the delete")
	}
	if *flushes < 5 {
		t.Errorf("the flush hook ran %d times, want one per write", *flushes)
	}
}

// readPublicMeta parses metadata.xml the way the public page does.
func readPublicMeta(t *testing.T, path string) publicSlideshowMeta {
	t.Helper()
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	var meta publicSlideshowMeta
	if err := xml.Unmarshal(raw, &meta); err != nil {
		t.Fatalf("metadata.xml is not valid XML: %v\n%s", err, raw)
	}
	return meta
}
