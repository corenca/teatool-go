# excel

面向业务的 xlsx 读写：声明要哪些列，包负责取值、校验、规范化；写出时按声明
顺序生成表头与数据行。基于 [excelize v2](https://github.com/xuri/excelize)，支持
xlsx / xlsm / xltx / xltm，不支持旧版 `.xls`、csv 与加密工作簿。

## 安装

```bash
go get github.com/corenca/teatool-go/excel
```

## 快速开始

### 有表头读取

```go
package main

import (
	"fmt"
	"log"
	"os"

	"github.com/corenca/teatool-go/excel"
)

func main() {
	f, err := os.Open("orders.xlsx")
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	rows, err := excel.Read(f, excel.ReadOptions{
		Columns: []excel.Column{
			{Header: "订单号", Key: "order_no"},
			{Header: "金额", Key: "amount", Type: excel.Float},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	for _, row := range rows {
		fmt.Println(row["order_no"], row["amount"])
	}
}
```

文件里的其他列会被忽略；声明的列在表头中不存在时返回 `ErrMissingColumn`，错误
信息会一次列出所有缺失列。

### 无表头读取

无表头时 `Columns` 按位置对应 A、B、C…，`Header` 被忽略，**声明列数必须等于
文件实际列宽**（所有数据行中最宽一行的有效单元格数）：

```go
rows, err := excel.Read(f, excel.ReadOptions{
	NoHeader: true,
	Columns: []excel.Column{
		{Key: "order_no"},
		{Key: "amount", Type: excel.Float},
	},
})
```

文件更宽或更窄都会返回 `ErrColumnMismatch`。

### 写出

```go
out, err := os.Create("export.xlsx")
if err != nil {
	log.Fatal(err)
}
defer out.Close()

err = excel.Write(out, []map[string]string{
	{"order_no": "A001", "amount": "12.5"},
	{"order_no": "A002", "amount": ""},
}, excel.WriteOptions{
	Columns: []excel.Column{
		{Header: "订单号", Key: "order_no", Width: 20},
		{Header: "金额", Key: "amount", Width: 12},
	},
	HeaderBold:   true,
	FreezeHeader: true,
})
```

每一行必须包含全部声明的 `Key`，缺一个就返回 `*RowError`（`ErrMissingKey`），
且不会写出文件；多余键忽略；空字符串是合法值。以 `=` 开头的值按文本写入。

需要写文件时用 `WriteFile`：它先写同目录临时文件再重命名，失败不会留下半截
文件。

### 结构体读写

```go
type Order struct {
	No     string     `excel:"订单号"`
	Amount float64    `excel:"金额"`
	Date   time.Time  `excel:"日期,layout:2006-01-02"`
	Note   *string    `excel:"备注"`
	Skip   string     `excel:"-"`
}

// 读：列由字段与 tag 决定，不接受 opts.Columns
orders, err := excel.ReadInto[Order](f, excel.ReadOptions{})

// 写：字段声明顺序就是列顺序
err = excel.WriteFrom(out, orders, excel.WriteOptions{HeaderBold: true})
```

结构体层支持 `string`、`bool`、各宽度 `int`/`uint`、`float32/64`、`time.Time`
以及它们的一层指针。空单元格：普通字段取零值，指针字段为 `nil`，标了
`required` 的字段报错。

### 流式读写

大文件优先用文件路径入口，文件与数据行都不整体留在内存：

```go
// 流式读（文件路径）
err := excel.ReadEachFile("big.xlsx", opts, func(rowNum int, row map[string]string) error {
	if row["amount"] == "" {
		return fmt.Errorf("第 %d 行金额为空", rowNum)
	}
	return nil
})

// 流式写
w, err := excel.NewWriter(out, excel.WriteOptions{Columns: cols})
if err != nil {
	return err
}
for _, row := range data {
	if err := w.Write(row); err != nil {
		return err // 失败后丢弃 w，不要 Close
	}
}
return w.Close()
```

`ReadEachFile` 的回调返回 `excel.ErrStop` 表示成功提前结束；`ReadEach(io.Reader)`
只避免保留全部数据行，Excelize 仍会把整个流读入内存。`Writer` 未 `Close` 时
目标写入器不会收到文件；`Close` 之后不能再 `Write`。

## 列声明 `Column`

| 字段 | 含义 |
| --- | --- |
| `Header` | Excel 表头文本。有表头读取时必填；无表头时忽略；写出为空时用 `Key` |
| `Key` | 读取结果 map 的键，也是写出时取值的键；为空时读取/写出退化为 `Header` |
| `Type` | 读取时的校验与规范化方式，默认 `Text`；写出时忽略 |
| `Layout` | `Date`/`DateTime`/`Time` 的 Go 布局，如 `2006-01-02`；只用于输出 |
| `Required` | 读取到空单元格时报 `*RowError`；写出时忽略 |
| `Width` | 写出列宽（字符数），0 表示不设置，上限 255 |

`Key` 必须非空且唯一。`Type` 只能是 `Text`、`Int`、`Float`、`Bool`、`Date`、
`DateTime`、`Time`；日期类必须给 `Layout`。

## `ReadOptions`

| 字段 | 含义 |
| --- | --- |
| `Sheet` | 工作表名；为空用第一个 |
| `NoHeader` | `true` 时首行即数据，按位置对应 |
| `SkipRows` | 表头/数据之前跳过的物理行数（标题行、说明行） |
| `Columns` | 列声明，不能为空 |
| `RawValues` | 取单元格原始值（日期为序列号、数字不带格式） |
| `TrimSpace` | 去除数据单元格首尾空白；表头始终去 BOM 与空白 |
| `Validate` | 逐行业务校验，返回 `ErrStop` 表示成功提前结束 |

## `WriteOptions`

| 字段 | 含义 |
| --- | --- |
| `Sheet` | 工作表名；为空用 `Sheet1` |
| `NoHeader` | 不写表头行 |
| `Columns` | 列声明，顺序即列顺序 |
| `HeaderBold` | 表头加粗 |
| `FreezeHeader` | 冻结首行（写表头时生效） |

## 值的类型

读取结果统一是 `string`，`Type` 只做校验与规范化，不改变返回类型：

| Type | 接受 | 输出 |
| --- | --- | --- |
| `Text` | 任意文本 | 原样 |
| `Int` | 十进制整数 | 无前导零的规范形式 |
| `Float` | 浮点数 | 不使用科学计数法 |
| `Bool` | `TRUE`/`FALSE`/`true`/`false`、原始值 `1`/`0` | `true`/`false` |
| `Date`/`DateTime`/`Time` | Excel 日期序列号 | 按 `Layout` 输出 |

**日期列必须设置 `RawValues: true`**，否则装配期返回 `ErrInvalidColumn`。原因是
一行单元格只能统一取格式化值或原始值：格式化文本取决于单元格自己的数字格式，
靠它猜日期不可靠。开启后同一次读取的其他列也会拿到原始值（数字不带千分位、
日期为序列号）。

真实的数字精度由 Excel 决定：Excel 用 float64 存数字，超过 2^53 的整数在单元格
里已经失真。`Int` 不会把它变成"精确的 int64"。身份证号、工号、前导零编号建议
保持文本，不要声明成 `Int`。

## 行号与错误

行级错误统一是 `*RowError`，`Row` 是 Excel 物理行号（1-based，含 `SkipRows` 与
表头行，空行也计数）：

```go
var rowErr *excel.RowError
if errors.As(err, &rowErr) {
	fmt.Printf("第 %d 行第 %s 列: %v\n", rowErr.Row, rowErr.Column, rowErr.Err)
}
```

空行会被跳过（不产生结果），但行号继续推进；文件中间整行缺失时，行号仍然对应
Excel 里的物理行。

## 结构体 tag

```go
excel:"表头名"
excel:"表头名,layout:2006-01-02,required,width:20"
excel:"-"
```

- 第一段是表头；为空时用字段名。
- `layout:` 只对 `time.Time` 有意义，且必填。
- `required` 时空单元格报错。
- `width:` 是写出列宽。
- 没有 tag 时用字段名作为表头。

`time.Time` 字段读取时先尝试按 Excel 序列号解析，失败再按 `layout` 解析，所以
既能读真实日期单元格（建议 `RawValues: true`），也能读本包写出的文本日期。
写出时 `time.Time` 按 `layout` 写成文本。

结构体层不接受 `opts.Columns`；列完全由字段与 tag 决定。支持一层指针用于可空
字段；匿名字段、切片、map、结构体（除 `time.Time`）、多层指针都会在装配期返回
`ErrInvalidColumn`，不会静默丢数据。

## 错误与判断

| 哨兵 | 场景 |
| --- | --- |
| `ErrUnsupportedFormat` | 不是 xlsx/xlsm/xltx/xltm（含 `.xls`、csv、加密文件） |
| `ErrSheetNotFound` | 指定的工作表不存在 |
| `ErrEmptySheet` | 空表，或有表头模式找不到表头行 |
| `ErrMissingColumn` | 声明的列在表头中不存在 |
| `ErrDuplicateColumn` | 列声明重复，或文件表头重复出现声明列 |
| `ErrColumnMismatch` | 无表头模式下声明列数与文件列宽不一致 |
| `ErrInvalidColumn` | 列声明、tag 或选项不合法 |
| `ErrMissingKey` | 写出的数据行缺少声明的 Key |
| `ErrStop` | 校验回调用于成功提前结束，不是错误 |

## 常见错误

**日期列报 "必须设置 RawValues"**：给 `ReadOptions` 加 `RawValues: true`。

**无表头报列数不一致**：Excel 会裁掉行尾空单元格，`A1:C1` 里只有 A 有值时实际
有效宽度是 1。声明列数必须与之相等。

**写出报 "数据行缺少声明的键"**：每一行 map 都要包含全部声明的 `Key`，即使值
为空字符串也要保留键。

**读 `.xls` 报不支持**：Excelize 只支持 OOXML。先用 LibreOffice 或 Excel 转存为
xlsx。

**大文件内存高**：所有 `io.Reader` 入口（`Read`/`ReadEach`/`ReadInto`）都会
经 Excelize `OpenReader` 把整个流读入内存；改用 `ReadFile`/`ReadFileInto`/
`ReadEachFile` 走文件路径。

**Excel 显示日期是文本**：本包写出统一按字符串写，日期以 `Layout` 文本落盘。
需要真正的日期单元格时，请自行用 Excelize 写 `time.Time`。
