// Package datetime 提供业务开发中的时间布局、时区、日历边界与解析工具。
//
// 包内所有函数都以 time.Time 作为唯一的时间表示：日期就是当日零点的
// time.Time，并保留输入时区，不额外定义 date 类型。边界计算始终在输入时间
// 自身的时区内进行；需要转换时区时，调用方先用 t.In(loc) 完成。
//
// 时间戳与单位换算由标准库覆盖（time.Unix、time.UnixMilli、time.UnixMicro），
// 本包不重复提供；解析只有显式的 Parse：布局必须由调用方给出，不做格式猜测。
package datetime

import (
	"fmt"
	"time"
)

// 常用布局常量。Go 的布局语法以参考时间 2006-01-02 15:04:05 为模板，
// 这些常量用于消除散落在业务代码里的字面量。
const (
	// Date 是连字符日期，如 2026-10-21。
	Date = "2006-01-02"
	// DateSlash 是斜杠日期，如 2026/10/21。
	DateSlash = "2006/01/02"
	// DateCN 是中文日期，如 2026年10月21日。
	DateCN = "2006年01月02日"

	// Time 是时分秒，如 15:04:05。
	Time = "15:04:05"
	// TimeMinute 是时分，如 15:04。
	TimeMinute = "15:04"

	// DateTime 是连字符日期时间，如 2026-10-21 15:04:05。
	DateTime = "2006-01-02 15:04:05"
	// DateTimeSlash 是斜杠日期时间，如 2026/10/21 15:04:05。
	DateTimeSlash = "2006/01/02 15:04:05"
	// DateTimeCN 是中文日期时间，如 2026年10月21日 15时04分05秒。
	DateTimeCN = "2006年01月02日 15时04分05秒"
	// DateTimeMillis 是带毫秒的日期时间，如 2026-10-21 15:04:05.000。
	DateTimeMillis = "2006-01-02 15:04:05.000"
	// DateTimeZone 是带时区偏移的日期时间，如 2026-10-21 15:04:05+08:00。
	DateTimeZone = "2006-01-02 15:04:05Z07:00"

	// Month 是年月，如 2026-10。
	Month = "2006-01"
	// CompactDate 是紧凑日期，常用于文件名与对象键，如 20261021。
	CompactDate = "20060102"
	// CompactDateTime 是紧凑日期时间，如 20261021150405。
	CompactDateTime = "20060102150405"
)

// 常用 IANA 时区名。命名以 Zone 为前缀，避免与 cities 等业务标识冲突；
// 常量只保存名字，加载见 Location 与 MustLocation。
const (
	ZoneUTC          = "UTC"
	ZoneShanghai     = "Asia/Shanghai"
	ZoneSingapore    = "Asia/Singapore"
	ZoneTokyo        = "Asia/Tokyo"
	ZoneSeoul        = "Asia/Seoul"
	ZoneHongKong     = "Asia/Hong_Kong"
	ZoneBangkok      = "Asia/Bangkok"
	ZoneDubai        = "Asia/Dubai"
	ZoneKolkata      = "Asia/Kolkata"
	ZoneLondon       = "Europe/London"
	ZoneParis        = "Europe/Paris"
	ZoneBerlin       = "Europe/Berlin"
	ZoneAmsterdam    = "Europe/Amsterdam"
	ZoneMoscow       = "Europe/Moscow"
	ZoneNewYork      = "America/New_York"
	ZoneLosAngeles   = "America/Los_Angeles"
	ZoneChicago      = "America/Chicago"
	ZoneToronto      = "America/Toronto"
	ZoneSaoPaulo     = "America/Sao_Paulo"
	ZoneSydney       = "Australia/Sydney"
	ZoneAuckland     = "Pacific/Auckland"
	ZoneJohannesburg = "Africa/Johannesburg"
)

// Location 加载时区并在进程内缓存。
//
// 时区数据来自运行环境（通常为系统 tzdata），精简镜像可能缺少；需要内置
// 时区数据时可在 main 包 import _ "time/tzdata"。name 为空或为 "UTC" 时
// 返回 time.UTC。
func Location(name string) (*time.Location, error) {
	if cached, ok := locations.Load(name); ok {
		return cached.(*time.Location), nil
	}

	loc, err := time.LoadLocation(name)
	if err != nil {
		return nil, fmt.Errorf("datetime: 加载时区 %q: %w", name, err)
	}

	locations.Store(name, loc)
	return loc, nil
}

// MustLocation 与 Location 相同，但加载失败时 panic。
//
// 适用于时区固定的初始化路径与测试；面向用户输入的代码应使用 Location。
func MustLocation(name string) *time.Location {
	loc, err := Location(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// DayStart 返回 t 所在日的零点，保留 t 的时区。
func DayStart(t time.Time) time.Time {
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, t.Location())
}

// DayRange 返回 t 所在日的左闭右开区间 [当日零点, 次日零点)。
//
// loc 为 nil 时使用 t 自身的时区；不为 nil 时先把 t 换算到 loc 再计算。
// 夏令时切换日该区间的实际时长可能是 23 或 25 小时，但两端恒为该时区的
// 钟面零点。
func DayRange(t time.Time, loc *time.Location) (start, end time.Time) {
	if loc == nil {
		loc = t.Location()
	}

	local := t.In(loc)
	start = time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, loc)
	end = start.AddDate(0, 0, 1)
	return start, end
}

// WeekStart 返回 t 所在周的周一零点，weeks 为正表示之后、为负表示之前。
//
// 周一为一周起点，周日归属其前一个周一。
func WeekStart(t time.Time, weeks int) time.Time {
	start := DayStart(t)
	offset := (int(start.Weekday()) + 6) % 7 // 周一为 0
	return start.AddDate(0, 0, -offset+7*weeks)
}

// MonthStart 返回 t 所在月偏移 months 个月后的首日零点，保留 t 的时区。
//
// months 为正表示之后、为负表示之前，0 表示当月。
func MonthStart(t time.Time, months int) time.Time {
	return time.Date(t.Year(), t.Month()+time.Month(months), 1, 0, 0, 0, 0, t.Location())
}

// MonthEnd 返回 t 所在月偏移 months 个月后的末日零点，保留 t 的时区。
//
// 与 MonthStart 的偏移语义一致；末日不受 t 原本是几号影响，例如 1 月 31 日
// 偏移一个月得到 2 月 28 日（闰年 29 日）。
func MonthEnd(t time.Time, months int) time.Time {
	// 下个月的第 0 天会被 time.Date 归一化为本月最后一天。
	return time.Date(t.Year(), t.Month()+time.Month(months)+1, 0, 0, 0, 0, 0, t.Location())
}

// QuarterStart 返回 t 所在季度偏移 quarters 个季度后的首日零点。
func QuarterStart(t time.Time, quarters int) time.Time {
	return time.Date(t.Year(), quarterFirstMonth(t.Month())+time.Month(quarters*3), 1, 0, 0, 0, 0, t.Location())
}

// QuarterEnd 返回 t 所在季度偏移 quarters 个季度后的末日零点。
func QuarterEnd(t time.Time, quarters int) time.Time {
	// 下一季度首月的前一天即本季度最后一天。
	return time.Date(t.Year(), quarterFirstMonth(t.Month())+time.Month((quarters+1)*3), 0, 0, 0, 0, 0, t.Location())
}

// DaysInMonth 返回 t 所在月的天数。
func DaysInMonth(t time.Time) int {
	return MonthEnd(t, 0).Day()
}

// Days 返回 [start, end) 区间内的逐日零点序列，不含 end 所在日。
//
// 序列按 start 的时区推进；两端时区不同时，end 先换算到 start 的时区再取
// 日期边界。start 不早于 end 时返回空切片。跨夏令时的日期仍各自是当地零点。
func Days(start, end time.Time) []time.Time {
	loc := start.Location()
	cursor := DayStart(start)
	limit := DayStart(end.In(loc))

	var days []time.Time
	for cursor.Before(limit) {
		days = append(days, cursor)
		cursor = cursor.AddDate(0, 0, 1)
	}
	return days
}

// Parse 按 layout 解析时间，loc 为 nil 时使用 time.Local。
//
// 布局必须由调用方显式给出，可复用本包的布局常量；本包不提供格式猜测或
// 多布局回退。解析失败时返回 *time.ParseError，便于调用方区分具体原因。
func Parse(text, layout string, loc *time.Location) (time.Time, error) {
	if loc == nil {
		loc = time.Local
	}
	return time.ParseInLocation(layout, text, loc)
}

// quarterFirstMonth 返回 month 所属季度的首月。
func quarterFirstMonth(month time.Month) time.Month {
	return time.Month((int(month)-1)/3*3 + 1)
}
