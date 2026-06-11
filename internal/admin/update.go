// update.go backs the /manage/update page: it shows the running vs latest
// version and, when a one-click update command is configured, exposes a
// CSRF-protected POST that launches it. The dangerous action (privileged
// self-update) is deliberately two-step — a GET confirmation page precedes the
// POST — and never takes any request-derived data into the command.
package admin

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/update"
)

// UpdateHandlers serves the self-update page. Repo is "owner/name", used only to
// build the human-facing releases link.
type UpdateHandlers struct {
	Parent  *Handlers
	Checker *update.Checker
	Updater *update.Updater
	Repo    string
}

// releaseURL returns the GitHub latest-release page for the configured repo, or
// "" when no repo is set.
func (h *UpdateHandlers) releaseURL() string {
	if h.Repo == "" {
		return ""
	}
	return fmt.Sprintf("https://github.com/%s/releases/latest", h.Repo)
}

// viewData assembles the template payload shared by GET and POST renders.
func (h *UpdateHandlers) viewData(sess auth.Session) map[string]any {
	st := h.Checker.State()
	return map[string]any{
		"Current":    st.Current,
		"Latest":     st.Latest,
		"Available":  st.Available,
		"CheckedAt":  st.CheckedAt,
		"Enabled":    h.Updater.Enabled(),
		"Running":    h.Updater.Running(),
		"ReleaseURL": h.releaseURL(),
		"CSRF":       sess.CSRF,
		"Updating":   false,
		"Error":      "",
	}
}

// Page renders GET /manage/update — the status + confirmation view.
func (h *UpdateHandlers) Page(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.Parent.Auth.ParseSession(r)
	if !ok {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	if err := h.Parent.Tpl.Render(w, r, http.StatusOK, "admin_update.html", h.viewData(sess)); err != nil {
		h.Parent.Logger.Error("admin.update.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// Trigger handles POST /manage/update — validates CSRF, launches the update,
// and renders a "restarting" page. The command runs detached; this process may
// be restarted out from under the response, so the page meta-refreshes back to
// /manage rather than relying on a follow-up request to this handler.
func (h *UpdateHandlers) Trigger(w http.ResponseWriter, r *http.Request) {
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

	data := h.viewData(sess)
	status := http.StatusOK
	switch err := h.Updater.Trigger(); {
	case err == nil, errors.Is(err, update.ErrInProgress):
		// Already running counts as success for the user's intent.
		data["Updating"] = true
		h.Parent.Logger.Info("admin.update.trigger",
			slog.String("by", sess.Username),
			slog.String("latest", h.Checker.State().Latest),
		)
	case errors.Is(err, update.ErrDisabled):
		status = http.StatusForbidden
		data["Error"] = "未配置 update_command，无法在线更新。"
	default:
		status = http.StatusInternalServerError
		data["Error"] = "启动更新失败：" + err.Error()
		h.Parent.Logger.Error("admin.update.trigger", slog.String("err", err.Error()))
	}

	if rerr := h.Parent.Tpl.Render(w, r, status, "admin_update.html", data); rerr != nil {
		h.Parent.Logger.Error("admin.update.render", slog.String("err", rerr.Error()))
	}
}
