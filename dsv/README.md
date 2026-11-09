# dsv

按声明读取、流式处理、映射结构体、校验并写出**分隔符文本文件**。

业务系统之间的数据交换经常是一份纯文本：一行一条记录，字段用固定分隔符连接。
分隔符可以是多个字符（例如 `!^`、`|~`），文件可能有表头也可能没有。`dsv` 把这
类文件的读取与写出收敛成一套声明式 API，并在字段数对不上时立即报错，避免整行
错位。

## 格式边界

- 一行一条记录；字段内部**不能**出现分隔符与换行，由业务代码保证。
- 不支持引号包裹、转义、字段内换行与跨行记录。
- 只处理 UTF-8 字节；文件开头的 UTF-8 BOM 会自动剥离，不做编码转换。
- 末尾分隔符必须由调用方显式声明，包内不做猜测。
- 行尾接受 `\n` 与 `\r\n`；最后一行没有换行也照常读取；完全空行跳过。
- 有表头时按表头文本（去首尾空白、区分大小写）匹配声明列；无表头时按位置对应。

## 快速开始

假设文件 `users.txt` 内容为：

```text
姓名!^年龄!^分数
张三!^30!^98.5
李四!^40!^90
```

读取为 `[]map[string]string`：

```go
opts := dsv.ReadOptions{
	Delimiter: "!^",
	Columns: []dsv.Column{
		{Header: "姓名", Key: "name", Required: true},
		{Header: "年龄", Key: "age", Type: dsv.Int},
		{Header: "分数", Key: "score", Type: dsv.Float},
	},
}

rows, err := dsv.ReadFile("users.txt", opts)
if err != nil {
	log.Fatal(err)
}
for _, row := range rows {
	fmt.Println(row["name"], row["age"], row["score"])
}
```

`age` 会输出规范化后的 `30`（去掉前导零），`score` 输出 `98.5`。声明之外的多余
列会被忽略；声明的列在表头中不存在则返回 `ErrMissingColumn`。

## 流式读取

大文件用 `Each`/`EachFile`，不会把整个文件读入内存：

```go
err := dsv.EachFile("users.txt", opts, func(line int, row map[string]string) error {
	if row["age"] == "0" {
		return nil // 跳过不需要的行
	}
	return save(row)
})
```

- `line` 是物理行号（1-based，含表头与跳过行）。
- 回调返回 `dsv.ErrStop` 表示成功提前结束，不会被当成错误。
- 回调返回其他错误会包成 `*dsv.RowError`，保留行号。
- 行长度没有 64KB 上限，超长行也能读。

## 结构体映射

用 `dsv` tag 把字段映射到列：

```go
type User struct {
	Name  string     `dsv:"姓名"`
	Age   int        `dsv:"年龄,required"`
	Score *float64   `dsv:"分数"`
	Birth time.Time  `dsv:"生日,layout:2006-01-02"`
	Note  string     `dsv:"-"`
}

users, err := dsv.ReadFileInto[User]("users.txt", dsv.ReadOptions{Delimiter: "!^"})
```

- tag 格式：`dsv:"表头[,required][,layout:2006-01-02]"`；第一段为空时用字段名，
  `dsv:"-"` 跳过该字段，未导出字段忽略。
- 结构体层不接受 `ReadOptions.Columns`（列来自 tag），非空返回 `ErrInvalidColumn`。
- 支持 `string`、`bool`、各宽度整数与浮点、带 `layout` 的 `time.Time`，以及实现
  `encoding.TextUnmarshaler`/`encoding.TextMarshaler` 的自定义类型。
- 一层指针表示可空：空字段 → `nil`，`"0"`/零点 → 非 `nil`。
- 文件里没有 NULL 标记：空字段就是空字符串。map 层统一是 `""`；结构体层非指针
  字段得到零值，需要区分「没填」与「零值」时用指针。
- 读取完成后会调用 `Validate() error`（如实现），错误包成 `*RowError`。
- 结构体层同样执行 `ReadOptions.Validate`：键是 tag 表头，值是原始文本
  （`TrimSpace` 之后、类型转换之前）。
- 文本文件是扁平列，不支持嵌套结构体展平。

流式结构体读取用 `EachInto`/`EachFileInto`。

## 写出

```go
rows := []map[string]string{
	{"name": "张三", "age": "30"},
	{"name": "李四", "age": "40"},
}
opts := dsv.WriteOptions{
	Delimiter: "!^",
	Columns: []dsv.Column{
		{Header: "姓名", Key: "name"},
		{Header: "年龄", Key: "age"},
	},
}
if err := dsv.WriteFile("out.txt", rows, opts); err != nil {
	log.Fatal(err)
}
```

文件句柄（`NewFileWriter`、`WriteFile`）先写同目录临时文件，`Close` 成功后
`rename` 到目标路径；失败或 `Abort` 时删除临时文件，目标文件保持原样。写结构体
用 `NewStructWriter[T]`/`WriteFileFrom[T]`：

```go
if err := dsv.WriteFileFrom("users.txt", users, dsv.WriteOptions{Delimiter: "!^"}); err != nil {
	log.Fatal(err)
}
```

### 字段内文本替换

字段里出现换行或分隔符时，写出的文件将无法读回。用 `Sanitize` 在拼接前逐个替换：

```go
opts.Sanitize = strings.NewReplacer(
	"\r\n", " ",
	"\n", " ",
	"\r", " ",
	"!^", ",",
).Replace
```

`Sanitize` 也作用于表头。替换后若字段仍含分隔符或换行，返回 `*RowError`
（`ErrIllegalField`），不会写出坏数据。

## 无表头

```go
opts := dsv.ReadOptions{
	Delimiter: "!^",
	NoHeader:  true,
	Columns: []dsv.Column{
		{Key: "name"},
		{Key: "age", Type: dsv.Int},
	},
}
```

无表头时 `Columns` 的位置就是字段位置，`Header` 被忽略；字段数必须等于声明列数，
否则返回 `ErrColumnMismatch`。结构体层则按字段顺序对应。

## 末尾分隔符

有些接口要求每行以分隔符结尾：

```text
姓名!^年龄!^
张三!^30!^
```

显式声明后再读取：

```go
opts.TrailingDelimiter = true
```

声明后每个非空行（含表头）都必须以分隔符结尾，缺少时返回 `*RowError`
（`ErrIllegalField`）；通过后只去掉一个分隔符再切分。写出时该选项会在每行换行前
补一个分隔符。

注意：该模式下去掉末尾分隔符后，最后一列仍可为空。要表达「最后一列为空」，
该行需要以**两个**分隔符结尾。例如声明三列（姓名、年龄、备注）、备注为空时，
数据行写成 `张三!^30!^!^`：去掉末尾一个分隔符后仍是三个字段，最后一列为空。
分隔符本身以空白结尾时，行尾的额外空白也能正确识别。

## 跳过标题行与去空白

- `SkipRows`：表头/数据之前跳过的物理行数（空行也计数），用于"导出时间..."这类
  文件头。
- `TrimSpace`：去掉数据字段首尾空白。表头始终去首尾空白与 BOM。
  `TrimSpace` 为真时，行尾空白会在末尾分隔符校验之前去掉。

## 编码

本包只读 UTF-8。GBK/GB18030 文件请先解码再传给 `io.Reader`：

```go
f, err := os.Open("gbk.txt")
if err != nil {
	return err
}
defer f.Close()

// 需要 golang.org/x/text/encoding/simplifiedchinese 与 transform
decoded := transform.NewReader(f, simplifiedchinese.GBK.NewDecoder())
rows, err := dsv.Read(decoded, opts)
```

写出 GBK 时同理：把 `Writer` 的目标换成一个编码后的 `io.Writer`。

## 选项速查

`ReadOptions`：

| 字段 | 含义 |
| --- | --- |
| `Delimiter` | 分隔符，必填，不能含换行 |
| `NoHeader` | 首行即数据，按位置对应 |
| `TrailingDelimiter` | 声明每行（含表头）以分隔符结尾 |
| `SkipRows` | 跳过的物理行数 |
| `TrimSpace` | 去数据字段首尾空白 |
| `Columns` | 列声明（map 层必填，结构体层必须为空） |
| `Validate` | 逐行回调；map 层键为 `Key` 且值已规范化，结构体层键为 tag 表头且值为原始文本；`ErrStop` 成功结束 |

`WriteOptions`：

| 字段 | 含义 |
| --- | --- |
| `Delimiter` | 分隔符，必填，不能含换行 |
| `NoHeader` | 不写表头 |
| `TrailingDelimiter` | 每行末尾补分隔符 |
| `LineEnding` | `""`/`"\n"` 或 `"\r\n"` |
| `Columns` | map 层列顺序；结构体层必须为空 |
| `Sanitize` | 字段（含表头）写前替换函数 |

`Column.Type` 的取值：`Text`（默认，原样）、`Int`、`Float`、`Bool`、`Date`、
`DateTime`、`Time`。后三者必须提供 `Layout`，按该布局解析文本并输出。

`Type` 和 `Required` 只在读取时生效；写出时 map 层原样输出字符串、不做类型校验。
结构体层用字段类型和 tag 的 `layout` 格式化。

## 错误处理

读取与写出的行级错误都是 `*dsv.RowError`：

```go
var rowErr *dsv.RowError
if errors.As(err, &rowErr) {
	fmt.Printf("第 %d 行第 %q 列: %v\n", rowErr.Line, rowErr.Column, rowErr.Err)
}

switch {
case errors.Is(err, dsv.ErrColumnMismatch):
	// 字段数与声明不一致
case errors.Is(err, dsv.ErrMissingKey):
	// 写出的 map 缺少声明的 Key
case errors.Is(err, dsv.ErrIllegalField):
	// 字段含分隔符或换行（含末尾分隔符缺失）
}
```

`RowError.Text` 保存出错记录的原始行（不含行尾换行），用于生成错误报告；它
不会被 `Error()` 输出，避免日志整体打印时泄漏原文。

常用哨兵：`ErrInvalidOption`、`ErrInvalidColumn`、`ErrMissingColumn`、
`ErrDuplicateColumn`、`ErrColumnMismatch`、`ErrMissingKey`、`ErrIllegalField`、
`ErrEmptyFile`、`ErrStop`、`ErrClosed`。表头级错误（缺列、重复表头、空文件）
直接返回哨兵，不包装成 `*RowError`。

## 容错读取：记录坏行并继续

默认是快速失败：第一行出错就停止，`Read` 返回 `nil, err`。如果业务要求「好行
照常加载、坏行记录下来稍后输出报告」，配置 `OnRowError`：

```go
var issues []*dsv.RowError
rows, err := dsv.ReadFile("users.txt", dsv.ReadOptions{
	Delimiter: "!^",
	Columns: []dsv.Column{
		{Header: "姓名", Key: "name", Required: true},
		{Header: "年龄", Key: "age", Type: dsv.Int},
		{Header: "分数", Key: "score", Type: dsv.Float},
	},
	OnRowError: func(e *dsv.RowError) error {
		issues = append(issues, e)
		return nil
	},
})
if err != nil {
	// 只剩文件级错误：选项、表头、IO
}

// rows 只包含成功行；issues 每项对应一个坏行
for _, e := range issues {
	fmt.Printf("第 %d 行: %v\n原文: %s\n", e.Line, e.Err, e.Text)
}
```

语义：

- **每行最多回调一次**。同一行多个字段出错会汇总，`e.Err` 是 `errors.Join`
  的结果，信息按 `列: 原因` 分行；此时 `e.Column` 为空。单列出错时 `e.Column`
  保留该列的 `Key`（结构体层是 tag 表头）。
- **`e.Text` 是原始行**，不含行尾换行；不在 `Error()` 里，报告由业务方自行生成。
- 行级错误包括切分失败、字段数不符、类型解析、必填为空、`Validate` 返回错误。
- 字段解码已失败的行不会再调用 `Validate`（map 层与结构体层一致）。
- 处理器返回 `nil` 继续读下一行；返回非 `nil` 立即中止，`Read` 原样返回该错误。
- 表头级错误（缺列、重复、空文件）、选项错误、IO 错误仍然直接返回，不会进入
  处理器。回调返回的普通错误属于业务失败，也不会被当作坏行跳过。

需要把报告落到文件时，直接复用本包的写出能力（报告格式由业务决定）：

```go
w, err := dsv.NewFileWriter("issues.txt", dsv.WriteOptions{
	Delimiter: "!^",
	Columns: []dsv.Column{
		{Header: "行号", Key: "line"},
		{Header: "原因", Key: "reason"},
		{Header: "原文", Key: "text"},
	},
	Sanitize: func(s string) string {
		s = strings.ReplaceAll(s, "!^", " ")
		return strings.NewReplacer("\n", " ", "\r", " ").Replace(s)
	},
})
if err != nil {
	return err
}
defer w.Abort()
for _, e := range issues {
	if err := w.Write(map[string]string{
		"line":   strconv.Itoa(e.Line),
		"reason": e.Err.Error(),
		"text":   e.Text,
	}); err != nil {
		return err
	}
}
return w.Close()
```

## 常见错误

- **字段数不一致**：数据行比表头多/少一个字段，通常是字段里出现了分隔符，
  或末尾分隔符没有声明。错误会带行号。
- **表头匹配不上**：表头去空白后精确匹配且区分大小写；`姓名` 与 `姓名 ` 相同，
  `name` 与 `Name` 不同。
- **忘记声明末尾分隔符**：文件每行以 `!^` 结尾却未设 `TrailingDelimiter`，
  最后一列会是空字符串或者字段数不符。
- **写出时缺 Key**：`Columns` 里的每个 `Key` 都必须在 map 中存在，空字符串可以，
  键缺失不行。
- **写出的字段含换行**：未设置 `Sanitize` 或替换不完整，返回 `ErrIllegalField`。
  先处理换行、回车与分隔符再写。

## 并发

读取入口（`Read`/`Each` 系列）一次性消费传入的 `io.Reader`；`Writer`/`StructWriter`
有失败态。它们都**不是并发安全的**，需要并发处理时每个 goroutine 使用独立的读写器。
