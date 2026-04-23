package admin

import (
	"log/slog"
	"net/http"
	"strings"

	"github.com/penguin/blog-server/internal/auth"
)

// sessionRow 是传给模板的展示结构 —— 跟 auth.SessionRecord 分开，避免
// 模板直接耦合 auth 内部字段；顺便把 UA 截短为"人眼好认"的设备描述。
type sessionRow struct {
	SID       string
	UserAgent string
	IP        string
	IssuedAt  string // RFC3339 本地短格式
	IsCurrent bool
}

// SessionsPage renders GET /manage/sessions —— 已登陆设备列表。
func (h *Handlers) SessionsPage(w http.ResponseWriter, r *http.Request) {
	sess, ok := h.Auth.ParseSession(r)
	if !ok {
		http.Redirect(w, r, "/manage/login?next=/manage/sessions", http.StatusSeeOther)
		return
	}
	records, err := h.Auth.ListSessions(sess.Username)
	if err != nil {
		h.Logger.Error("admin.sessions.list", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
		return
	}
	rows := make([]sessionRow, 0, len(records))
	for _, rec := range records {
		rows = append(rows, sessionRow{
			SID:       rec.SID,
			UserAgent: shortUA(rec.UserAgent),
			IP:        rec.IP,
			IssuedAt:  rec.IssuedAt.Local().Format("2006-01-02 15:04"),
			IsCurrent: rec.SID == sess.SID,
		})
	}
	data := map[string]any{
		"CSRF":     sess.CSRF,
		"Username": sess.Username,
		"Sessions": rows,
		"Info":     r.URL.Query().Get("m"),
		"Error":    r.URL.Query().Get("e"),
	}
	if err := h.Tpl.Render(w, r, http.StatusOK, "admin_sessions.html", data); err != nil {
		h.Logger.Error("admin.sessions.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

// SessionsRevoke handles POST /manage/sessions/revoke。
// 表单字段：csrf + sid。仅能撤销当前用户名下的 sid，越权尝试静默返回（不泄露是否存在）。
func (h *Handlers) SessionsRevoke(w http.ResponseWriter, r *http.Request) {
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
	sid := strings.TrimSpace(r.Form.Get("sid"))
	if sid == "" {
		http.Redirect(w, r, "/manage/sessions?e="+urlq("缺少 sid"), http.StatusSeeOther)
		return
	}
	n, err := h.Auth.RevokeSession(sid, sess.Username)
	if err != nil {
		h.Logger.Error("admin.sessions.revoke", slog.String("err", err.Error()))
		http.Redirect(w, r, "/manage/sessions?e="+urlq("撤销失败"), http.StatusSeeOther)
		return
	}
	h.Logger.Info("admin.sessions.revoked",
		slog.String("by", sess.Username),
		slog.String("sid", sid),
		slog.Int64("n", n),
	)
	// 如果撤销的是当前会话，下一次请求就会被 AuthGate 踢回登录页；这里先把
	// cookie 也清掉，避免用户看到自己撤销完却还"登着"的错觉。
	if sid == sess.SID {
		http.SetCookie(w, h.Auth.ClearCookie())
		http.Redirect(w, r, "/manage/login?m="+urlq("已撤销当前会话，请重新登录"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/manage/sessions?m="+urlq("已撤销该设备"), http.StatusSeeOther)
}

// shortUA 把 User-Agent 字符串截成"人眼好认"的短描述。
// UA 原串往往上百字符、全是版本号噪声；这里只保留前若干个 token 的关键词，
// 足以让人区分"Chrome on Mac"、"Safari on iPhone"、"curl/..."。
func shortUA(ua string) string {
	if ua == "" {
		return "(未知)"
	}
	// 粗糙但够用：挑出常见浏览器/系统关键词。
	// 顺序有意义 —— Edge 在 Chrome 之前，因为 Edge UA 也含 "Chrome"。
	keywords := []string{"Edg/", "OPR/", "Firefox/", "Chrome/", "Safari/", "curl/", "wget/"}
	var hits []string
	for _, k := range keywords {
		if i := strings.Index(ua, k); i >= 0 {
			// 取 keyword + 后续最多 8 字符（通常是主版本号）
			end := i + len(k) + 8
			if end > len(ua) {
				end = len(ua)
			}
			seg := strings.TrimSpace(ua[i:end])
			if sp := strings.IndexByte(seg, ' '); sp > 0 {
				seg = seg[:sp]
			}
			hits = append(hits, seg)
			// Chrome/Safari 容易同时出现，只取最上游一个够了
			break
		}
	}
	os := ""
	switch {
	case strings.Contains(ua, "Windows NT"):
		os = "Windows"
	case strings.Contains(ua, "Macintosh"):
		os = "macOS"
	case strings.Contains(ua, "iPhone"):
		os = "iPhone"
	case strings.Contains(ua, "iPad"):
		os = "iPad"
	case strings.Contains(ua, "Android"):
		os = "Android"
	case strings.Contains(ua, "Linux"):
		os = "Linux"
	}
	out := strings.Join(hits, " ")
	if os != "" {
		if out != "" {
			out += " · " + os
		} else {
			out = os
		}
	}
	if out == "" {
		// 兜底：截取前 40 字符
		if len(ua) > 40 {
			return ua[:40] + "…"
		}
		return ua
	}
	return out
}
