// update.go backs the dashboard's "检测新版本" button: a CSRF-protected POST that
// forces a synchronous release check, then redirects back to the version bar.
// Version detection only — the in-app one-click self-update was removed; when a
// newer release exists the dashboard links out to the GitHub release page.
package admin

import (
	"net/http"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/update"
)

// UpdateHandlers serves the dashboard's force-check endpoint. It holds only the
// release Checker; rendering of the version bar happens in the dashboard via the
// UpdateBanner state injected by middleware.
type UpdateHandlers struct {
	Parent  *Handlers
	Checker *update.Checker
}

// Check handles POST /manage/update/check — the dashboard "检测新版本" button.
// It forces a synchronous release check (instead of waiting for the poll) then
// redirects back to the dashboard, where the version bar reflects the refreshed
// state. CSRF-protected; a fetch failure is logged inside CheckNow and leaves
// prior state intact (fail-soft).
func (h *UpdateHandlers) Check(w http.ResponseWriter, r *http.Request) {
	// POST-only: this mutates checker state, and r.ParseForm() would otherwise
	// fold a GET query into r.Form, letting GET ...?csrf=<token> trigger a
	// check.
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	sess, ok := h.Parent.Auth.ParseSession(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	if !auth.CSRFValid(sess, r.Form.Get("csrf")) {
		http.Error(w, "csrf", http.StatusForbidden)
		return
	}
	h.Checker.CheckNow(r.Context())
	// Redirect to the #version anchor so the full-page reload lands back on the
	// version bar instead of scrolling to the top of the dashboard.
	http.Redirect(w, r, "/manage#version", http.StatusSeeOther)
}
