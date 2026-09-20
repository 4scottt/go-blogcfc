package feeds

import (
	"encoding/xml"
	"io"
	"strings"
)

// xmlWellFormed reports whether a document parses, which is all a golden
// file cannot tell us.
func xmlWellFormed(doc string) error {
	dec := xml.NewDecoder(strings.NewReader(doc))
	for {
		_, err := dec.Token()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}
	}
}
