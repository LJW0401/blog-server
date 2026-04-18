package main

import (
	"net/http"
	"strings"

	"github.com/penguin/blog-server/internal/admin"
)

// buildAdminMux assembles the protected /manage/* routes. Split out of main()
// so the entry point stays within the project's gocyclo threshold.
func buildAdminMux(
	adm *admin.Handlers,
	docs *admin.DocHandlers,
	images *admin.ImageHandlers,
	settings *admin.SettingsHandlers,
	projects *admin.ProjectHandlers,
) *http.ServeMux {
	mux := http.NewServeMux()

	mux.HandleFunc("/manage", adm.Dashboard)
	mux.HandleFunc("/manage/password", postOrGet(adm.PasswordSubmit, adm.PasswordPage))

	// Docs
	mux.HandleFunc("/manage/docs", docs.DocsList)
	mux.HandleFunc("/manage/docs/new", postOrGet(docs.SaveDoc, docs.NewDoc))
	mux.HandleFunc("/manage/docs/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/edit") && r.Method == http.MethodGet:
			docs.EditDoc(w, r)
		case strings.HasSuffix(r.URL.Path, "/edit") && r.Method == http.MethodPost:
			docs.SaveDoc(w, r)
		case strings.HasSuffix(r.URL.Path, "/delete") && r.Method == http.MethodPost:
			docs.DeleteDoc(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	// Images
	mux.HandleFunc("/manage/images", images.ImagesList)
	mux.HandleFunc("/manage/images/upload", images.ImagesUpload)

	// Settings
	mux.HandleFunc("/manage/settings", postOrGet(settings.SettingsSubmit, settings.SettingsPage))

	// Projects
	mux.HandleFunc("/manage/repos", projects.ReposList)
	mux.HandleFunc("/manage/repos/new", projects.ReposNew)
	mux.HandleFunc("/manage/projects/", func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/edit") && r.Method == http.MethodGet:
			projects.EditProject(w, r)
		case strings.HasSuffix(r.URL.Path, "/edit") && r.Method == http.MethodPost:
			projects.SaveProject(w, r)
		case strings.HasSuffix(r.URL.Path, "/delete") && r.Method == http.MethodPost:
			projects.ReposDelete(w, r)
		default:
			http.NotFound(w, r)
		}
	})

	return mux
}

// postOrGet returns a handler that dispatches to `post` on POST and `get`
// on anything else. Keeps the individual route wiring a one-liner.
func postOrGet(post, get http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			post(w, r)
			return
		}
		get(w, r)
	}
}
