package diary_test

import (
	"testing"
	"time"

	"github.com/penguin/blog-server/internal/diary"
)

func date(y int, m time.Month, d int) time.Time {
	return time.Date(y, m, d, 0, 0, 0, 0, time.UTC)
}

// --- Smoke (WI-1.6) --------------------------------------------------------

// 2026-04 的月视图：第一天 (4-1) 是周三，所以第一行从上月 3-30 开始；
// 4-19 是当前日期（IsToday=true）；entries 里 day=19 → HasEntry=true。
func TestCalendar_Smoke_MonthGridShape(t *testing.T) {
	today := date(2026, time.April, 19)
	entries := map[int]bool{19: true}

	grid := diary.MonthGrid(2026, time.April, today, entries)

	if len(grid) != 6 {
		t.Fatalf("expected 6 rows, got %d", len(grid))
	}
	for i, row := range grid {
		if len(row) != 7 {
			t.Errorf("row %d has %d cols, want 7", i, len(row))
		}
	}

	first := grid[0][0]
	if first.Date.Year() != 2026 || first.Date.Month() != time.March || first.Date.Day() != 30 {
		t.Errorf("first cell = %v, want 2026-03-30", first.Date)
	}
	if first.InMonth {
		t.Error("first cell is March placeholder, InMonth should be false")
	}

	// 找 4-19 的格子
	var apr19 diary.Day
	for _, row := range grid {
		for _, d := range row {
			if d.Date.Year() == 2026 && d.Date.Month() == time.April && d.Date.Day() == 19 {
				apr19 = d
			}
		}
	}
	if !apr19.InMonth || !apr19.HasEntry || !apr19.IsToday {
		t.Errorf("apr19 = %+v; expected InMonth/HasEntry/IsToday all true", apr19)
	}
}

// 周视图：2026-04-19 (周日) 所在的周 = 4-13 ~ 4-19。
func TestCalendar_Smoke_WeekGridIncludesFocus(t *testing.T) {
	today := date(2026, time.April, 19)
	focus := date(2026, time.April, 19)
	entries := map[string]bool{"2026-04-19": true}

	week := diary.WeekGrid(focus, today, entries)
	if len(week) != 7 {
		t.Fatalf("want 7 cells, got %d", len(week))
	}
	// 周一到周日
	wantDates := []string{
		"2026-04-13", "2026-04-14", "2026-04-15", "2026-04-16",
		"2026-04-17", "2026-04-18", "2026-04-19",
	}
	for i, want := range wantDates {
		if got := week[i].Date.Format("2006-01-02"); got != want {
			t.Errorf("cell %d = %s, want %s", i, got, want)
		}
	}
	if !week[6].HasEntry {
		t.Error("2026-04-19 should have entry mark")
	}
	if !week[6].IsToday {
		t.Error("2026-04-19 should be today")
	}
}

// --- 异常 / 边界 (WI-1.7) --------------------------------------------------

// 表格驱动，覆盖跨年（1 月、12 月）、闰年 2 月、非闰年 2 月。
func TestCalendar_Edge_MonthGridBoundaryMonths(t *testing.T) {
	cases := []struct {
		name      string
		year      int
		month     time.Month
		wantFirst string // 第一行首格的 YYYY-MM-DD
	}{
		{"january_2026", 2026, time.January, "2025-12-29"},          // 2026-01-01 是周四
		{"december_2026", 2026, time.December, "2026-11-30"},        // 2026-12-01 是周二
		{"leap_feb_2024", 2024, time.February, "2024-01-29"},        // 2024-02-01 是周四
		{"noleap_feb_2026", 2026, time.February, "2026-01-26"},      // 2026-02-01 是周日
		{"march_2026_sunday_first", 2026, time.March, "2026-02-23"}, // 3-1 是周日，前推到 2-23
	}
	today := date(2026, time.April, 19) // 任选一个非用例内的日期，避免 IsToday 干扰
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			grid := diary.MonthGrid(c.year, c.month, today, nil)
			got := grid[0][0].Date.Format("2006-01-02")
			if got != c.wantFirst {
				t.Errorf("first cell = %s, want %s", got, c.wantFirst)
			}
			// 所有行都 7 列，所有格 InMonth 判断正确
			for _, row := range grid {
				for _, d := range row {
					wantInMonth := d.Date.Year() == c.year && d.Date.Month() == c.month
					if d.InMonth != wantInMonth {
						t.Errorf("%s InMonth = %v, want %v", d.Date.Format("2006-01-02"), d.InMonth, wantInMonth)
					}
				}
			}
		})
	}
}

// 闰年 2-29 能被 entries 正确标记。
func TestCalendar_Edge_LeapFebEntry(t *testing.T) {
	today := date(2024, time.February, 29)
	entries := map[int]bool{29: true}
	grid := diary.MonthGrid(2024, time.February, today, entries)
	var feb29 diary.Day
	for _, row := range grid {
		for _, d := range row {
			if d.InMonth && d.Date.Day() == 29 {
				feb29 = d
			}
		}
	}
	if !feb29.HasEntry || !feb29.IsToday {
		t.Errorf("feb29 = %+v; expected HasEntry=true & IsToday=true", feb29)
	}
}

// 周视图跨月：focus 在月末跨到下月。
func TestCalendar_Edge_WeekGridCrossMonth(t *testing.T) {
	focus := date(2026, time.April, 30) // 周四
	today := date(2026, time.April, 30)
	week := diary.WeekGrid(focus, today, nil)
	// 周一 = 4-27，周日 = 5-3
	if week[0].Date.Format("2006-01-02") != "2026-04-27" {
		t.Errorf("week[0] = %s, want 2026-04-27", week[0].Date.Format("2006-01-02"))
	}
	if week[6].Date.Format("2006-01-02") != "2026-05-03" {
		t.Errorf("week[6] = %s, want 2026-05-03", week[6].Date.Format("2006-01-02"))
	}
}

// NormaliseMonth：非法 year/month 必须回落到 now；合法值透传。
func TestCalendar_Edge_NormaliseMonth(t *testing.T) {
	now := date(2026, time.April, 19)
	cases := []struct {
		inY, inM int
		wantY    int
		wantM    time.Month
	}{
		{2026, 4, 2026, time.April},     // 合法
		{2026, 13, 2026, time.April},    // month 超界
		{2026, 0, 2026, time.April},     // month 为 0
		{2026, -1, 2026, time.April},    // month 负
		{1899, 4, 2026, time.April},     // year 下界
		{2201, 4, 2026, time.April},     // year 上界
		{1900, 1, 1900, time.January},   // 极限合法
		{2200, 12, 2200, time.December}, // 极限合法
	}
	for _, c := range cases {
		y, m := diary.NormaliseMonth(c.inY, c.inM, now)
		if y != c.wantY || m != c.wantM {
			t.Errorf("NormaliseMonth(%d,%d) = (%d,%v), want (%d,%v)",
				c.inY, c.inM, y, m, c.wantY, c.wantM)
		}
	}
}

// Day 的辅助方法契约：ISODate / DayNum
func TestCalendar_Edge_DayHelpers(t *testing.T) {
	d := diary.Day{Date: date(2026, time.April, 19)}
	if d.ISODate() != "2026-04-19" {
		t.Errorf("ISODate = %s", d.ISODate())
	}
	if d.DayNum() != 19 {
		t.Errorf("DayNum = %d", d.DayNum())
	}
}
