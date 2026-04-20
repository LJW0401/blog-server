package public

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/penguin/blog-server/internal/assets"
)

// About handles GET /about. Body is read from AboutPath (a plain-Markdown
// file outside content/docs so the page is managed on its own admin page
// and never leaks into blog listings/feeds/nav). When the file is missing
// or empty we fall back to assets.DefaultAbout() so a fresh deploy always
// has a sensible /about — the admin replaces it via /manage/about.
func (h *Handlers) About(w http.ResponseWriter, r *http.Request) {
	body := ""
	if h.AboutPath != "" {
		if b, err := os.ReadFile(h.AboutPath); err == nil {
			body = strings.TrimSpace(string(b))
		}
	}
	if body == "" {
		body = strings.TrimSpace(assets.DefaultAbout())
	}
	if body == "" {
		// Shouldn't happen in practice (defaults file is embedded) but keep
		// the 404 path so the test suite can force it by overriding both.
		h.NotFound(w, r)
		return
	}
	data := map[string]any{"Body": body}
	if err := h.Tpl.Render(w, r, http.StatusOK, "about.html", data); err != nil {
		h.Logger.Error("about.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}
