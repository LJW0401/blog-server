package diary

import (
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/penguin/blog-server/internal/auth"
	"github.com/penguin/blog-server/internal/render"
)

// Handlers 持有 diary 相关 HTTP 入口需要的依赖。字段公开以便 main.go 在组装
// 时直接注入，方向参考 internal/admin.Handlers。
type Handlers struct {
	Store  *Store
	Tpl    *render.Templates
	Auth   *auth.Store
	Logger *slog.Logger
	// Now 允许测试注入固定时间；生产下为 nil 时走 time.Now()。
	Now func() time.Time
}

// New 构造 Handlers，确保 logger 非 nil，Now 有默认。
func New(store *Store, tpl *render.Templates, authStore *auth.Store, logger *slog.Logger) *Handlers {
	if logger == nil {
		logger = slog.Default()
	}
	return &Handlers{
		Store:  store,
		Tpl:    tpl,
		Auth:   authStore,
		Logger: logger,
		Now:    func() time.Time { return time.Now() },
	}
}

// Page 处理 GET /diary。未登录 302 到 /manage/login?next=/diary；否则根据
// ?year&month 渲染月视图日历。非法参数按需求 2.1.2 回落到当前月。
//
// 为什么这里重复做 session 检查：/diary 不走 AuthGate 中间件（那是 /manage/* 的
// 专属），但共享同一个 session cookie。直接在 handler 里 ParseSession 最简单。
func (h *Handlers) Page(w http.ResponseWriter, r *http.Request) {
	if _, ok := h.Auth.ParseSession(r); !ok {
		http.Redirect(w, r, "/manage/login?next=/diary", http.StatusSeeOther)
		return
	}

	now := h.Now()
	year, month := NormaliseMonth(atoiOr(r.URL.Query().Get("year"), 0), atoiOr(r.URL.Query().Get("month"), 0), now)

	entries, err := h.Store.DatesIn(year, month)
	if err != nil {
		h.Logger.Error("diary.page.dates_in", slog.String("err", err.Error()))
		// 读不到目录不致命，用空集合继续渲染
		entries = map[int]bool{}
	}

	grid := MonthGrid(year, month, now, entries)

	// 上月 / 下月 URL（简单做法：构造 time.Time 取它的前/后一个月）
	cursor := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	prevCursor := cursor.AddDate(0, -1, 0)
	nextCursor := cursor.AddDate(0, 1, 0)

	data := map[string]any{
		"Year":      year,
		"Month":     int(month),
		"MonthName": month.String(), // "April" 等；模板里可再本地化
		"Grid":      grid,
		"Today":     now.Format("2006-01-02"),
		"PrevURL":   monthURL(prevCursor.Year(), int(prevCursor.Month())),
		"NextURL":   monthURL(nextCursor.Year(), int(nextCursor.Month())),
		"ThisURL":   "/diary",
	}

	if err := h.Tpl.Render(w, r, http.StatusOK, "diary.html", data); err != nil {
		h.Logger.Error("diary.page.render", slog.String("err", err.Error()))
		http.Error(w, "internal error", http.StatusInternalServerError)
	}
}

func monthURL(year, month int) string {
	return "/diary?year=" + strconv.Itoa(year) + "&month=" + strconv.Itoa(month)
}

// atoiOr 把 query 字符串转 int，失败返回 fallback。非法值走 NormaliseMonth 继续
// 回落到当月，符合"非法输入不 400，静默回落"的需求 2.1.2 约定。
func atoiOr(s string, fallback int) int {
	if s == "" {
		return fallback
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return fallback
	}
	return n
}
