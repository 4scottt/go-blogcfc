package admin_test

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFP_A26_ImageUploadRefusesNonImagesAndBrowseInsertsTag covers PLAN
// §9 A26 (admin/imgwin.cfm, admin/imgbrowse.cfm): the two popups the
// entry editor opens. Both are `body#popUpFormBody` documents (PLAN
// §12) that hand an `<img>` tag to the body textarea in the window that
// opened them, and both work as full pages. The as-is trusted the
// uploaded file's extension; this sniffs the bytes.
func TestFP_A26_ImageUploadRefusesNonImagesAndBrowseInsertsTag(t *testing.T) {
	h, dataDir := newDataHarness(t)
	h.user("admin", "Admin")
	h.login("admin")
	flushes := countFlushes(h)
	images := filepath.Join(dataDir, "images")

	resp, body := h.get("/admin/images/upload")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the upload popup: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{`id="popUpFormBody"`, `name="image"`, `value="Upload Image"`, "window.opener"} {
		if !strings.Contains(body, want) {
			t.Errorf("the upload popup is missing %s", want)
		}
	}

	// Anything that is not a GIF, JPEG or PNG is refused on its bytes,
	// whatever the file is called, and nothing is written.
	resp, body = h.postMultipart("/admin/images/upload", nil, "image", "payload.png", []byte("<?php echo 1; ?>"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a non-picture upload: status %d, want 400", resp.StatusCode)
	}
	if !strings.Contains(body, "not a GIF, JPEG or PNG") {
		t.Error("the refusal does not say why")
	}
	if entries, _ := os.ReadDir(images); len(entries) != 0 {
		t.Errorf("a refused upload wrote %d files", len(entries))
	}
	if *flushes != 0 {
		t.Errorf("a refused upload flushed the caches (%d flushes)", *flushes)
	}

	// A real PNG under a name that claims otherwise is saved with the
	// extension its bytes call for, and the window hands back the tag.
	resp, body = h.postMultipart("/admin/images/upload", nil, "image", "screenshot.txt", onePixelPNG(t))
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("a picture upload: status %d, want 200", resp.StatusCode)
	}
	if _, err := os.Stat(filepath.Join(images, "screenshot.png")); err != nil {
		t.Fatalf("the picture is not in DATA_DIR/images: %v", err)
	}
	for _, want := range []string{
		`id="popUpFormBody"`,
		"http://127.0.0.1:8080/images/uploads/screenshot.png",
		"&lt;img src=",
		"insertTag(",
		"window.close()",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the upload result is missing %s", want)
		}
	}
	if *flushes != 1 {
		t.Errorf("the flush hook ran %d times on an upload, want 1", *flushes)
	}

	// A second picture of the same name is kept beside the first.
	h.postMultipart("/admin/images/upload", nil, "image", "screenshot.png", onePixelGIF(t))
	if _, err := os.Stat(filepath.Join(images, "screenshot.gif")); err != nil {
		t.Errorf("the second picture is not there: %v", err)
	}

	// A file the popups never wrote is listed all the same, and one that
	// is not a picture is not.
	writeDataFile(t, dataDir, "images/logo.gif", string(onePixelGIF(t)))
	writeDataFile(t, dataDir, "images/notes.txt", "not a picture")

	resp, body = h.get("/admin/images/browse")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("the browser popup: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		`id="popUpFormBody"`,
		"screenshot.png",
		"logo.gif",
		`data-tag="&lt;img src=&#34;http://127.0.0.1:8080/images/uploads/logo.gif&#34; /&gt;"`,
		">Insert</a>",
		"insertTag(",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the image browser is missing %s", want)
		}
	}
	if strings.Contains(body, "notes.txt") {
		t.Error("the image browser lists a file that is not a picture")
	}
	if *flushes != 2 {
		t.Errorf("the flush hook ran %d times, want 2 (one per upload)", *flushes)
	}

	// Both popups are behind the session, like every other admin screen.
	h.get("/admin/logout")
	for _, path := range []string{"/admin/images/upload", "/admin/images/browse"} {
		resp, _ = h.get(path)
		if resp.StatusCode != http.StatusFound {
			t.Errorf("GET %s signed out: status %d, want 302 to the login", path, resp.StatusCode)
		}
	}
}
