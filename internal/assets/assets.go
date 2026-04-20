// Package assets embeds the project's frontend resources (HTML templates +
// static CSS/JS) into the final binary so deployment is a single file.
package assets

import (
	"embed"
	"io/fs"
)

//go:embed templates/*.html
var templatesFS embed.FS

//go:embed static
var staticFS embed.FS

//go:embed defaults/about.md
var defaultAboutMD string

// DefaultAbout returns the fallback Markdown for the /about page shown when
// no admin-authored content/about.md exists. Source lives at
// internal/assets/defaults/about.md — edit that file to change the default.
func DefaultAbout() string { return defaultAboutMD }

// Templates returns an fs.FS rooted at the templates directory.
func Templates() fs.FS {
	sub, err := fs.Sub(templatesFS, "templates")
	if err != nil {
		panic(err) // build-time correctness
	}
	return sub
}

// Static returns an fs.FS rooted at the static directory, suitable for
// http.FileServer.
func Static() fs.FS {
	sub, err := fs.Sub(staticFS, "static")
	if err != nil {
		panic(err)
	}
	return sub
}
