// Package excel 提供面向业务的 xlsx 读取、写出与结构体映射。
//
// 包只做一条链路：调用方声明要哪些列，包负责按声明取值、校验、规范化并
// 输出。读取结果默认是 []map[string]string，列声明里的 Key 就是 map 的键；
// 需要 Go 类型时用 ReadInto/WriteFrom 把行映射到结构体。
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
// 全量 API（Read/Write）与流式 API（ReadEach/ReadEachFile/Writer）共享同一套
// 列声明和语义。
// 所有函数都不修改调用方传入的切片；返回的 map 由调用方独占。
package excel

import (
	"errors"
	"fmt"

	"github.com/xuri/excelize/v2"
)

// ValueType 声明一列在读取时的校验与规范化方式。
//
// 它不改变输出类型：值始终是 string，类型只负责"能否解析 + 统一格式"。
// 真正需要 int64、time.Time 等 Go 类型时，使用结构体层的 ReadInto。
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
	// Type 是读取时的校验与规范化方式，默认 Text。写出时忽略。
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
	// Sheet 是工作表名；为空时使用第一个工作表。
	Sheet string
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
	// 被包装成 *RowError。
	Validate func(rowNum int, row map[string]string) error
}

// WriteOptions 控制一次写出。
type WriteOptions struct {
	// Sheet 是工作表名；为空时使用 "Sheet1"。
	Sheet string
	// NoHeader 为 true 时不写表头行。
	NoHeader bool
	// Columns 声明写出的列顺序；不能为空，Key 必须唯一。
	Columns []Column
	// HeaderBold 为 true 时表头加粗。
	HeaderBold bool
	// FreezeHeader 为 true 且写出表头时冻结首行。
	FreezeHeader bool
}

// RowError 是带位置的读取/写出行级错误。
//
// Row 是 Excel 物理行号（1-based，含表头与跳过行）；Column 是列 Key，
// 整行错误时为空。用 errors.As 取出，再用 Unwrap/errors.Is 判断原因。
type RowError struct {
	Row    int
	Column string
	Err    error
}

// Error 实现 error，信息包含行号与列名。
func (e *RowError) Error() string {
	if e.Column != "" {
		return fmt.Sprintf("excel: 第 %d 行第 %q 列: %v", e.Row, e.Column, e.Err)
	}
	return fmt.Sprintf("excel: 第 %d 行: %v", e.Row, e.Err)
}

// Unwrap 返回底层错误，供 errors.Is/errors.As 使用。
func (e *RowError) Unwrap() error {
	return e.Err
}

// 装配期与读取期的哨兵错误。调用方用 errors.Is 判断。
var (
	// ErrUnsupportedFormat 表示输入不是受支持的 xlsx/xlsm/xltx/xltm。
	ErrUnsupportedFormat = errors.New("excel: 不支持的文件格式")
	// ErrSheetNotFound 表示指定的工作表不存在。
	ErrSheetNotFound = errors.New("excel: 工作表不存在")
	// ErrEmptySheet 表示工作表为空，或有表头模式下找不到表头行。
	ErrEmptySheet = errors.New("excel: 工作表为空或没有表头行")
	// ErrMissingColumn 表示声明的列在表头中不存在。
	ErrMissingColumn = errors.New("excel: 声明的列在表头中不存在")
	// ErrDuplicateColumn 表示列声明重复，或文件表头中出现重复的声明列。
	ErrDuplicateColumn = errors.New("excel: 列声明重复或文件表头重复")
	// ErrColumnMismatch 表示无表头模式下声明列数与文件实际列宽不一致。
	ErrColumnMismatch = errors.New("excel: 无表头模式下声明列数与文件列宽不一致")
	// ErrInvalidColumn 表示列声明或选项本身不合法。
	ErrInvalidColumn = errors.New("excel: 列声明无效")
	// ErrMissingKey 表示写出的数据行缺少声明的键。
	ErrMissingKey = errors.New("excel: 数据行缺少声明的键")
	// ErrStop 由校验回调返回，表示成功的提前结束，不是错误。
	ErrStop = errors.New("excel: 停止读取")
)

// normalizeColumns 填充 Key 并校验列声明，返回可安全使用的副本。
func normalizeColumns(cols []Column) ([]Column, error) {
	if len(cols) == 0 {
		return nil, fmt.Errorf("%w: 未声明任何列", ErrInvalidColumn)
	}
	out := make([]Column, len(cols))
	seen := make(map[string]struct{}, len(cols))
	for i, c := range cols {
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

		if c.Width < 0 || c.Width > excelize.MaxColumnWidth {
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

// validate 校验读取选项，必须在打开工作簿之前调用。
func (o ReadOptions) validate() error {
	if o.SkipRows < 0 {
		return fmt.Errorf("%w: SkipRows 不能为负", ErrInvalidColumn)
	}
	cols, err := normalizeColumns(o.Columns)
	if err != nil {
		return err
	}
	seenHeader := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		if isDateType(c.Type) && !o.RawValues {
			return fmt.Errorf("%w: 列 %q 是日期类型，必须设置 RawValues", ErrInvalidColumn, c.Key)
		}
		if o.NoHeader {
			continue
		}
		if c.Header == "" {
			return fmt.Errorf("%w: 列 %q 缺少 Header", ErrInvalidColumn, c.Key)
		}
		if _, ok := seenHeader[c.Header]; ok {
			return fmt.Errorf("%w: 表头 %q 重复", ErrInvalidColumn, c.Header)
		}
		seenHeader[c.Header] = struct{}{}
	}
	return nil
}

// validate 校验写出选项，必须在创建工作簿之前调用。
func (o WriteOptions) validate() error {
	cols, err := normalizeColumns(o.Columns)
	if err != nil {
		return err
	}
	seenHeader := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		header := c.Header
		if header == "" {
			header = c.Key
		}
		if _, ok := seenHeader[header]; ok {
			return fmt.Errorf("%w: 写出的表头 %q 重复", ErrInvalidColumn, header)
		}
		seenHeader[header] = struct{}{}
	}
	return nil
}
