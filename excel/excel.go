// Package excel 提供面向业务的 xlsx 读取、写出与结构体映射。
//
// 包只做一条链路：调用方声明要哪些列，包负责按声明取值、校验、规范化并
// 输出。读取结果默认是 []map[string]string，列声明里的 Key 就是 map 的键；
// 需要 Go 类型时用 SheetReader.ReadInto/EachInto 读结构体，用
// Builder.WriteFrom/AddSheetFrom 写结构体。
//
// 三条固定语义：
//
//   - 读取严格按声明：声明之外的多余列/键忽略，声明了但文件里没有的列报错。
//   - 无表头模式按位置对应，声明列数必须等于文件实际列宽，否则报错。
//   - 值统一为 string；Column.Type 只做校验与规范化，不改变返回类型，也不
//     根据内容猜测类型。
//
// 支持的格式是 xlsx/xlsm/xltx/xltm（ZIP 容器）。旧版 .xls、csv、加密工作簿
// 会返回 ErrUnsupportedFormat，不做格式转换。
//
// 配置 ReadOptions.OnRowError 可跳过坏行继续读取，用来先加载好行、再汇总
// 行级错误生成报告。
//
// 读取用 Reader/SheetReader（Open/OpenFile → Sheet → Read/Each/ReadInto/
// EachInto），写出用 Builder/SheetWriter（NewBuilder/NewFileBuilder →
// AddSheet/AddSheetFrom → Write/WriteFrom → Close）；单 sheet 文件可以用
// ReadFile/ReadFileInto/WriteFile/WriteFileFrom 一行入口。读写共享同一套列声明和语义。
// 所有函数都不修改调用方传入的切片；返回的 map 由调用方独占。
package excel

import (
	"errors"
	"fmt"
	"io"

	"github.com/xuri/excelize/v2"
)

// ValueType 声明一列在读取时的校验与规范化方式。
//
// 它不改变输出类型：值始终是 string，类型只负责"能否解析 + 统一格式"。
// 真正需要 int64、time.Time 等 Go 类型时，使用 SheetReader.ReadInto。
type ValueType string

const (
	// Text 原样返回单元格文本，不做任何解析。这是默认值。
	Text ValueType = ""
	// Int 要求十进制整数，输出无前导零的规范形式。
	Int ValueType = "int"
	// Float 要求浮点数，输出不使用科学计数法的规范形式。
	Float ValueType = "float"
	// Bool 接受 TRUE/FALSE（大小写不敏感）与原始值 1/0，输出 true/false。
	Bool ValueType = "bool"
	// Date 要求日期序列号（必须同时设置 RawValues），按 Layout 输出。
	Date ValueType = "date"
	// DateTime 要求日期时间序列号（必须同时设置 RawValues），按 Layout 输出。
	DateTime ValueType = "datetime"
	// Time 要求时间序列号（必须同时设置 RawValues），按 Layout 输出。
	Time ValueType = "time"
)

// Column 声明一列的来源、输出键与读取/写出规则。
//
// 有表头读取时按 Header 匹配表头单元格；无表头读取时按 Columns 中的位置
// 对应 A、B、C 等列，Header 被忽略。写出时 Columns 的顺序就是列顺序，
// Header 是写出的表头文本（为空时用 Key）。
type Column struct {
	// Header 是 Excel 表头文本。有表头读取时必填；无表头读取时忽略；
	// 写出时为空则使用 Key。
	Header string
	// Key 是读取结果 map 的键，也是写出时从数据行取值的键。
	// 为空时读取/写出都会退化为使用 Header。
	Key string
	// Type 是读取时的校验与规范化方式，默认 Text。写出不消费 Type 与 Layout，
	// 但装配期仍会校验其合法性（未知类型或日期类型缺 Layout 会报错）。
	Type ValueType
	// Layout 是 Date/DateTime/Time 的 Go 时间布局，例如 "2006-01-02"。
	// 它用于输出格式；输入是 Excel 日期序列号，不按该布局解析文本。
	Layout string
	// Required 为 true 时，读取到空单元格会返回 *RowError。写出时忽略。
	Required bool
	// Width 是写出时的列宽（字符数），0 表示不设置，上限 255。
	Width float64
}

// ReadOptions 控制一次读取。
type ReadOptions struct {
	// NoHeader 为 true 时首行即数据，列按位置对应；为 false 时按 Header 匹配表头。
	NoHeader bool
	// SkipRows 是表头/数据之前要跳过的物理行数，用于标题行、说明行；不能为负。
	SkipRows int
	// Columns 声明要读取的列；不能为空。
	Columns []Column
	// RawValues 为 true 时取单元格原始值（日期为序列号、数字不带格式）。
	// 声明了 Date/DateTime/Time 列时必须为 true。
	RawValues bool
	// TrimSpace 为 true 时去除数据单元格首尾空白；表头始终会去除 BOM 与空白。
	TrimSpace bool
	// Validate 在类型规范化之后、交给调用方之前逐行调用。rowNum 是 Excel
	// 物理行号（1-based）。返回 ErrStop 表示成功提前结束；返回其他错误会
	// 被包装成 *RowError。字段解码已经失败的行不会调用 Validate。
	Validate func(rowNum int, row map[string]string) error
	// OnRowError 非 nil 时进入容错读取：行级错误（必填、类型转换、Validate、
	// 结构体转换等）不再中止读取，该行被跳过并交给它处理，后续行继续读。
	// 同一行多个错误会汇总成一个 *RowError（Err 为 errors.Join 的结果）。
	// 返回 nil 表示继续；返回非 nil 立即中止，并原样返回该错误。
	// 表头/空 sheet/IO 等文件级错误以及回调返回的普通错误不受影响，仍然直接
	// 返回。配置后 Read/ReadInto 只返回成功行，整体 error 为 nil。
	OnRowError func(*RowError) error
}

// WriteOptions 控制一次写出。
type WriteOptions struct {
	// NoHeader 为 true 时不写表头行。
	NoHeader bool
	// Columns 声明写出的列顺序；不能为空，Key 必须唯一。
	Columns []Column
	// HeaderBold 为 true 时表头加粗。
	HeaderBold bool
	// FreezeHeader 为 true 且写出表头时冻结首行。
	FreezeHeader bool
}

// SheetInfo 描述一个工作簿中的 sheet 条目。
//
// Index 是工作簿顺序（0-based）；Visible 为 false 表示隐藏或深度隐藏。
// Excelize 没有公开的 sheet 类型判断，chart/dialog sheet 也可能出现在
// Sheets 的返回列表里。
type SheetInfo struct {
	Name    string // 工作簿里的规范工作表名
	Index   int    // 工作簿顺序，0-based
	Visible bool   // 是否可见；隐藏/深度隐藏均为 false
}

// Reader 是只读工作簿句柄。
//
// 用 Open/OpenFile 创建，Close 释放底层工作簿。Reader 不是并发安全的；
// 同一时刻只允许一个 SheetReader.Each/EachInto 在跑。关闭后除 SheetReader.Info
// 外的读取操作返回 ErrClosed。
type Reader struct {
	file        *excelize.File
	closed      bool
	propsLoaded bool
	date1904    bool
}

// SheetReader 是一张工作表的读取句柄。
//
// 由 Reader.Sheet 创建；Read/Each 返回或回调 map 行，ReadInto/EachInto 把行
// 映射到结构体。Info 是缓存的工作表信息，Reader 关闭后仍可读取。
type SheetReader struct {
	reader *Reader
	name   string
	info   SheetInfo
}

// Builder 是工作簿写出器。
//
// 用 NewBuilder/NewFileBuilder 创建；AddSheet 返回 map 行的流式句柄，
// AddSheetFrom 返回结构体的流式句柄。Close 提交整个工作簿，Abort 丢弃。
// Builder 不是并发安全的；同一时刻只有一个活动 sheet。任一行写失败后进入
// 失败态，只能 Abort。
type Builder struct {
	target   io.Writer
	path     string
	file     *excelize.File
	sheets   int
	added    []string
	active   *SheetWriter
	style    int
	styleOk  bool
	styleErr error
	closed   bool
	failed   error
}

// SheetWriter 是一个 sheet 的流式写出句柄。
//
// Write 按调用顺序追加行；Close 只 Flush 当前 sheet，幂等。Builder.Close
// 会自动关闭未关闭的 sheet。
type SheetWriter struct {
	builder *Builder
	stream  *excelize.StreamWriter
	columns []Column
	rowNum  int
	closed  bool
}

// RowError 是带位置的读取/写出行级错误。
//
// Row 是 Excel 物理行号（1-based，含表头与跳过行）；Column 是出错列：
// map 层是 Column.Key，结构体层是字段 tag 的表头；整行错误或同一行多列出错
// 时为空。Cells 是出错行的实际单元格值（按列顺序），用于生成错误报告；
// 它不会被 Error() 输出，避免日志泄漏整行数据。同一行多个错误会汇总：
// Err 为 errors.Join 的结果，各列原因带列名。
// 用 errors.AsType 取出，再用 Unwrap/errors.Is 判断原因。
type RowError struct {
	Row    int
	Column string
	Cells  []string
	Err    error
}

// Error 实现 error，信息包含行号与列名。
func (e *RowError) Error() string {
	if e.Column != "" {
		return fmt.Sprintf("excel: 第 %d 行第 %q 列: %v", e.Row, e.Column, e.Err)
	}
	return fmt.Sprintf("excel: 第 %d 行: %v", e.Row, e.Err)
}

// Unwrap 返回底层错误，供 errors.Is/errors.AsType 使用。
func (e *RowError) Unwrap() error {
	return e.Err
}

// 装配期与读取期的哨兵错误。调用方用 errors.Is 判断。
var (
	// ErrUnsupportedFormat 表示输入不是受支持的 xlsx/xlsm/xltx/xltm。
	ErrUnsupportedFormat = errors.New("excel: 不支持的文件格式")
	// ErrSheetNotFound 表示指定的工作表不存在。
	ErrSheetNotFound = errors.New("excel: 工作表不存在")
	// ErrEmptySheet 表示工作簿/工作表为空，或表头模式下找不到表头行；
	// NoHeader 模式下空工作表返回空结果，不返回该错误。
	ErrEmptySheet = errors.New("excel: 工作表为空或没有表头行")
	// ErrMissingColumn 表示声明的列在表头中不存在。
	ErrMissingColumn = errors.New("excel: 声明的列在表头中不存在")
	// ErrDuplicateColumn 表示列声明重复，或文件表头中出现重复的声明列。
	ErrDuplicateColumn = errors.New("excel: 列声明重复或文件表头重复")
	// ErrColumnMismatch 表示无表头模式下声明列数与文件实际列宽不一致。
	ErrColumnMismatch = errors.New("excel: 无表头模式下声明列数与文件列宽不一致")
	// ErrInvalidColumn 表示列声明或选项本身不合法。
	ErrInvalidColumn = errors.New("excel: 列声明无效")
	// ErrClosed 表示 Reader/Builder/SheetWriter/StructSheetWriter 已经关闭或放弃。
	ErrClosed = errors.New("excel: 已关闭")
	// ErrSheetExists 表示要创建的工作表已经存在（大小写不敏感）。
	ErrSheetExists = errors.New("excel: 工作表已存在")
	// ErrEmptyWorkbook 表示 Builder 还没有添加任何 sheet。
	ErrEmptyWorkbook = errors.New("excel: 工作簿没有工作表")
	// ErrInvalidSheetName 表示工作表名不符合 Excel 规则。
	ErrInvalidSheetName = errors.New("excel: 工作表名无效")
	// ErrMissingKey 表示写出的数据行缺少声明的键。
	ErrMissingKey = errors.New("excel: 数据行缺少声明的键")
	// ErrStop 由校验回调返回，表示成功的提前结束，不是错误。
	ErrStop = errors.New("excel: 停止读取")
)

// normalizeColumns 填充 Key 并校验写出列声明，返回可安全使用的副本。
func normalizeColumns(cols []Column) ([]Column, error) {
	return normalize(cols, true, false)
}

// normalizeReadColumns 是读取路径的列规范化：清洗 Header、填默认 Key，
// 不校验 Width（Width 只作用于写出）。
func normalizeReadColumns(cols []Column) ([]Column, error) {
	return normalize(cols, false, true)
}

// normalize 是读写共用的列规范化实现。
func normalize(cols []Column, checkWidth, cleanHeaders bool) ([]Column, error) {
	if len(cols) == 0 {
		return nil, fmt.Errorf("%w: 未声明任何列", ErrInvalidColumn)
	}
	out := make([]Column, len(cols))
	seen := make(map[string]struct{}, len(cols))
	for i, c := range cols {
		if cleanHeaders {
			c.Header = cleanHeader(c.Header)
		}
		if c.Key == "" {
			c.Key = c.Header
		}
		if c.Key == "" {
			return nil, fmt.Errorf("%w: 第 %d 列的 Key 为空", ErrInvalidColumn, i+1)
		}
		if _, ok := seen[c.Key]; ok {
			return nil, fmt.Errorf("%w: Key %q 重复", ErrInvalidColumn, c.Key)
		}
		seen[c.Key] = struct{}{}

		switch c.Type {
		case Text, Int, Float, Bool:
		case Date, DateTime, Time:
			if c.Layout == "" {
				return nil, fmt.Errorf("%w: 列 %q 的类型 %s 需要 Layout", ErrInvalidColumn, c.Key, c.Type)
			}
		default:
			return nil, fmt.Errorf("%w: 列 %q 的类型 %q 未知", ErrInvalidColumn, c.Key, c.Type)
		}

		if checkWidth && (c.Width < 0 || c.Width > excelize.MaxColumnWidth) {
			return nil, fmt.Errorf("%w: 列 %q 的宽度 %.2f 超出范围", ErrInvalidColumn, c.Key, c.Width)
		}
		out[i] = c
	}
	return out, nil
}

// isDateType 判断是否为需要日期序列号的类型。
func isDateType(t ValueType) bool {
	return t == Date || t == DateTime || t == Time
}

// prepare 校验读取选项并返回规范化后的列声明。
func (o ReadOptions) prepare() ([]Column, error) {
	if o.SkipRows < 0 {
		return nil, fmt.Errorf("%w: SkipRows 不能为负", ErrInvalidColumn)
	}
	cols, err := normalizeReadColumns(o.Columns)
	if err != nil {
		return nil, err
	}
	seenHeader := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		if isDateType(c.Type) && !o.RawValues {
			return nil, fmt.Errorf("%w: 列 %q 是日期类型，必须设置 RawValues", ErrInvalidColumn, c.Key)
		}
		if o.NoHeader {
			continue
		}
		if c.Header == "" {
			return nil, fmt.Errorf("%w: 列 %q 缺少 Header", ErrInvalidColumn, c.Key)
		}
		if _, ok := seenHeader[c.Header]; ok {
			return nil, fmt.Errorf("%w: 表头 %q 重复", ErrInvalidColumn, c.Header)
		}
		seenHeader[c.Header] = struct{}{}
	}
	return cols, nil
}

// validate 校验读取选项；语义与 prepare 一致（丢弃列返回值）。
func (o ReadOptions) validate() error {
	_, err := o.prepare()
	return err
}

// prepare 校验写出选项并返回规范化后的列声明。
func (o WriteOptions) prepare() ([]Column, error) {
	cols, err := normalizeColumns(o.Columns)
	if err != nil {
		return nil, err
	}
	seenHeader := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		header := c.Header
		if header == "" {
			header = c.Key
		}
		if _, ok := seenHeader[header]; ok {
			return nil, fmt.Errorf("%w: 写出的表头 %q 重复", ErrInvalidColumn, header)
		}
		seenHeader[header] = struct{}{}
	}
	return cols, nil
}

// validate 校验写出选项；语义与 prepare 一致（丢弃列返回值）。
func (o WriteOptions) validate() error {
	_, err := o.prepare()
	return err
}

// rowProblem 是一条记录里某一列的解析错误。
type rowProblem struct {
	column string
	err    error
}

// joinRowErrors 把同一行的多个错误汇总成一个 *RowError。
//
// 只有一个错误时保留 Column；多个时 Column 为空，信息按「列: 原因」汇总，
// errors.Is 仍能命中被 %w 包装的哨兵。没有错误时返回 nil。
func joinRowErrors(rowNum int, problems []rowProblem) *RowError {
	switch len(problems) {
	case 0:
		return nil
	case 1:
		return &RowError{Row: rowNum, Column: problems[0].column, Err: problems[0].err}
	}
	errs := make([]error, len(problems))
	for i, p := range problems {
		errs[i] = fmt.Errorf("%s: %w", p.column, p.err)
	}
	return &RowError{Row: rowNum, Err: errors.Join(errs...)}
}
