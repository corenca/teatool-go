# excel

面向业务的 xlsx 读写包：按列声明取值、校验与规范化，支持 map、结构体、多工作表与流式读写。

- 只支持 xlsx/xlsm/xltx/xltm（ZIP 容器）；`.xls`、csv、加密工作簿返回 `ErrUnsupportedFormat`。
- 值统一为 `string`；`Column.Type` 只做校验与规范化，不改变返回类型。
- 读取严格按声明：多余列忽略，缺列报错；无表头模式下声明列数必须等于文件实际列宽。
- 写出每行必须包含全部声明的 `Key`，缺一个报 `*RowError`（`ErrMissingKey`）。
- 需要 Go 1.27+（结构体读写使用泛型方法与泛型类型）。

```bash
go get github.com/corenca/teatool-go/excel
```

## 单 sheet 文件：一行入口

```go
cols := []excel.Column{
	{Header: "订单号", Key: "no"},
	{Header: "金额", Key: "amount", Type: excel.Float},
}

rows, err := excel.ReadFile("in.xlsx", "订单", excel.ReadOptions{Columns: cols})
if err != nil {
	return err
}

err = excel.WriteFile("out.xlsx", "订单", rows, excel.WriteOptions{
	Columns:    cols,
	HeaderBold: true,
})
```

`sheet == ""` 时读取第一个工作表；**写出必须给非空 sheet 名**，否则返回 `ErrInvalidSheetName`。

结构体版本：

```go
type Order struct {
	No     string  `excel:"订单号"`
	Amount float64 `excel:"金额"`
}

orders, err := excel.ReadFileInto[Order]("in.xlsx", "订单", excel.ReadOptions{})
err = excel.WriteFileFrom("out.xlsx", "订单", orders, excel.WriteOptions{HeaderBold: true})
```

## 多 sheet：Reader / Builder

读取：

```go
r, err := excel.OpenFile("in.xlsx")
if err != nil {
	return err
}
defer r.Close()

infos, err := r.Sheets()
for _, info := range infos {
	fmt.Println(info.Name, info.Index, info.Visible)
}

s, err := r.Sheet("用户") // 大小写不敏感；空名取第一个
if err != nil {
	return err
}
rows, err := s.Read(excel.ReadOptions{Columns: cols})
users, err := s.ReadInto[User](excel.ReadOptions{})
```

`Open(io.Reader)` 会把流读入内存；大文件用 `OpenFile`。`Reader` 不是并发安全的，同一时刻只读一个 sheet。`Close` 幂等；关闭后 `Sheets`/`Sheet`/`Read`/`Each`/`EachInto` 返回 `ErrClosed`，`Info()` 仍可读。

读错误不会污染 Reader：A 表缺列报错后，仍可读 B 表或换声明重试 A 表。

写出：

```go
b, err := excel.NewFileBuilder("out.xlsx") // 原子写，权限 0644
if err != nil {
	return err
}
defer b.Abort() // Close 成功后是 no-op

if err := b.AddSheetRows("用户", rows, excel.WriteOptions{Columns: cols, HeaderBold: true}); err != nil {
	return err
}
if err := b.WriteFrom("统计", stats, excel.WriteOptions{HeaderBold: true}); err != nil {
	return err
}
return b.Close()
```

`NewBuilder(io.Writer)` 写入内存/网络流；`NewFileBuilder(path)` 写文件。`AddSheet` 返回流式句柄：

```go
sw, err := b.AddSheet("明细", excel.WriteOptions{Columns: cols})
if err != nil {
	return err
}
for _, row := range rows {
	if err := sw.Write(row); err != nil {
		return err // 失败后不要继续写，交给 defer b.Abort()
	}
}
if err := sw.Close(); err != nil { // 只 Flush 当前 sheet
	return err
}
```

`AddSheet`/`AddSheetFrom`/`AddSheetRows`/`WriteFrom` 会自动结束上一个活动 sheet。`Builder.Close` 才写整个工作簿；`SheetWriter.Close`/`StructSheetWriter.Close` 只 Flush，且幂等。

写失败后 Builder 进入失败态：`Close` 返回第一次失败的错误且不产出文件，只能 `Abort`。成功 `Close` 一次后再次 `Close` 返回 `ErrClosed`；无 sheet 时 `Close` 返回 `ErrEmptyWorkbook` 且不关闭，可以 `AddSheet` 后重试。

## 流式结构体写出

`AddSheetFrom[T]` 按字段 tag 生成列并返回泛型句柄，之后逐行 `Write`：

```go
sw, err := b.AddSheetFrom[User]("用户", excel.WriteOptions{HeaderBold: true})
if err != nil {
	return err
}
for _, u := range users {
	if err := sw.Write(u); err != nil {
		return err // 失败后不要继续写，交给 defer b.Abort()
	}
}
if err := sw.Close(); err != nil { // 只 Flush 当前 sheet
	return err
}
```

- 列顺序、表头、`width` 来自字段 tag；不接受 `opts.Columns`。
- `sw.Close` 只 Flush 当前 sheet；`Builder.Close` 会自动关闭未关闭的句柄。
- `Builder.WriteFrom` 是“`AddSheetFrom` + 逐行 `Write` + `Close`”的批量封装。
- Excelize 流式写要求行号递增、只能追加；同一 sheet 不能混用普通写和流式写。缓冲区超过 16MB 时会落临时文件，因此不要边写边读同一 sheet。

## 流式读取

```go
err := s.Each(excel.ReadOptions{Columns: cols}, func(rowNum int, row map[string]string) error {
	if row["金额"] == "" {
		return fmt.Errorf("第 %d 行金额为空", rowNum)
	}
	return nil
})
```

`rowNum` 是 Excel 物理行号（1-based，含表头与跳过行）。回调返回 `ErrStop` 表示成功提前结束；返回其他错误会包装成 `*RowError`。`Each` 不保留全部数据行；`OpenFile` 入口不把文件整体读入内存。

结构体版本：

```go
err := s.EachInto[User](excel.ReadOptions{}, func(rowNum int, u User) error {
	t.Logf("第 %d 行: %+v", rowNum, u)
	return nil
})
```

`EachInto` 与 `ReadInto` 共用字段 tag、类型解析与 `Validate`，只是不保留完整 `[]T`。`ReadInto` 在语义上等于用 `EachInto` 收集全部结果。

## 列声明

| 字段 | 含义 |
| --- | --- |
| `Header` | 表头文本；有表头读取时按它匹配，写出时为空则用 `Key` |
| `Key` | 读取结果 map 的键、写出行取值的键；为空时用 `Header` |
| `Type` | 读取校验与规范化方式，默认 `Text`；写出不消费，但装配期仍校验合法性 |
| `Layout` | `Date`/`DateTime`/`Time` 的输出布局；必填 |
| `Required` | 读取时空单元格报 `*RowError` |
| `Width` | 写出列宽，0 表示不设置；读取时忽略 |

| `Type` | 输入 | 输出 |
| --- | --- | --- |
| `Text` | 任意文本 | 原样 |
| `Int` | 十进制整数 | 无前导零 |
| `Float` | 浮点数 | 非科学计数法 |
| `Bool` | `TRUE/FALSE`（大小写不敏感）、`1/0` | `true`/`false` |
| `Date`/`DateTime`/`Time` | Excel 日期序列号 | 按 `Layout` |

日期列必须同时设置 `RawValues: true`；同一次读取的其他列也会拿到原始值（数字不带格式）。`TrimSpace` 会去除数据单元格首尾空白，纯空格行按空行跳过。

### 无表头

```go
rows, err := s.Read(excel.ReadOptions{
	NoHeader: true,
	Columns: []excel.Column{
		{Key: "col1"},
		{Key: "col2"},
	},
})
```

无表头时声明按位置对应 A、B、C；声明列数必须等于文件实际列宽（所有数据行中最宽一行的有效单元格数），否则 `ErrColumnMismatch`。`NoHeader` 下空工作表返回空结果，不返回 `ErrEmptySheet`。

## 结构体映射

```go
type User struct {
	Name     string     `excel:"姓名"`
	Age      *int       `excel:"年龄"` // 空单元格 → nil；0 → &0
	Birthday *time.Time `excel:"生日,layout:2006-01-02"`
	Skip     string     `excel:"-"`
}
```

- tag 第一段是表头；没有 tag 时用字段名。
- 选项：`layout:`（`time.Time` 必填）、`required`、`width:`；`excel:"-"` 跳过。
- 支持 `string`、`bool`、各宽度 `int`/`uint`、`float32/64`、`time.Time` 以及它们的一层指针。
- 非标量自定义类型只要实现 `encoding.TextMarshaler`（写）或 `encoding.TextUnmarshaler`（读）即可按文本列处理；`time.Time` 仍优先走 layout/序列号。
- 嵌套/匿名字段会递归展平：父字段 tag 的第一段作为前缀，生成 `前缀.子表头`；不写父 tag 时叶子表头保持原样。父字段不支持 `required`/`width`/`layout` 等选项，选项写在叶子字段上。
- 嵌套指针读取时有任一非空叶子才分配，全空保持 `nil`；写出 `nil` 时整组子列写空。
- 只有顶层结构体的 `Validate() error` 会被调用；嵌套结构体自身的 `Validate` 不会单独调用。
- 结构体层不接受 `opts.Columns`；列由字段与 tag 决定。
- `time.Time` 先按 `layout` 解析，失败再按 Excel 日期序列号解析；读真实日期单元格建议 `RawValues: true`。

### 可空字段与零值

可能为空的字段用一层指针：空单元格读成 `nil`，单元格里明确写着 `0` 或某个时间则读成非 `nil` 指针。

```go
if u.Age == nil {
	// 空，不是 0
} else if *u.Age == 0 {
	// 明确写了 0
}
```

写回同样按指针语义：`nil` 写空单元格，`&0` 写 `"0"`，`&time.Time{}` 写 `"0001-01-01 00:00:00"`。

- `*string` 区分不了“空单元格”和“明确写了空字符串”，xlsx 层面两者相同。
- `required` 与指针组合时空单元格仍报错。
- 建议开启 `TrimSpace`，否则只含空格的数值单元格会被当成非空并解析失败。
- 所有列都为空的行属于空行，会被跳过。

## 工作表名

`AddSheet`/`AddSheetFrom`/`AddSheetRows`/`WriteFrom` 以及 `WriteFile`/`WriteFileFrom` 都要求非空名字，规则与 Excelize 一致：

- 1–31 个 UTF-16 单元（emoji 算 2）；
- 不含 `:\/?*[]`；
- 首尾不能是单引号；
- 大小写不敏感去重（`Foo` 与 `foo` 冲突）。

重名返回 `ErrSheetExists`，非法名返回 `ErrInvalidSheetName`；这两种校验不污染 Builder，可以换名重试。

读取时 `Reader.Sheet(name)` 沿用 Excelize 的大小写不敏感匹配，首尾空格有意义，不做 trim；`Info().Name` 返回工作簿里的规范名字。

## 错误

```go
var (
	ErrUnsupportedFormat
	ErrSheetNotFound
	ErrEmptySheet
	ErrMissingColumn
	ErrDuplicateColumn
	ErrColumnMismatch
	ErrInvalidColumn
	ErrMissingKey
	ErrStop
	ErrClosed
	ErrSheetExists
	ErrEmptyWorkbook
	ErrInvalidSheetName
)
```

行级错误用 `*RowError`，含 Excel 物理行号与列名：

```go
if rowErr, ok := errors.AsType[*excel.RowError](err); ok {
	fmt.Println(rowErr.Row, rowErr.Column)
}
```

`errors.AsType` 从 Go 1.26 开始提供，本项目要求 Go 1.27+，优先使用它；`errors.As` 仍然可用。

`RowError.Column` 在 map 层是 `Column.Key`，在结构体层是字段 tag 的表头。

## 常见错误

- **缺列**：声明了文件里不存在的表头 → `ErrMissingColumn`。
- **无表头列数不一致**：Excelize 会裁掉行尾空单元格；声明列数必须等于实际有效宽度。
- **写出缺 Key**：每一行 map 必须包含全部声明 `Key`，空字符串是合法值。
- **日期列报“必须设置 RawValues”**：加 `RawValues: true`。
- **写侧 sheet 名为空**：写侧不做自动命名，给显式名字。
- **Builder 失败后继续写**：任一行失败后只能 `Abort`；`Close` 返回原错误且不产出文件。
- **读 `.xls` 不支持**：先转存为 xlsx。
- **大文件内存高**：用 `OpenFile`/`Each`/`SheetWriter`；`Open(io.Reader)` 会把整个流读入内存。

## 限制

- 不支持在已有工作簿上追加/修改 sheet；只创建新工作簿。
- 不支持并发写多个 sheet；一次只有一个活动 sheet。
- 流式写必须按行递增追加，不能在同一 sheet 混用普通写和流式写；Excelize 缓冲区超过 16MB 时会使用临时文件。
- 不做自动列宽、样式 DSL、图片/图表/批注/公式写入。
- 合并单元格只有左上角有值；公式取缓存值。
- `Sheets()` 可能包含 chart/dialog sheet（Excelize 没有公开的类型判断）；非 worksheet 上的 `Read/Each` 会由 Excelize 报错。
