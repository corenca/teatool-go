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
//
// 常量本身不会加载时区：把名字传给 Location 拿到 *time.Location，再配合
// t.In(loc) 使用。偏移说明仅为便于阅读，实际以运行环境的 tzdata 为准。
const (
	// ZoneUTC 是协调世界时，无夏令时。
	ZoneUTC = "UTC"
	// ZoneShanghai 是中国标准时间（UTC+8），无夏令时。
	ZoneShanghai = "Asia/Shanghai"
	// ZoneSingapore 是新加坡时间（UTC+8），无夏令时。
	ZoneSingapore = "Asia/Singapore"
	// ZoneTokyo 是日本标准时间（UTC+9），无夏令时。
	ZoneTokyo = "Asia/Tokyo"
	// ZoneSeoul 是韩国标准时间（UTC+9），无夏令时。
	ZoneSeoul = "Asia/Seoul"
	// ZoneHongKong 是香港时间（UTC+8），无夏令时。
	ZoneHongKong = "Asia/Hong_Kong"
	// ZoneBangkok 是泰国时间（UTC+7），无夏令时。
	ZoneBangkok = "Asia/Bangkok"
	// ZoneDubai 是阿联酋时间（UTC+4），无夏令时。
	ZoneDubai = "Asia/Dubai"
	// ZoneKolkata 是印度标准时间（UTC+5:30），无夏令时。
	ZoneKolkata = "Asia/Kolkata"
	// ZoneLondon 是英国时间，冬令时 UTC+0、夏令时 UTC+1。
	ZoneLondon = "Europe/London"
	// ZoneParis 是中欧时间，冬令时 UTC+1、夏令时 UTC+2。
	ZoneParis = "Europe/Paris"
	// ZoneBerlin 是中欧时间，冬令时 UTC+1、夏令时 UTC+2。
	ZoneBerlin = "Europe/Berlin"
	// ZoneAmsterdam 是中欧时间，冬令时 UTC+1、夏令时 UTC+2。
	ZoneAmsterdam = "Europe/Amsterdam"
	// ZoneMoscow 是莫斯科时间（UTC+3），2014 年后不再使用夏令时。
	ZoneMoscow = "Europe/Moscow"
	// ZoneNewYork 是美国东部时间，冬令时 UTC-5、夏令时 UTC-4。
	ZoneNewYork = "America/New_York"
	// ZoneLosAngeles 是美国太平洋时间，冬令时 UTC-8、夏令时 UTC-7。
	ZoneLosAngeles = "America/Los_Angeles"
	// ZoneChicago 是美国中部时间，冬令时 UTC-6、夏令时 UTC-5。
	ZoneChicago = "America/Chicago"
	// ZoneToronto 是加拿大东部时间，冬令时 UTC-5、夏令时 UTC-4。
	ZoneToronto = "America/Toronto"
	// ZoneSaoPaulo 是巴西利亚时间（UTC-3），2019 年后不再使用夏令时。
	ZoneSaoPaulo = "America/Sao_Paulo"
	// ZoneSydney 是澳大利亚东部时间，冬令时 UTC+10、夏令时 UTC+11。
	ZoneSydney = "Australia/Sydney"
	// ZoneAuckland 是新西兰时间，冬令时 UTC+12、夏令时 UTC+13。
	ZoneAuckland = "Pacific/Auckland"
	// ZoneJohannesburg 是南非标准时间（UTC+2），无夏令时。
	ZoneJohannesburg = "Africa/Johannesburg"
)

// Location 加载时区并在进程内缓存。
//
// 参数 name 是 IANA 时区名，可直接用 Zone* 常量；为空或为 "UTC" 时返回
// time.UTC。返回的 *time.Location 可直接传给 t.In(loc) 或本包其他函数。
//
// 时区数据来自运行环境（通常为系统 tzdata），精简镜像可能缺少；需要内置
// 时区数据时可在 main 包 import _ "time/tzdata"。加载失败（名称非法、tzdata
// 缺失）时返回包装后的错误，可用 errors.Is 对比 *time.UnknownTimeZoneError。
//
// 成功加载的结果存入进程级缓存，后续调用不再读取磁盘；本函数可并发调用。
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
// 参数 name 是 IANA 时区名，语义同 Location。适用于时区固定的初始化路径与
// 测试：写死 ".Asia/Shanghai" 时宁可启动就崩溃，也不要带错时区跑下去。
// 面向用户输入的代码应使用 Location，把错误交回调用方。
func MustLocation(name string) *time.Location {
	loc, err := Location(name)
	if err != nil {
		panic(err)
	}
	return loc
}

// DayStart 返回 t 所在日的零点，保留 t 的时区。
//
// 参数 t 可以是任意时刻。返回值是用 t 的时区重新构造的当日 00:00:00，纳秒
// 截断；夏令时切换日同样是钟面零点，而不是"当天第一时刻"。
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
// 参数 t 是基准时刻，weeks 是周偏移量（0 表示本周）。周一为一周起点，周日
// 归属其前一个周一。返回值保留 t 的时区。
func WeekStart(t time.Time, weeks int) time.Time {
	start := DayStart(t)
	offset := (int(start.Weekday()) + 6) % 7 // 周一为 0
	return start.AddDate(0, 0, -offset+7*weeks)
}

// MonthStart 返回 t 所在月偏移 months 个月后的首日零点，保留 t 的时区。
//
// 参数 t 是基准时刻，months 是月偏移量（可为负），0 表示当月。月份溢出由
// time.Date 自动进位到年份，例如 2026-12 偏移 1 个月得到 2027-01-01。
func MonthStart(t time.Time, months int) time.Time {
	return time.Date(t.Year(), t.Month()+time.Month(months), 1, 0, 0, 0, 0, t.Location())
}

// MonthEnd 返回 t 所在月偏移 months 个月后的末日零点，保留 t 的时区。
//
// 参数 t 是基准时刻，months 是月偏移量（可为负），0 表示当月。返回值是该月
// 最后一天的 00:00，不是次月首日，便于用 .Day() 取天数。与 MonthStart 的偏移
// 语义一致；末日不受 t 原本是几号影响，例如 1 月 31 日偏移一个月得到 2 月 28 日
// （闰年 29 日）。
func MonthEnd(t time.Time, months int) time.Time {
	// 下个月的第 0 天会被 time.Date 归一化为本月最后一天。
	return time.Date(t.Year(), t.Month()+time.Month(months)+1, 0, 0, 0, 0, 0, t.Location())
}

// QuarterStart 返回 t 所在季度偏移 quarters 个季度后的首日零点。
//
// 参数 t 是基准时刻，quarters 是季度偏移量（可为负），0 表示当季；季度以自然
// 季度划分（1 月、4 月、7 月、10 月开始）。返回值保留 t 的时区。
func QuarterStart(t time.Time, quarters int) time.Time {
	return time.Date(t.Year(), quarterFirstMonth(t.Month())+time.Month(quarters*3), 1, 0, 0, 0, 0, t.Location())
}

// QuarterEnd 返回 t 所在季度偏移 quarters 个季度后的末日零点。
//
// 参数 t 是基准时刻，quarters 是季度偏移量（可为负），0 表示当季。返回值是该
// 季度最后一天的 00:00，保留 t 的时区；用它再取 [QuarterStart, QuarterEnd+1天)
// 即得季度区间。
func QuarterEnd(t time.Time, quarters int) time.Time {
	// 下一季度首月的前一天即本季度最后一天。
	return time.Date(t.Year(), quarterFirstMonth(t.Month())+time.Month((quarters+1)*3), 0, 0, 0, 0, 0, t.Location())
}

// DaysInMonth 返回 t 所在月的天数。
//
// 参数 t 是基准时刻，只用到它的年、月、时区；返回 28~31，闰年二月返回 29。
func DaysInMonth(t time.Time) int {
	return MonthEnd(t, 0).Day()
}

// Days 返回 [start, end) 区间内的逐日零点序列，不含 end 所在日。
//
// 参数 start、end 是区间端点（左闭右开），返回值为该区间的每一天的当地零点。
// 序列按 start 的时区推进；两端时区不同时，end 先换算到 start 的时区再取
// 日期边界。start 不早于 end 时返回 nil。跨夏令时的日期仍各自是当地零点，
// 因此相邻元素的间隔可能是 23 或 25 小时。
//
// 适合补全报表的空缺日期、生成日历：区间长度按天数而非小时数计算。
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
// 参数 text 是待解析文本，layout 是 Go 布局串（可复用本包的布局常量），loc 是
// 结果时区。解析只接受调用方给定的布局，不做格式猜测或多布局回退。
//
// 返回解析出的 time.Time；文本与布局不匹配时返回 *time.ParseError，可用
// errors.As 取出具体字段，便于定位是哪个片段出错。
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
