// Package admin contains the HTTP handlers behind /manage/* — login, logout,
// password change, and (from P5 onwards) content CRUD. The handlers wrap
// auth.Store for session management and write config changes through
// config.Config.Save with atomic rename.
package admin

import (
	"html/template"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/config"
	"github.com/penguin/blog-server/internal/render"
)

// Handlers aggregates the admin area's dependencies. Fields are public so
// main.go can wire them after construction.
type Handlers struct {
	Auth       *auth.Store
	Config     *config.Config
	ConfigPath string
	Tpl        *render.Templates
	Logger     *slog.Logger
}

// New returns a Handlers with sensible defaults.
func New(a *auth.Store, cfg *config.Config, cfgPath string, tpl *render.Templates, logger *slog.Logger) *Handlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handlers{
		Auth:       a,
		Config:     cfg,
		ConfigPath: cfgPath,
		Tpl:        tpl,
		Logger:     logger,
	}
}

// SessionFromRequest exposes the auth.Session for handlers under /manage/*.
func (h *Handlers) SessionFromRequest(r *http.Request) (auth.Session, bool) {
	return h.Auth.ParseSession(r)
}

// --- Login -----------------------------------------------------------------

// LoginPage renders the login form.
func (h *Handlers) LoginPage(w http.ResponseWriter, r *http.Request) {
	// If already authenticated, go to dashboard.
	if _, ok := h.Auth.ParseSession(r); ok {
		http.Redirect(w, r, nextFrom(r), http.StatusSeeOther)
		return
	}
	data := map[string]any{
		"Error":    r.URL.Query().Get("e"),
		"Next":     r.URL.Query().Get("next"),
		"Username": h.Config.AdminUsername,
	}
	if err := h.Tpl.Render(w, r, http.StatusOK, "admin_login.html", data); err != nil {
		h.Logger.Error("admin.login.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// LoginSubmit handles POST /manage/login.
func (h *Handlers) LoginSubmit(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	ip := auth.RemoteIP(r)
	if limited, end := h.Auth.IsRateLimited(ip); limited {
		retryAfter := int(time.Until(end).Seconds())
		if retryAfter < 1 {
			retryAfter = 1
		}
		w.Header().Set("Retry-After", itoa(retryAfter))
		redirectWithError(w, r, "登录尝试过多，请稍后再试", r.Form.Get("next"))
		return
	}
	username := strings.TrimSpace(r.Form.Get("username"))
	password := r.Form.Get("password")
	if username == "" || password == "" {
		redirectWithError(w, r, "请填写用户名与密码", r.Form.Get("next"))
		return
	}
	if username != h.Config.AdminUsername {
		h.Auth.RegisterFailure(ip)
		redirectWithError(w, r, "用户名或密码错误", r.Form.Get("next"))
		return
	}
	if err := auth.VerifyPassword(h.Config.AdminPasswordBcrypt, password); err != nil {
		count, _, limited := h.Auth.RegisterFailure(ip)
		h.Logger.Info("admin.login.fail", slog.String("ip", ip), slog.Int("count", count))
		if limited {
			redirectWithError(w, r, "登录尝试过多，请稍后再试", r.Form.Get("next"))
			return
		}
		redirectWithError(w, r, "用户名或密码错误", r.Form.Get("next"))
		return
	}
	// Success — issue session, clear failures.
	_, cookie, err := h.Auth.IssueSession(username, r.UserAgent())
	if err != nil {
		h.Logger.Error("admin.login.issue", slog.String("err", err.Error()))
		http.Error(w, "internal", http.StatusInternalServerError)
		return
	}
	http.SetCookie(w, cookie)
	h.Auth.ClearFailures(ip)
	target := r.Form.Get("next")
	if target == "" || !strings.HasPrefix(target, "/manage") {
		target = "/manage"
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

// Logout clears the session cookie.
func (h *Handlers) Logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, h.Auth.ClearCookie())
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// --- Dashboard (placeholder for P5) ---------------------------------------

// Dashboard renders a minimal landing page so authenticated users have
// something to see before P5's CRUD lands.
func (h *Handlers) Dashboard(w http.ResponseWriter, r *http.Request) {
	sess, _ := h.Auth.ParseSession(r)
	data := map[string]any{
		"Username": sess.Username,
		"CSRF":     sess.CSRF,
		"Banner":   h.Config.DefaultPasswordUnchanged(),
		"Changed":  h.Config.PasswordChangedAt,
	}
	if err := h.Tpl.Render(w, r, http.StatusOK, "admin_dashboard.html", data); err != nil {
		h.Logger.Error("admin.dashboard.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// --- Password change ------------------------------------------------------

// PasswordPage renders the change-password form.
func (h *Handlers) PasswordPage(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.Auth.ParseSession(r)
	if !ok {
		http.Redirect(w, r, "/manage/login?next=/manage/password", http.StatusSeeOther)
		return
	}
	data := map[string]any{
		"CSRF":  sess.CSRF,
		"Error": r.URL.Query().Get("e"),
		"Info":  r.URL.Query().Get("m"),
	}
	if err := h.Tpl.Render(w, r, http.StatusOK, "admin_password.html", data); err != nil {
		h.Logger.Error("admin.password.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// PasswordSubmit handles POST /manage/password.
func (h *Handlers) PasswordSubmit(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.Auth.ParseSession(r)
	if !ok {
		http.Redirect(w, r, "/manage/login", http.StatusSeeOther)
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
	oldPw := r.Form.Get("old")
	newPw := r.Form.Get("new")
	confirm := r.Form.Get("confirm")

	if err := auth.VerifyPassword(h.Config.AdminPasswordBcrypt, oldPw); err != nil {
		http.Redirect(w, r, "/manage/password?e="+urlq("旧密码错误"), http.StatusSeeOther)
		return
	}
	if len(newPw) < 8 {
		http.Redirect(w, r, "/manage/password?e="+urlq("新密码至少 8 位"), http.StatusSeeOther)
		return
	}
	if newPw != confirm {
		http.Redirect(w, r, "/manage/password?e="+urlq("两次输入不一致"), http.StatusSeeOther)
		return
	}
	hash, err := auth.HashPassword(newPw)
	if err != nil {
		http.Error(w, "hash fail", http.StatusInternalServerError)
		return
	}
	now := time.Now().UTC()
	prevHash := h.Config.AdminPasswordBcrypt
	prevChanged := h.Config.PasswordChangedAt
	h.Config.AdminPasswordBcrypt = hash
	h.Config.PasswordChangedAt = &now
	if err := h.Config.Save(h.ConfigPath); err != nil {
		// Roll back in-memory on disk failure.
		h.Config.AdminPasswordBcrypt = prevHash
		h.Config.PasswordChangedAt = prevChanged
		h.Logger.Error("admin.password.save", slog.String("err", err.Error()))
		http.Redirect(w, r, "/manage/password?e="+urlq("保存失败，请重试"), http.StatusSeeOther)
		return
	}
	h.Logger.Info("admin.password.changed", slog.String("user", sess.Username))
	http.Redirect(w, r, "/manage?m="+urlq("密码已更新"), http.StatusSeeOther)
}

// --- Helpers ---------------------------------------------------------------

func redirectWithError(w http.ResponseWriter, r *http.Request, msg, next string) {
	target := "/manage/login?e=" + urlq(msg)
	if next != "" {
		target += "&next=" + urlq(next)
	}
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func nextFrom(r *http.Request) string {
	n := r.URL.Query().Get("next")
	if strings.HasPrefix(n, "/manage") {
		return n
	}
	return "/manage"
}

// urlq escapes a string for use as a URL query value; tiny wrapper to keep
// redirect-building lines short.
func urlq(s string) string { return template.URLQueryEscaper(s) }

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
