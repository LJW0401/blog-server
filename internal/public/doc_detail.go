package public

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/penguin/blog-server/internal/content"
)

// DocDetail handles GET /docs/:slug.
func (h *Handlers) DocDetail(w http.ResponseWriter, r *http.Request) {
	slug := strings.TrimPrefix(r.URL.Path, "/docs/")
	slug = strings.Trim(slug, "/")
	if slug == "" || strings.Contains(slug, "/") {
		http.NotFound(w, r)
		return
	}
	e, ok := h.Content.Docs().Get(content.KindDoc, slug)
	if !ok {
		http.NotFound(w, r)
		return
	}
	loggedIn := isLoggedIn(r)

	// Status-based access control.
	switch e.Status {
	case content.StatusDraft:
		if !loggedIn {
			http.NotFound(w, r)
			return
		}
	case content.StatusPublished, content.StatusArchived:
		// Accessible to everyone.
	default:
		http.NotFound(w, r)
		return
	}

	prev, next := prevNext(h.Content.Docs().List(content.KindDoc), e)

	// Record a read — only for published (not draft preview or archived).
	if e.Status == content.StatusPublished && h.Stats != nil {
		h.Stats.RecordRead(r.Context(), slug, remoteIP(r), r.UserAgent())
	}
	var readCount int
	if h.Stats != nil {
		readCount = h.Stats.Count(r.Context(), slug)
	}

	data := map[string]any{
		"Doc":       e,
		"Prev":      prev,
		"Next":      next,
		"ReadCount": readCount,
	}
	if err := h.Tpl.Render(w, r, http.StatusOK, "doc_detail.html", data); err != nil {
		h.Logger.Error("docs.detail.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// remoteIP mirrors stats.RemoteIP without an import cycle dance.
func remoteIP(r *http.Request) string {
	if xf := r.Header.Get("X-Forwarded-For"); xf != "" {
		if i := strings.IndexByte(xf, ','); i > 0 {
			return strings.TrimSpace(xf[:i])
		}
		return strings.TrimSpace(xf)
	}
	host := r.RemoteAddr
	if i := strings.LastIndexByte(host, ':'); i > 0 {
		return host[:i]
	}
	return host
}

// prevNext picks adjacent entries within the list (assumed sorted by Updated
// desc). prev is the newer neighbour, next is the older neighbour.
func prevNext(all []*content.Entry, current *content.Entry) (prev, next *content.Entry) {
	for i, e := range all {
		if e.Slug != current.Slug || e.Kind != current.Kind {
			continue
		}
		if i > 0 {
			prev = all[i-1]
		}
		if i+1 < len(all) {
			next = all[i+1]
		}
		return
	}
	return
}

// isLoggedIn is a placeholder admin predicate for phase 2 (real auth arrives
// in phase 4). Requests with the header X-Preview-Admin: 1 are treated as
// logged in, enabling draft preview for smoke tests and the admin workflow
// bootstrap.
func isLoggedIn(r *http.Request) bool {
	return r.Header.Get("X-Preview-Admin") == "1"
}
