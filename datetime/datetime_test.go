package datetime_test

import (
	"testing"
	"time"

	"github.com/corenca/teatool-go/datetime"
)

// shanghai 返回上海时区；缺少 tzdata 的环境跳过依赖时区的用例。
func shanghai(t *testing.T) *time.Location {
	t.Helper()

	loc, err := datetime.Location(datetime.ZoneShanghai)
	if err != nil {
		t.Skipf("缺少时区数据，跳过: %v", err)
	}
	return loc
}

func newYork(t *testing.T) *time.Location {
	t.Helper()

	loc, err := datetime.Location(datetime.ZoneNewYork)
	if err != nil {
		t.Skipf("缺少时区数据，跳过: %v", err)
	}
	return loc
}

func TestLayouts(t *testing.T) {
	loc := shanghai(t)
	value := time.Date(2026, 10, 21, 15, 4, 5, 0, loc)

	tests := []struct {
		name   string
		layout string
		want   string
	}{
		{name: "Date", layout: datetime.Date, want: "2026-10-21"},
		{name: "DateSlash", layout: datetime.DateSlash, want: "2026/10/21"},
		{name: "DateCN", layout: datetime.DateCN, want: "2026年10月21日"},
		{name: "Time", layout: datetime.Time, want: "15:04:05"},
		{name: "TimeMinute", layout: datetime.TimeMinute, want: "15:04"},
		{name: "DateTime", layout: datetime.DateTime, want: "2026-10-21 15:04:05"},
		{name: "DateTimeSlash", layout: datetime.DateTimeSlash, want: "2026/10/21 15:04:05"},
		{name: "DateTimeCN", layout: datetime.DateTimeCN, want: "2026年10月21日 15时04分05秒"},
		{name: "DateTimeMillis", layout: datetime.DateTimeMillis, want: "2026-10-21 15:04:05.000"},
		{name: "DateTimeZone", layout: datetime.DateTimeZone, want: "2026-10-21 15:04:05+08:00"},
		{name: "Month", layout: datetime.Month, want: "2026-10"},
		{name: "CompactDate", layout: datetime.CompactDate, want: "20261021"},
		{name: "CompactDateTime", layout: datetime.CompactDateTime, want: "20261021150405"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := value.Format(tt.layout); got != tt.want {
				t.Fatalf("Format(%q) = %q, 期望 %q", tt.layout, got, tt.want)
			}
		})
	}
}

func TestMonthStart(t *testing.T) {
	loc := shanghai(t)
	base := time.Date(2026, 10, 21, 15, 4, 5, 0, loc)

	tests := []struct {
		name   string
		base   time.Time
		months int
		want   time.Time
	}{
		{name: "当月", base: base, months: 0, want: time.Date(2026, 10, 1, 0, 0, 0, 0, loc)},
		{name: "下月", base: base, months: 1, want: time.Date(2026, 11, 1, 0, 0, 0, 0, loc)},
		{name: "跨年往前", base: base, months: -10, want: time.Date(2025, 12, 1, 0, 0, 0, 0, loc)},
		{name: "跨年往后", base: time.Date(2026, 12, 31, 23, 59, 0, 0, loc), months: 1, want: time.Date(2027, 1, 1, 0, 0, 0, 0, loc)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := datetime.MonthStart(tt.base, tt.months)
			if !got.Equal(tt.want) {
				t.Fatalf("MonthStart = %v, 期望 %v", got, tt.want)
			}
			if got.Location() != loc {
				t.Fatalf("时区被改变: %v", got.Location())
			}
		})
	}
}

func TestMonthEnd(t *testing.T) {
	loc := shanghai(t)

	tests := []struct {
		name   string
		base   time.Time
		months int
		want   time.Time
	}{
		{name: "当月", base: time.Date(2026, 1, 31, 10, 30, 0, 0, loc), months: 0, want: time.Date(2026, 1, 31, 0, 0, 0, 0, loc)},
		{name: "跨到平年二月", base: time.Date(2026, 1, 31, 10, 30, 0, 0, loc), months: 1, want: time.Date(2026, 2, 28, 0, 0, 0, 0, loc)},
		{name: "跨到闰年二月", base: time.Date(2024, 1, 15, 0, 0, 0, 0, loc), months: 1, want: time.Date(2024, 2, 29, 0, 0, 0, 0, loc)},
		{name: "跨年", base: time.Date(2026, 12, 5, 0, 0, 0, 0, loc), months: 1, want: time.Date(2027, 1, 31, 0, 0, 0, 0, loc)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := datetime.MonthEnd(tt.base, tt.months); !got.Equal(tt.want) {
				t.Fatalf("MonthEnd = %v, 期望 %v", got, tt.want)
			}
		})
	}
}

func TestDaysInMonth(t *testing.T) {
	tests := []struct {
		name string
		base time.Time
		want int
	}{
		{name: "闰年二月", base: time.Date(2024, 2, 10, 0, 0, 0, 0, time.UTC), want: 29},
		{name: "平年二月", base: time.Date(2026, 2, 10, 0, 0, 0, 0, time.UTC), want: 28},
		{name: "四月", base: time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC), want: 30},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := datetime.DaysInMonth(tt.base); got != tt.want {
				t.Fatalf("DaysInMonth = %d, 期望 %d", got, tt.want)
			}
		})
	}
}

func TestQuarter(t *testing.T) {
	loc := shanghai(t)
	base := time.Date(2026, 5, 17, 9, 0, 0, 0, loc)

	tests := []struct {
		name string
		got  time.Time
		want time.Time
	}{
		{name: "本季开始", got: datetime.QuarterStart(base, 0), want: time.Date(2026, 4, 1, 0, 0, 0, 0, loc)},
		{name: "本季结束", got: datetime.QuarterEnd(base, 0), want: time.Date(2026, 6, 30, 0, 0, 0, 0, loc)},
		{name: "下季开始", got: datetime.QuarterStart(base, 1), want: time.Date(2026, 7, 1, 0, 0, 0, 0, loc)},
		{name: "上季结束", got: datetime.QuarterEnd(base, -1), want: time.Date(2026, 3, 31, 0, 0, 0, 0, loc)},
		{name: "跨年下季", got: datetime.QuarterStart(time.Date(2026, 11, 1, 0, 0, 0, 0, loc), 1), want: time.Date(2027, 1, 1, 0, 0, 0, 0, loc)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if !tt.got.Equal(tt.want) {
				t.Fatalf("= %v, 期望 %v", tt.got, tt.want)
			}
		})
	}
}

func TestWeekStart(t *testing.T) {
	loc := shanghai(t)
	// 2026-10-21 是星期三，所在周的周一是 10-19。
	base := time.Date(2026, 10, 21, 15, 4, 5, 0, loc)

	tests := []struct {
		name  string
		weeks int
		want  time.Time
	}{
		{name: "本周", weeks: 0, want: time.Date(2026, 10, 19, 0, 0, 0, 0, loc)},
		{name: "上周", weeks: -1, want: time.Date(2026, 10, 12, 0, 0, 0, 0, loc)},
		{name: "下周", weeks: 1, want: time.Date(2026, 10, 26, 0, 0, 0, 0, loc)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := datetime.WeekStart(base, tt.weeks); !got.Equal(tt.want) {
				t.Fatalf("WeekStart = %v, 期望 %v", got, tt.want)
			}
		})
	}

	// 周日应归属前一天开始的那一周。
	sunday := time.Date(2026, 10, 25, 8, 0, 0, 0, loc)
	if got, want := datetime.WeekStart(sunday, 0), time.Date(2026, 10, 19, 0, 0, 0, 0, loc); !got.Equal(want) {
		t.Fatalf("周日的周起点 = %v, 期望 %v", got, want)
	}
}

func TestDayStart(t *testing.T) {
	loc := shanghai(t)
	base := time.Date(2026, 10, 21, 23, 59, 59, 999, loc)

	want := time.Date(2026, 10, 21, 0, 0, 0, 0, loc)
	if got := datetime.DayStart(base); !got.Equal(want) {
		t.Fatalf("DayStart = %v, 期望 %v", got, want)
	}
}

func TestDayRange(t *testing.T) {
	loc := shanghai(t)
	base := time.Date(2026, 10, 21, 15, 4, 5, 0, loc)

	start, end := datetime.DayRange(base, nil)
	if want := time.Date(2026, 10, 21, 0, 0, 0, 0, loc); !start.Equal(want) {
		t.Fatalf("start = %v, 期望 %v", start, want)
	}
	if want := time.Date(2026, 10, 22, 0, 0, 0, 0, loc); !end.Equal(want) {
		t.Fatalf("end = %v, 期望 %v", end, want)
	}
	if got := end.Sub(start); got != 24*time.Hour {
		t.Fatalf("普通日区间时长 = %v, 期望 24h", got)
	}
}

func TestDayRangeAcrossDST(t *testing.T) {
	loc := newYork(t)

	// 2026-03-08 美东进入夏令时，当天只有 23 小时。
	springStart, springEnd := datetime.DayRange(time.Date(2026, 3, 8, 12, 0, 0, 0, loc), loc)
	if got := springEnd.Sub(springStart); got != 23*time.Hour {
		t.Fatalf("春季切换日区间 = %v, 期望 23h", got)
	}

	// 2026-11-01 美东退出夏令时，当天有 25 小时。
	fallStart, fallEnd := datetime.DayRange(time.Date(2026, 11, 1, 12, 0, 0, 0, loc), loc)
	if got := fallEnd.Sub(fallStart); got != 25*time.Hour {
		t.Fatalf("秋季切换日区间 = %v, 期望 25h", got)
	}
}

func TestDays(t *testing.T) {
	loc := shanghai(t)

	days := datetime.Days(
		time.Date(2026, 10, 1, 15, 0, 0, 0, loc),
		time.Date(2026, 10, 4, 9, 0, 0, 0, loc),
	)
	if len(days) != 3 {
		t.Fatalf("逐日长度 = %d, 期望 3", len(days))
	}
	for i, day := range days {
		want := time.Date(2026, 10, 1+i, 0, 0, 0, 0, loc)
		if !day.Equal(want) {
			t.Fatalf("第 %d 天 = %v, 期望 %v", i, day, want)
		}
	}

	if got := len(datetime.Days(days[0], days[0])); got != 0 {
		t.Fatalf("空区间应返回空切片, 得到 %d", got)
	}
	if got := len(datetime.Days(days[2], days[0])); got != 0 {
		t.Fatalf("反向区间应返回空切片, 得到 %d", got)
	}
}

func TestParse(t *testing.T) {
	loc := shanghai(t)

	tests := []struct {
		name   string
		text   string
		layout string
		want   time.Time
	}{
		{name: "Date", text: "2026-10-21", layout: datetime.Date, want: time.Date(2026, 10, 21, 0, 0, 0, 0, loc)},
		{name: "DateSlash", text: "2026/10/21", layout: datetime.DateSlash, want: time.Date(2026, 10, 21, 0, 0, 0, 0, loc)},
		{name: "DateCN", text: "2026年10月21日", layout: datetime.DateCN, want: time.Date(2026, 10, 21, 0, 0, 0, 0, loc)},
		{name: "DateTime", text: "2026-10-21 15:04:05", layout: datetime.DateTime, want: time.Date(2026, 10, 21, 15, 4, 5, 0, loc)},
		{name: "DateTimeMillis", text: "2026-10-21 15:04:05.123", layout: datetime.DateTimeMillis, want: time.Date(2026, 10, 21, 15, 4, 5, 123e6, loc)},
		{name: "DateTimeZone", text: "2026-10-21 15:04:05+08:00", layout: datetime.DateTimeZone, want: time.Date(2026, 10, 21, 15, 4, 5, 0, loc)},
		{name: "Month", text: "2026-10", layout: datetime.Month, want: time.Date(2026, 10, 1, 0, 0, 0, 0, loc)},
		{name: "CompactDate", text: "20261021", layout: datetime.CompactDate, want: time.Date(2026, 10, 21, 0, 0, 0, 0, loc)},
		{name: "CompactDateTime", text: "20261021150405", layout: datetime.CompactDateTime, want: time.Date(2026, 10, 21, 15, 4, 5, 0, loc)},
		{name: "RFC3339", text: "2026-10-21T15:04:05+08:00", layout: time.RFC3339, want: time.Date(2026, 10, 21, 15, 4, 5, 0, loc)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := datetime.Parse(tt.text, tt.layout, loc)
			if err != nil {
				t.Fatalf("Parse(%q, %q) 失败: %v", tt.text, tt.layout, err)
			}
			if !got.Equal(tt.want) {
				t.Fatalf("Parse(%q, %q) = %v, 期望 %v", tt.text, tt.layout, got, tt.want)
			}
		})
	}

	if _, err := datetime.Parse("不是时间", datetime.DateTime, loc); err == nil {
		t.Fatal("格式不匹配时应返回错误")
	}

	// 布局必须显式匹配：日期串用日期时间布局解析应失败。
	if _, err := datetime.Parse("2026-10-21", datetime.DateTime, loc); err == nil {
		t.Fatal("布局不匹配时应返回错误")
	}

	// loc 为 nil 时按本地时区解释。
	local, err := datetime.Parse("2026-10-21", datetime.Date, nil)
	if err != nil {
		t.Fatalf("Parse 失败: %v", err)
	}
	if local.Location() != time.Local {
		t.Fatalf("nil 时区应使用 time.Local, 得到 %v", local.Location())
	}
}

func TestLocation(t *testing.T) {
	first, err := datetime.Location(datetime.ZoneShanghai)
	if err != nil {
		t.Skipf("缺少时区数据，跳过: %v", err)
	}

	second, err := datetime.Location(datetime.ZoneShanghai)
	if err != nil {
		t.Fatalf("Location 失败: %v", err)
	}
	if first != second {
		t.Fatal("Location 应复用缓存的时区对象")
	}

	if _, err := datetime.Location("Nowhere/Fake"); err == nil {
		t.Fatal("非法时区应返回错误")
	}
}

func TestMustLocation(t *testing.T) {
	if loc := datetime.MustLocation(datetime.ZoneUTC); loc != time.UTC {
		t.Fatalf("MustLocation(UTC) = %v, 期望 time.UTC", loc)
	}

	defer func() {
		if recover() == nil {
			t.Fatal("非法时区应 panic")
		}
	}()
	datetime.MustLocation("Nowhere/Fake")
}
