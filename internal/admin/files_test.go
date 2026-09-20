package admin_test

import (
	"context"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/4scottt/go-blogcfc/internal/admin"
	"github.com/4scottt/go-blogcfc/internal/auth"
	"github.com/4scottt/go-blogcfc/internal/config"
	"github.com/4scottt/go-blogcfc/internal/testdb"
)

// dataHarness is newHarness with a DATA_DIR of its own: the file
// manager, the slideshows and the image popups all work on the tree
// under it (PLAN §6), and the shared harness leaves it empty on
// purpose so a screen that writes files cannot write anywhere by
// accident. It returns the same *harness, so every helper on it works.
func newDataHarness(t *testing.T) (*harness, string) {
	t.Helper()
	st := testdb.New(t)
	dataDir := t.TempDir()

	cfg := &config.Config{Port: 8080, BlogBaseURL: "http://127.0.0.1:8080",
		SessionSecret: "test-session-secret", DataDir: dataDir}
	settings := config.NewSettings(st, cfg)
	if err := settings.Reload(context.Background()); err != nil {
		t.Fatalf("settings reload: %v", err)
	}
	sessions := auth.New(cfg.SessionSecret, false, st)
	m := admin.New(cfg, st, settings, sessions)

	mux := http.NewServeMux()
	m.Routes(mux)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatalf("cookiejar: %v", err)
	}
	client := &http.Client{
		Jar:           jar,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	return &harness{t: t, module: m, store: st, settings: settings, server: srv, client: client}, dataDir
}

// countFlushes makes the module's cache-flush hook countable: every
// screen that writes has to call it (PLAN §9 A29).
func countFlushes(h *harness) *int {
	n := 0
	h.module.Flush = func() { n++ }
	return &n
}

// writeDataFile puts a file in the tree the file manager browses.
func writeDataFile(t *testing.T, dataDir, rel, content string) string {
	t.Helper()
	full := filepath.Join(dataDir, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		t.Fatalf("mkdir for %s: %v", rel, err)
	}
	if err := os.WriteFile(full, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", rel, err)
	}
	return full
}

// TestFP_A25_FileManagerScopedToDataDirRefusesTraversal covers PLAN §9
// A25 (admin/filemanager.cfm): the listing of DATA_DIR and its two
// trees, a directory row that links deeper, download and delete per
// file, upload into the directory being shown, and every way out of
// DATA_DIR refused. The as-is browsed the webroot and quietly rewrote a
// `..` to `/`; this answers 400.
func TestFP_A25_FileManagerScopedToDataDirRefusesTraversal(t *testing.T) {
	h, dataDir := newDataHarness(t)
	h.user("admin", "Admin")
	h.login("admin")
	flushes := countFlushes(h)

	writeDataFile(t, dataDir, "enclosures/report.txt", "the quarterly numbers")
	// A file the blog has no business serving, one level above DATA_DIR.
	secret := filepath.Join(filepath.Dir(dataDir), "outside.txt")
	if err := os.WriteFile(secret, []byte("not yours"), 0o644); err != nil {
		t.Fatalf("write the outside file: %v", err)
	}

	resp, body := h.get("/admin/files")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("files: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{
		"<h1>Files</h1>",
		`href="/admin/files?dir=enclosures"`,
		`href="/admin/files?dir=images"`,
		`id="uploadFileForm"`,
		`name="file"`,
		`name="dir"`,
		`value="Upload File"`,
	} {
		if !strings.Contains(body, want) {
			t.Errorf("the file manager is missing %s", want)
		}
	}

	resp, body = h.get("/admin/files?dir=enclosures")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("files in enclosures: status %d, want 200", resp.StatusCode)
	}
	for _, want := range []string{"report.txt", `value="Download"`, `value="Delete"`, `id="updir"`} {
		if !strings.Contains(body, want) {
			t.Errorf("the enclosures listing is missing %s", want)
		}
	}

	// Every shape of escape is a 400, not a redirect to the root.
	for _, dir := range []string{"..", "../", "enclosures/../..", "/etc", "..%2f..", `images\..`} {
		resp, _ = h.get("/admin/files?dir=" + url.QueryEscape(dir))
		if resp.StatusCode != http.StatusBadRequest {
			t.Errorf("GET /admin/files?dir=%q: status %d, want 400", dir, resp.StatusCode)
		}
	}
	resp, _ = h.get("/admin/files/download?dir=&file=" + url.QueryEscape("../outside.txt"))
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a download of ../outside.txt: status %d, want 400", resp.StatusCode)
	}

	// Download streams the file itself.
	resp, body = h.get("/admin/files/download?dir=enclosures&file=report.txt")
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("download: status %d, want 200", resp.StatusCode)
	}
	if body != "the quarterly numbers" {
		t.Errorf("download body = %q, want the file's contents", body)
	}
	if cd := resp.Header.Get("Content-Disposition"); !strings.Contains(cd, "attachment") || !strings.Contains(cd, "report.txt") {
		t.Errorf("Content-Disposition = %q, want an attachment named report.txt", cd)
	}
	if *flushes != 0 {
		t.Errorf("reading files flushed the caches %d times", *flushes)
	}

	// Upload lands in the directory the form carried.
	resp, _ = h.postMultipart("/admin/files/upload",
		url.Values{"dir": {"enclosures"}}, "file", "notes.txt", []byte("uploaded bytes"))
	loc := redirectedTo(t, resp, "/admin/files?dir=enclosures")
	if !strings.Contains(loc, "uploaded=notes.txt") {
		t.Errorf("after an upload: Location = %q, want the uploaded file's name", loc)
	}
	got, err := os.ReadFile(filepath.Join(dataDir, "enclosures", "notes.txt"))
	if err != nil {
		t.Fatalf("the uploaded file is not in DATA_DIR: %v", err)
	}
	if string(got) != "uploaded bytes" {
		t.Errorf("the uploaded file holds %q", got)
	}
	if *flushes != 1 {
		t.Errorf("the flush hook ran %d times after an upload, want 1", *flushes)
	}

	// A second upload of the same name is kept beside the first, not over it.
	h.postMultipart("/admin/files/upload",
		url.Values{"dir": {"enclosures"}}, "file", "notes.txt", []byte("second"))
	if _, err := os.Stat(filepath.Join(dataDir, "enclosures", "notes-1.txt")); err != nil {
		t.Errorf("a name clash did not make a unique name: %v", err)
	}
	if again, _ := os.ReadFile(filepath.Join(dataDir, "enclosures", "notes.txt")); string(again) != "uploaded bytes" {
		t.Error("the second upload overwrote the first")
	}

	// Delete takes one file, and refuses anything that is not a plain name.
	resp, _ = h.postForm("/admin/files/delete", url.Values{"dir": {"enclosures"}, "file": {"../../outside.txt"}})
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("a delete of ../../outside.txt: status %d, want 400", resp.StatusCode)
	}
	if _, err := os.Stat(secret); err != nil {
		t.Fatalf("the file outside DATA_DIR is gone: %v", err)
	}
	resp, _ = h.postForm("/admin/files/delete", url.Values{"dir": {"enclosures"}, "file": {"report.txt"}})
	redirectedTo(t, resp, "/admin/files?dir=enclosures")
	if _, err := os.Stat(filepath.Join(dataDir, "enclosures", "report.txt")); !os.IsNotExist(err) {
		t.Error("the file was not deleted")
	}
	if *flushes != 3 {
		t.Errorf("the flush hook ran %d times, want 3 (two uploads and a delete)", *flushes)
	}

	// With `filebrowse` off the screen is not there at all, as the as-is had it.
	h.setSetting("filebrowse", "no")
	resp, _ = h.get("/admin/files")
	if resp.StatusCode != http.StatusForbidden {
		t.Errorf("with filebrowse off: status %d, want 403", resp.StatusCode)
	}
}
