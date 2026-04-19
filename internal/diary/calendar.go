package diary

import "time"

// Day 是日历网格一个格子的渲染数据。纯值对象，无行为。
type Day struct {
	Date     time.Time // 格子对应的完整日期
	InMonth  bool      // 是否属于当前查看的月份（否 = 上月/下月占位日）
	HasEntry bool      // 当天有日记文件（日历上画绿点的依据）
	IsToday  bool      // 是否等于当前日期
}

// ISODate 返回 "YYYY-MM-DD"，供模板和 API 对齐 Store 的 key 格式。
func (d Day) ISODate() string { return d.Date.Format("2006-01-02") }

// Day 数字（1–31），避免模板里写 .Date.Day。
func (d Day) DayNum() int { return d.Date.Day() }

// MonthGrid 构造 6 行 × 7 列的日历网格，第一列是周一，最后一列是周日。
// 不足 6 周时会用下个月的日期补满 —— 保证布局高度固定，避免日历高度跳变。
// entries 是该月内"有日记的 day 数字"集合。
func MonthGrid(year int, month time.Month, today time.Time, entries map[int]bool) [][]Day {
	// 本月第一天，计算它在周一起头下的"周内索引"
	first := time.Date(year, month, 1, 0, 0, 0, 0, time.UTC)
	// Weekday: 周日=0..周六=6；换成周一=0..周日=6
	offset := (int(first.Weekday()) + 6) % 7
	start := first.AddDate(0, 0, -offset)

	todayYMD := today.Format("2006-01-02")

	grid := make([][]Day, 6)
	for w := 0; w < 6; w++ {
		row := make([]Day, 7)
		for d := 0; d < 7; d++ {
			date := start.AddDate(0, 0, w*7+d)
			inMonth := date.Year() == year && date.Month() == month
			has := false
			if inMonth {
				has = entries[date.Day()]
			}
			row[d] = Day{
				Date:     date,
				InMonth:  inMonth,
				HasEntry: has,
				IsToday:  date.Format("2006-01-02") == todayYMD,
			}
		}
		grid[w] = row
	}
	return grid
}

// WeekGrid 返回包含 focus 的那一周（周一到周日）。
// entries 这里用 ISO 日期字符串作 key，因为周视图会跨月、day 数字会碰撞。
func WeekGrid(focus, today time.Time, entries map[string]bool) []Day {
	offset := (int(focus.Weekday()) + 6) % 7
	start := focus.AddDate(0, 0, -offset)
	todayYMD := today.Format("2006-01-02")

	row := make([]Day, 7)
	for d := 0; d < 7; d++ {
		date := start.AddDate(0, 0, d)
		ymd := date.Format("2006-01-02")
		row[d] = Day{
			Date: date,
			// 周视图没有"本月"概念，InMonth 统一为 true 表示可点击
			InMonth:  true,
			HasEntry: entries[ymd],
			IsToday:  ymd == todayYMD,
		}
	}
	return row
}

// NormaliseMonth 把任意 (year, month int) 参数收敛到合法范围：
// year ∈ [1900,2200]，month ∈ [1,12]。主要给 handlers 层做 URL 参数回落用。
// 需求 2.1.2 明确非法值应回落到"当前月"；这里提供统一入口。
func NormaliseMonth(year, month int, now time.Time) (int, time.Month) {
	y, m := now.Year(), now.Month()
	if year >= 1900 && year <= 2200 {
		y = year
	}
	if month >= 1 && month <= 12 {
		m = time.Month(month)
	}
	return y, m
}
