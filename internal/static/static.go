// Package static holds the embedded assets served under /static/.
package static

import "embed"

// FS is the asset tree. Paths are relative to this package, so
// css/site.css is served as /static/css/site.css.
//
//go:embed css
var FS embed.FS
