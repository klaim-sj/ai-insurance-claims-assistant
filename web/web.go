// Package web menyematkan template HTML dan aset statis (CSS hasil build
// Tailwind, htmx.min.js, app.js) sehingga binary dapat berjalan tanpa
// Node.js maupun CDN.
package web

import (
	"embed"
	"io/fs"
)

//go:embed templates
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

// Templates mengembalikan filesystem berakar di folder templates.
func Templates() fs.FS {
	sub, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		panic(err)
	}
	return sub
}

// Static mengembalikan filesystem berakar di folder static.
func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
