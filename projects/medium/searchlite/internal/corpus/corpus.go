// Package corpus is the demo document set, compiled into the binary so that
// `searchlite serve` works with zero setup.
package corpus

import (
	"embed"
	"io/fs"
)

//go:embed docs/*.md
var docs embed.FS

// FS returns the documents rooted so that names are plain file names.
func FS() fs.FS {
	sub, _ := fs.Sub(docs, "docs") // fails only for a malformed path
	return sub
}
