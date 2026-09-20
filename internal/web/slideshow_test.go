package web

import (
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestFP_P23_SlideshowOneImagePrevNextClamped is P23: one picture a page
// from DATA_DIR/images/slideshows/{name}, in file-name order, with the
// caption metadata.xml gives it, prev/next links and an index clamped
// into range (PLAN §9 P23, slideshow.cfm and slideshow.cfc).
func TestFP_P23_SlideshowOneImagePrevNextClamped(t *testing.T) {
	s := newExtraSite(t)
	// Written out of order: the slideshow sorts by name.
	s.writeDataFile("images/slideshows/holiday/03.gif", "GIF")
	s.writeDataFile("images/slideshows/holiday/01.jpg", "JPEG")
	s.writeDataFile("images/slideshows/holiday/02.png", "PNG")
	// Neither of these is a picture and neither may be counted.
	s.writeDataFile("images/slideshows/holiday/notes.txt", "not a picture")
	s.writeDataFile("images/slideshows/holiday/metadata.xml", `<slideshow>
	<formalname>Holiday 2011</formalname>
	<captions>
		<image name="02.png" caption="The beach at dawn" />
	</captions>
</slideshow>`)

	first := s.getOK("/slideshow/holiday")
	mustContain(t, "the first slide", first,
		"Slideshow - Holiday 2011 - Picture 1 of 3",
		`<img src="`+testBase+`/images/uploads/slideshows/holiday/01.jpg"`,
		`<a href="`+testBase+`/slideshow/holiday?slide=2">Next</a>`,
	)
	if strings.Contains(first, `>Previous</a>`) {
		t.Error("the first slide should not link to a previous one")
	}

	second := s.getOK("/slideshow/holiday?slide=2")
	mustContain(t, "the second slide", second,
		"Picture 2 of 3",
		`<img src="`+testBase+`/images/uploads/slideshows/holiday/02.png"`,
		"The beach at dawn",
		`<a href="`+testBase+`/slideshow/holiday?slide=1">Previous</a>`,
		`<a href="`+testBase+`/slideshow/holiday?slide=3">Next</a>`,
	)

	// Past the end: clamped to the last picture, which has no Next.
	last := s.getOK("/slideshow/holiday?slide=99")
	mustContain(t, "a slide past the end", last,
		"Picture 3 of 3",
		`<img src="`+testBase+`/images/uploads/slideshows/holiday/03.gif"`,
		`<a href="`+testBase+`/slideshow/holiday?slide=2">Previous</a>`,
	)
	if strings.Contains(last, `>Next</a>`) {
		t.Error("the last slide should not link to a next one")
	}
	// Below the start, and anything that is not a number at all.
	for _, q := range []string{"?slide=0", "?slide=-3", "?slide=two", "?slide="} {
		if body := s.getOK("/slideshow/holiday" + q); !strings.Contains(body, "Picture 1 of 3") {
			t.Errorf("%q did not clamp to the first picture", q)
		}
	}

	// A slideshow without metadata.xml still shows, without a name or
	// captions.
	s.writeDataFile("images/slideshows/plain/a.jpeg", "JPEG")
	mustContain(t, "a slideshow without metadata", s.getOK("/slideshow/plain"),
		"Slideshow - Picture 1 of 1",
		`<img src="`+testBase+`/images/uploads/slideshows/plain/a.jpeg"`,
	)

	// An unknown slideshow, an empty one and a name that is not a plain
	// directory name are all 404s.
	if err := os.MkdirAll(filepath.Join(s.dataDir, "images", "slideshows", "empty"), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	for _, path := range []string{"/slideshow/missing", "/slideshow/empty", "/slideshow/..%2Fholiday", "/slideshow/a.b"} {
		if rec := s.get(path); rec.Code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, rec.Code)
		}
	}

	// The picture itself is served from DATA_DIR/images.
	rec := s.get("/images/uploads/slideshows/holiday/01.jpg")
	if rec.Code != http.StatusOK || rec.Body.String() != "JPEG" {
		t.Errorf("the image answered %d %q", rec.Code, rec.Body.String())
	}
}
