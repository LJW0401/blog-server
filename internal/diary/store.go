// Package diary 实现日记功能的文件系统存储和相关辅助。
// 日记条目一对一映射到 $DataDir/content/diary/YYYY-MM-DD.md，每个文件含
// 极简 frontmatter（date + updated_at）和纯文本 body。与 internal/content
// 独立，不进入公共站点的扫描范围 —— 这是 diary-requirements §2.6.3 的
// "公共路由隔离" 硬性约束。
package diary

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

var (
	// ErrInvalidDate 表示日期字符串不满足 YYYY-MM-DD 或者不是真实存在的日历日（比如闰年被拒）。
	ErrInvalidDate = errors.New("diary: invalid date")
)

// dateRe 做第一层正则检查；time.Parse 再做一次语义校验（例如 2025-02-29 会被拒）。
var dateRe = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

// Store 封装 <root>/content/diary/ 下的文件操作。单用户场景，无需锁；
// 多标签页并发写同一日期遵循"后写入胜出"语义（需求 §5 开放问题 2）。
type Store struct {
	dir string
}

// NewStore 返回一个 Store，root 通常是服务的 data_dir；内部自动 mkdir 保证
// 首次运行不因目录缺失崩溃。权限 0700 因为是私密内容。
func NewStore(root string) (*Store, error) {
	dir := filepath.Join(root, "content", "diary")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, fmt.Errorf("diary: mkdir %q: %w", dir, err)
	}
	return &Store{dir: dir}, nil
}

// Validate 对日期字符串做严格校验并返回规范化的 time.Time。
// 校验两层：正则 + time.Parse，组合拒绝路径穿越、非法月日、闰年错误等。
func (s *Store) Validate(date string) (time.Time, error) {
	if !dateRe.MatchString(date) {
		return time.Time{}, fmt.Errorf("%w: %q", ErrInvalidDate, date)
	}
	t, err := time.Parse("2006-01-02", date)
	if err != nil {
		return time.Time{}, fmt.Errorf("%w: %q: %v", ErrInvalidDate, date, err)
	}
	return t, nil
}

// Get 读取某日日记；exists=false 表示该日无记录。返回的 body 不含 frontmatter。
func (s *Store) Get(date string) (body string, exists bool, err error) {
	if _, err := s.Validate(date); err != nil {
		return "", false, err
	}
	raw, rerr := os.ReadFile(s.path(date))
	if errors.Is(rerr, os.ErrNotExist) {
		return "", false, nil
	}
	if rerr != nil {
		return "", false, fmt.Errorf("diary: read %s: %w", date, rerr)
	}
	return stripFrontmatter(string(raw)), true, nil
}

// Put 写入（或覆盖）某日日记。body 会做基本清洗：CRLF→LF、首尾去空白。
// 空 body 不落盘（调用方期望的"清空"语义），转而删除对应文件。
func (s *Store) Put(date, body string) error {
	if _, err := s.Validate(date); err != nil {
		return err
	}
	cleaned := strings.TrimSpace(strings.ReplaceAll(body, "\r\n", "\n"))
	if cleaned == "" {
		// 空内容等同于 Delete（需求 2.3.2：保存空内容不创建空文件）
		return s.Delete(date)
	}
	out := fmt.Sprintf("---\ndate: %s\nupdated_at: %s\n---\n%s\n",
		date,
		time.Now().Format(time.RFC3339),
		cleaned,
	)
	// 保证目录存在后再写（构造时已 MkdirAll，但为防外部 rm -rf 后接着写入也能恢复）
	if err := os.MkdirAll(s.dir, 0o700); err != nil {
		return fmt.Errorf("diary: mkdir: %w", err)
	}
	if err := os.WriteFile(s.path(date), []byte(out), 0o600); err != nil {
		return fmt.Errorf("diary: write %s: %w", date, err)
	}
	return nil
}

// Delete 删除某日日记。幂等：不存在也不报错。
func (s *Store) Delete(date string) error {
	if _, err := s.Validate(date); err != nil {
		return err
	}
	if err := os.Remove(s.path(date)); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("diary: delete %s: %w", date, err)
	}
	return nil
}

// DatesIn 返回给定月份内存在日记的 day 数字集合（1–31），用于日历绿点渲染。
// 不存在的月份返回空集（非 error），与 "没写日记" 对齐。
func (s *Store) DatesIn(year int, month time.Month) (map[int]bool, error) {
	out := map[int]bool{}
	entries, err := os.ReadDir(s.dir)
	if errors.Is(err, os.ErrNotExist) {
		return out, nil
	}
	if err != nil {
		return nil, fmt.Errorf("diary: readdir: %w", err)
	}
	prefix := fmt.Sprintf("%04d-%02d-", year, int(month))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		name := e.Name()
		if !strings.HasPrefix(name, prefix) || !strings.HasSuffix(name, ".md") {
			continue
		}
		// 文件名 "YYYY-MM-DD.md" → 取 day 两位数字
		datePart := strings.TrimSuffix(name, ".md")
		if _, err := s.Validate(datePart); err != nil {
			continue // 不合法文件名直接跳过，不污染集合
		}
		dayStr := datePart[len(prefix):]
		var day int
		if _, err := fmt.Sscanf(dayStr, "%d", &day); err == nil && day >= 1 && day <= 31 {
			out[day] = true
		}
	}
	return out, nil
}

// path 把日期转成完整文件路径。Validate 之后调用即安全。
func (s *Store) path(date string) string {
	return filepath.Join(s.dir, date+".md")
}

// stripFrontmatter 剥掉开头的 `---\n...---\n` 区块，返回 body。如果没有
// frontmatter（例如文件被外部工具手写过）则原文返回。
func stripFrontmatter(raw string) string {
	if !strings.HasPrefix(raw, "---\n") {
		return strings.TrimSpace(raw)
	}
	end := strings.Index(raw[4:], "\n---\n")
	if end < 0 {
		return strings.TrimSpace(raw)
	}
	// 4 (开头 ---\n) + end + 5 (\n---\n)
	body := raw[4+end+5:]
	return strings.TrimSpace(body)
}
