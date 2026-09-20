// Package fp holds the traceability test: every function point in
// docs/function-points.md has a test whose name contains its id
// (TestFP_<id>_…), or, for a row whose only test kind is the walk (w) or
// CI, a marker comment in the script or workflow that exercises it.
package fp

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// pending are ids whose milestone has not landed yet. Each entry is
// removed when its milestone does; the list must be empty at v0.1.0.
var pending = map[string]string{
	"O03": "M6 telemetry",
	"O04": "M6 telemetry",
	"O05": "M6 base URL audit",
	"O07": "M6 image workflow",
}

var (
	rowRe  = regexp.MustCompile(`^\| ([A-Z]\d{2}) \|(.*)\|\s*$`)
	testRe = regexp.MustCompile(`func TestFP_([A-Z]\d{2})_`)
)

func TestFunctionPointsCovered(t *testing.T) {
	root := repoRoot(t)

	// The contract: id → the kinds of test the row asks for.
	kinds := map[string]string{}
	f, err := os.Open(filepath.Join(root, "docs", "function-points.md"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		m := rowRe.FindStringSubmatch(sc.Text())
		if m == nil {
			continue
		}
		cells := strings.Split(m[2], "|")
		kinds[m[1]] = strings.TrimSpace(cells[len(cells)-1])
	}
	if len(kinds) < 100 {
		t.Fatalf("only %d function points parsed; the table format changed?", len(kinds))
	}

	// What exists: TestFP_ names across every _test.go file.
	tested := map[string]bool{}
	err = filepath.WalkDir(root, func(p string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == "node_modules" || strings.HasPrefix(d.Name(), ".walk")) {
			return filepath.SkipDir
		}
		if !strings.HasSuffix(p, "_test.go") {
			return nil
		}
		b, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		for _, m := range testRe.FindAllStringSubmatch(string(b), -1) {
			tested[m[1]] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}

	// Walk-only and CI-only rows: a "FP: <id>" marker in the script or
	// the workflows.
	markers := map[string]bool{}
	for _, p := range []string{"scripts/walk.mjs", ".github/workflows/test.yml", ".github/workflows/image.yml"} {
		b, err := os.ReadFile(filepath.Join(root, p))
		if err != nil {
			continue
		}
		for _, m := range regexp.MustCompile(`FP: ([A-Z]\d{2})`).FindAllStringSubmatch(string(b), -1) {
			markers[m[1]] = true
		}
	}

	var missing []string
	for id, kind := range kinds {
		if pending[id] != "" {
			continue
		}
		byMarker := kind == "w" || kind == "CI"
		if byMarker && markers[id] {
			continue
		}
		if !byMarker && tested[id] {
			continue
		}
		missing = append(missing, id+" ("+kind+")")
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d function points without a test or marker:\n  %s", len(missing), strings.Join(missing, "\n  "))
	}
	for id := range pending {
		if tested[id] || markers[id] {
			t.Errorf("%s is covered now: remove it from pending", id)
		}
	}
}

func repoRoot(t *testing.T) string {
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("go.mod not found above the test directory")
		}
		dir = parent
	}
}
