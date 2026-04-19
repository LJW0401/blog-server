package diary_test

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/diary"
)

func newStore(t *testing.T) *diary.Store {
	t.Helper()
	s, err := diary.NewStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewStore: %v", err)
	}
	return s
}

// --- Smoke (WI-1.2) --------------------------------------------------------

func TestStore_Smoke_Roundtrip(t *testing.T) {
	s := newStore(t)

	if err := s.Put("2026-04-19", "hello diary"); err != nil {
		t.Fatalf("Put: %v", err)
	}
	body, exists, err := s.Get("2026-04-19")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if !exists {
		t.Fatal("Get should find the entry just written")
	}
	if body != "hello diary" {
		t.Errorf("body = %q, want %q", body, "hello diary")
	}
}

func TestStore_Smoke_DatesIn(t *testing.T) {
	s := newStore(t)
	_ = s.Put("2026-04-05", "early")
	_ = s.Put("2026-04-19", "today")
	_ = s.Put("2026-04-30", "late")
	// 别的月份不应该被计入
	_ = s.Put("2026-03-01", "other month")

	got, err := s.DatesIn(2026, time.April)
	if err != nil {
		t.Fatalf("DatesIn: %v", err)
	}
	want := map[int]bool{5: true, 19: true, 30: true}
	if len(got) != len(want) {
		t.Fatalf("DatesIn size = %d, want %d; got=%v", len(got), len(want), got)
	}
	for d := range want {
		if !got[d] {
			t.Errorf("DatesIn missing day %d", d)
		}
	}
}

func TestStore_Smoke_DeleteMakesGetReturnNotExists(t *testing.T) {
	s := newStore(t)
	_ = s.Put("2026-04-19", "body")
	if err := s.Delete("2026-04-19"); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	_, exists, _ := s.Get("2026-04-19")
	if exists {
		t.Error("after Delete, Get should report exists=false")
	}
}

// --- 异常 / 边界 (WI-1.3) --------------------------------------------------

// 非法输入：路径穿越、非数字、超长、带空字节等都必须被 Validate 拒绝，
// 且拒绝发生在触碰任何文件之前。
func TestStore_Edge_ValidateRejectsMalformed(t *testing.T) {
	s := newStore(t)
	bad := []string{
		"",
		"..",
		"/etc/passwd",
		"../foo",
		"2026/04/19",
		"2026-4-19", // 缺前导 0
		"2026-04-1", // 缺前导 0
		"26-04-19",  // 四位年要求
		"abc-de-fg",
		"2026-13-01",   // 月超范围
		"2026-00-01",   // 月为 0
		"2026-02-29",   // 非闰年 2 月 29
		"2026-04-31",   // 4 月没 31 日
		"2026-04-19\n", // 换行尝试
		"2026-04-19\x00",
	}
	for _, in := range bad {
		if _, err := s.Validate(in); err == nil {
			t.Errorf("Validate(%q) should reject, got nil", in)
		}
		// 即便 Put/Get/Delete 被错误调用，也应由 Validate 拒绝，不触碰文件
		if err := s.Put(in, "nope"); err == nil {
			t.Errorf("Put(%q,...) should reject, got nil", in)
		}
		if _, _, err := s.Get(in); err == nil {
			t.Errorf("Get(%q) should reject, got nil", in)
		}
		if err := s.Delete(in); err == nil {
			t.Errorf("Delete(%q) should reject, got nil", in)
		}
	}
}

// 边界值：闰年合法 2024-02-29；跨月跨年 12-31 / 01-01 都能正常读写。
func TestStore_Edge_BoundaryDatesRoundtrip(t *testing.T) {
	s := newStore(t)
	ok := []string{"2024-02-29", "2026-01-01", "2026-12-31", "2000-02-29", "1900-01-01"}
	for _, d := range ok {
		if err := s.Put(d, "x"); err != nil {
			t.Errorf("Put(%q): %v", d, err)
			continue
		}
		body, exists, err := s.Get(d)
		if err != nil || !exists || body != "x" {
			t.Errorf("Get(%q) = %q, exists=%v, err=%v; want body='x', exists=true", d, body, exists, err)
		}
	}
}

// 失败依赖：权限不足或底层磁盘错误时，Put 应返回 error 而不 panic；
// 用只读目录模拟。
func TestStore_Edge_ReadOnlyRootFailsPut(t *testing.T) {
	dir := t.TempDir()
	s, err := diary.NewStore(dir)
	if err != nil {
		t.Fatal(err)
	}
	// 先 chmod 0500 让写入失败
	diaryDir := filepath.Join(dir, "content", "diary")
	if err := os.Chmod(diaryDir, 0o500); err != nil {
		t.Skipf("cannot chmod in this environment: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(diaryDir, 0o700) })

	err = s.Put("2026-04-19", "should fail")
	if err == nil {
		t.Error("Put on read-only dir should fail")
	}
}

// 异常恢复：Delete 不存在的日期必须幂等；重复 Put 同一天只保留最新内容。
func TestStore_Edge_DeleteIdempotentAndPutOverwrites(t *testing.T) {
	s := newStore(t)

	// 幂等 delete
	if err := s.Delete("2026-04-19"); err != nil {
		t.Errorf("Delete nonexistent should be nil, got %v", err)
	}
	if err := s.Delete("2026-04-19"); err != nil {
		t.Errorf("Delete again should still be nil, got %v", err)
	}

	// 覆盖写
	_ = s.Put("2026-04-19", "v1")
	_ = s.Put("2026-04-19", "v2")
	body, _, _ := s.Get("2026-04-19")
	if body != "v2" {
		t.Errorf("after overwrite, body=%q, want v2", body)
	}
}

// 边界值：Put 空字符串 / 纯空白应等同 Delete（需求 2.3.2）。
func TestStore_Edge_EmptyBodyRemovesFile(t *testing.T) {
	s := newStore(t)
	_ = s.Put("2026-04-19", "hello")
	if err := s.Put("2026-04-19", ""); err != nil {
		t.Fatalf("Put empty: %v", err)
	}
	_, exists, _ := s.Get("2026-04-19")
	if exists {
		t.Error("Put(empty) should remove file")
	}

	_ = s.Put("2026-04-19", "hello")
	if err := s.Put("2026-04-19", "   \n\t  "); err != nil {
		t.Fatalf("Put whitespace: %v", err)
	}
	_, exists, _ = s.Get("2026-04-19")
	if exists {
		t.Error("Put(whitespace-only) should remove file")
	}
}

// DatesIn 对不存在的月份 / 空目录应返回空集且不报错（幂等读）。
func TestStore_Edge_DatesInEmptyMonth(t *testing.T) {
	s := newStore(t)
	got, err := s.DatesIn(2026, time.April)
	if err != nil {
		t.Fatalf("DatesIn: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("empty dir should yield empty set, got %v", got)
	}
}

// DatesIn 不应被同目录下的非法文件名污染（忽略而不报错）。
func TestStore_Edge_DatesInIgnoresGarbage(t *testing.T) {
	dir := t.TempDir()
	s, _ := diary.NewStore(dir)
	_ = s.Put("2026-04-19", "legit")
	// 手动放一些混淆文件
	dd := filepath.Join(dir, "content", "diary")
	_ = os.WriteFile(filepath.Join(dd, "readme.txt"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(dd, "2026-04-19.md.bak"), []byte("x"), 0o600)
	_ = os.WriteFile(filepath.Join(dd, "2026-13-01.md"), []byte("x"), 0o600) // 非法日期
	_ = os.Mkdir(filepath.Join(dd, "subdir"), 0o700)

	got, err := s.DatesIn(2026, time.April)
	if err != nil {
		t.Fatal(err)
	}
	if !got[19] || len(got) != 1 {
		t.Errorf("DatesIn should only pick up legit 04-19; got %v", got)
	}
}

// ErrInvalidDate sentinel 应被调用方 errors.Is 识别，作为稳定错误契约。
func TestStore_Edge_ErrInvalidDateIsExported(t *testing.T) {
	s := newStore(t)
	_, err := s.Validate("2026-13-01")
	if !errors.Is(err, diary.ErrInvalidDate) {
		t.Errorf("expected errors.Is(err, ErrInvalidDate) = true, got err=%v", err)
	}
}

// stripFrontmatter 的边角：没有 frontmatter 的文件被外部手写过，应直接返回 body
// 而不损坏内容。
func TestStore_Edge_GetHandlesFileWithoutFrontmatter(t *testing.T) {
	dir := t.TempDir()
	s, _ := diary.NewStore(dir)
	dd := filepath.Join(dir, "content", "diary")
	// 直接写一个无 frontmatter 的文件，模拟人工写入
	_ = os.WriteFile(filepath.Join(dd, "2026-04-19.md"), []byte("just body"), 0o600)

	body, exists, err := s.Get("2026-04-19")
	if err != nil || !exists {
		t.Fatalf("Get: err=%v exists=%v", err, exists)
	}
	if !strings.Contains(body, "just body") {
		t.Errorf("body should contain 'just body', got %q", body)
	}
}
