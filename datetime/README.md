# datetime 使用说明

业务开发中的时间布局、时区、日历边界与显式解析。所有函数都只用 `time.Time`，
日期就是当日零点的 `time.Time`，并保留输入时区——不额外定义 date 类型。

适用场景：

- 报表按日、周、月、季度取区间，生成"空缺日期"补零序列。
- 统一时间字符串格式，避免各业务代码各写一份 `"2006-01-02"`。
- 从配置或接口文本按**已知**布局解析时间，并指定结果时区。

不做的事情：

- 不封装时间戳换算，直接用 `time.Unix` / `time.UnixMilli` / `time.UnixMicro`。
- 不做格式猜测或多布局回退，`Parse` 只接受调用方给出的布局。
- 不解析时区缩写（`CST`、`EST` 有歧义），请用 IANA 时区名。
- 不做 cron、时长、工作日历、农历。

```bash
go get github.com/corenca/teatool-go
```

## 快速上手

```go
package main

import (
	"fmt"
	"time"

	"github.com/corenca/teatool-go/datetime"
)

func main() {
	sh, err := datetime.Location(datetime.ZoneShanghai)
	if err != nil {
		return // 名称非法或 tzdata 缺失
	}

	now := time.Date(2026, 10, 21, 15, 4, 5, 0, sh)

	fmt.Println(datetime.DayStart(now).Format(datetime.DateTime))   // 2026-10-21 00:00:00
	fmt.Println(datetime.WeekStart(now, 0).Format(datetime.Date))   // 2026-10-19（周一）
	fmt.Println(datetime.MonthStart(now, 0).Format(datetime.Date))  // 2026-10-01
	fmt.Println(datetime.MonthEnd(now, 0).Format(datetime.Date))    // 2026-10-31
	fmt.Println(datetime.QuarterStart(now, 0).Format(datetime.Date)) // 2026-10-01
	fmt.Println(datetime.QuarterEnd(now, 0).Format(datetime.Date))   // 2026-12-31

	start, end := datetime.DayRange(now, nil)
	fmt.Println(start.Format(time.RFC3339), end.Format(time.RFC3339))
	// 2026-10-21T00:00:00+08:00 2026-10-22T00:00:00+08:00
}
```

## 布局常量

| 常量 | 布局 | 示例 |
| --- | --- | --- |
| `Date` | `2006-01-02` | `2026-10-21` |
| `DateSlash` | `2006/01/02` | `2026/10/21` |
| `DateCN` | `2006年01月02日` | `2026年10月21日` |
| `Time` | `15:04:05` | `15:04:05` |
| `TimeMinute` | `15:04` | `15:04` |
| `DateTime` | `2006-01-02 15:04:05` | `2026-10-21 15:04:05` |
| `DateTimeSlash` | `2006/01/02 15:04:05` | `2026/10/21 15:04:05` |
| `DateTimeCN` | `2006年01月02日 15时04分05秒` | `2026年10月21日 15时04分05秒` |
| `DateTimeMillis` | `2006-01-02 15:04:05.000` | `2026-10-21 15:04:05.000` |
| `DateTimeZone` | `2006-01-02 15:04:05Z07:00` | `2026-10-21 15:04:05+08:00` |
| `Month` | `2006-01` | `2026-10` |
| `CompactDate` | `20060102` | `20261021` |
| `CompactDateTime` | `20060102150405` | `20261021150405` |

布局串是 Go 的"参考时间"语法，直接用于 `t.Format` 与 `datetime.Parse`。`CompactDate`
与 `CompactDateTime` 适合拼进文件名和对象键：无分隔符，字典序即时间序。

## 时区

`Zone*` 常量只保存 IANA 时区名，要用 `Location` 加载成 `*time.Location`：

```go
sh := datetime.MustLocation(datetime.ZoneShanghai) // 写死时区、失败即 panic
loc, err := datetime.Location(datetime.ZoneTokyo)  // 配置/用户输入走这个
```

内置常量覆盖 UTC、亚太、中东、欧洲、美洲主要城市（见 `go doc datetime`）。
`Location` 会缓存成功结果，可并发调用；名称为空或 `"UTC"` 时返回 `time.UTC`。

时区数据来自运行环境的 tzdata，精简 Docker 镜像（如 `scratch`、`alpine` 未装
`tzdata`）会加载失败。需要内置数据时在 `main` 包加一行：

```go
import _ "time/tzdata"
```

## 边界函数

| 函数 | 参数 | 返回 |
| --- | --- | --- |
| `DayStart(t)` | 任意时刻 | 当日零点，保留 `t` 的时区 |
| `DayRange(t, loc)` | `loc` 为 nil 时用 `t` 的时区 | 左闭右开 `[当日零点, 次日零点)` |
| `WeekStart(t, weeks)` | `weeks` 周偏移，可负 | 周一零点；周日归属前一个周一 |
| `MonthStart(t, months)` | `months` 月偏移，可负 | 当（偏移后）月首日零点 |
| `MonthEnd(t, months)` | 同上 | 当（偏移后）月最后一天零点 |
| `QuarterStart(t, quarters)` | `quarters` 季度偏移，可负 | 当（偏移后）季度首日零点 |
| `QuarterEnd(t, quarters)` | 同上 | 当（偏移后）季度最后一天零点 |
| `DaysInMonth(t)` | 任意时刻 | 28~31，闰年二月 29 |

约定：

- **边界都在输入时区内计算**，不做隐式 UTC 转换。跨时区时先 `t.In(loc)` 再调用。
- 所有"末日"函数返回的是当天零点，不是次日零点。要拿区间右端请自己 `+1 天`，
  或用 `DayRange` 这类已经左闭右开的函数。
- 偏移量由 `time.Date` 自动进位，如 2026-12 偏移 1 个月得到 2027-01。
- 月末语义不受原日期影响：1 月 31 日偏移一个月得到 2 月 28 日（闰年 29 日）。
- 夏令时切换日的 `DayRange` 实际时长可能是 23 或 25 小时，但两端都是当地钟面零点。

## 逐日序列

```go
start := time.Date(2026, 10, 28, 0, 0, 0, 0, sh)
end := time.Date(2026, 11, 2, 0, 0, 0, 0, sh)

for _, day := range datetime.Days(start, end) {
	fmt.Println(day.Format(datetime.Date), day.Weekday())
}
// 2026-10-28 Wednesday
// 2026-10-29 Thursday
// 2026-10-30 Friday
// 2026-10-31 Saturday
// 2026-11-01 Sunday
```

`Days(start, end)` 返回 `[start, end)` 内每天的当地零点，**不含 end 所在日**；`start`
不早于 `end` 时返回 nil。序列按 `start` 的时区推进，`end` 会先换算到该时区再取日期
边界。报表补零的典型写法：

```go
counts := map[string]int64{} // 来自数据库的按日统计，键是 yyyy-mm-dd
for _, day := range datetime.Days(monthStart, monthStart.AddDate(0, 0, 32)) {
	key := day.Format(datetime.Date)
	if _, ok := counts[key]; !ok {
		counts[key] = 0
	}
}
```

## 解析

```go
sh := datetime.MustLocation(datetime.ZoneShanghai)

t, err := datetime.Parse("2026-10-21 15:04:05", datetime.DateTime, sh)
// 2026-10-21T15:04:05+08:00

_, err = datetime.Parse("2026/10/21", datetime.DateTime, sh)
// parsing time "2026/10/21" as "2006-01-02 15:04:05": cannot parse "/10/21" as "-"
```

- `text` 是待解析文本，`layout` 是布局（可直接用上面的常量），`loc` 为 nil 时用
  `time.Local`。
- 返回 `*time.ParseError`，可用 `errors.As` 取出具体字段定位偏差。
- 不做格式猜测：同一份数据有多种可能格式时，由业务代码显式选择布局，不要指望
  自动识别。

## 常见错误

**"日期不对，差了一天"**：`DayStart(t)` 保留 `t` 的时区。服务端拿到 UTC 时间后
直接调用得到的是 UTC 零点，不是北京时间的零点。先 `t.In(sh)` 再算边界。

**把 `MonthEnd` 当区间右端**：它返回当月最后一天的零点，右开区间要用
`MonthEnd(...).AddDate(0, 0, 1)`，或直接用 `DayRange` / `Days` 这类半开区间函数。

**夏令时按 24 小时推算**：纽约、伦敦等时区切换日的一天是 23 或 25 小时。用
`DayRange` / `Days` / `AddDate` 按日历推进，不要用 `Add(24 * time.Hour)`。

**`Location` 报 unknown time zone**：运行环境缺少 tzdata，见上文 import `time/tzdata`；
或名称写错（`"Asia/Beijing"` 不存在，中国时区是 `"Asia/Shanghai"`）。

**`Parse` 结果时区是 UTC**：`loc` 传了 nil 而机器时区是 UTC，或布局里带了时区
偏移（`DateTimeZone` 会以文本中的偏移为准）。需要固定时区就显式传 `loc`。

**`"CST"` 解析出 8 小时偏差**：`CST` 同时表示中国、美国中部等多个时区。布局里
不要用时区缩写，用 `Z07:00` 这种偏移形式或直接指定 `Location`。

**字符串直接比较时间**：`Date` 布局可以用字典序比较，带时间与时区的字符串不行。
比较大小统一转成 `time.Time` 用 `Before` / `After`。
