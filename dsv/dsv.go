// Package dsv 提供业务分隔符文本文件的读取、写出与校验。
//
// 文件格式固定为：一行一条记录，字段之间用同一个字符串分隔（可以是多个字符，
// 例如 "!^"），可选一行表头。字段内部不允许出现分隔符与换行，由业务代码保证；
// 本包不做引号、转义与类型猜测，字段数对不上直接返回行级错误。
//
// 读取用 Read/Each 得到 []map[string]string 或逐行回调，用
// ReadInto/EachInto 映射到结构体；文件入口是 ReadFile/EachFile 等。
// 写出用 Writer（map 层）或 StructWriter（结构体层），逐行追加后 Close 提交；
// 文件句柄通过同目录临时文件加 rename 原子落盘。所有读取都是一次性消费的流，
// 大文件请用 Each/EachFile，不会把整个文件读入内存。
//
// 三条固定语义：
//
//   - 只处理 UTF-8 字节，文件开头的 UTF-8 BOM 会被剥离；不做编码转换。
//   - 空行跳过；行尾接受 \n 与 \r\n；末尾分隔符必须由调用方显式声明。
//   - 有表头时按表头文本（去首尾空白、区分大小写）匹配声明的列，无表头时按位置对应。
//
// 包内所有类型都不是并发安全的；每次 Read/Each 都会把传入的 io.Reader 消费到
// 结尾（回调返回 ErrStop 时提前结束）。
package dsv

import (
	"errors"
	"fmt"
	"strings"
)

// ValueType 声明一列在读取时的校验与规范化方式。
//
// 它不改变返回类型：map 层的值始终是 string，类型只负责"能否解析 + 统一格式"。
// 需要真正的 Go 类型时使用 ReadInto/EachInto。
type ValueType string

const (
	// Text 原样返回字段文本，不做任何解析。这是默认值。
	Text ValueType = ""
	// Int 要求十进制整数，输出无前导零的规范形式。
	Int ValueType = "int"
	// Float 要求浮点数，输出不使用科学计数法的规范形式。
	Float ValueType = "float"
	// Bool 接受 TRUE/FALSE（大小写不敏感）与 1/0，输出 true/false。
	Bool ValueType = "bool"
	// Date 要求能按 Layout 解析的日期文本，输出 t.Format(Layout)。
	Date ValueType = "date"
	// DateTime 要求能按 Layout 解析的日期时间文本，输出 t.Format(Layout)。
	DateTime ValueType = "datetime"
	// Time 要求能按 Layout 解析的时间文本，输出 t.Format(Layout)。
	Time ValueType = "time"
)

// Column 声明一列的来源、输出键与读取规则。
//
// 有表头读取时按 Header 匹配表头文本；无表头读取时按 Columns 中的位置对应，
// Header 被忽略。写出时 Columns 的顺序就是字段顺序，Header 是写出的表头文本
// （为空时用 Key）。
type Column struct {
	// Header 是表头文本。有表头读取与写出表头时使用；无表头读取时忽略。
	Header string
	// Key 是读取结果 map 的键，也是写出时从数据行取值的键。
	// 为空时读取/写出都会退化为使用 Header。
	Key string
	// Type 是读取时的校验与规范化方式，默认 Text。写出不消费 Type 与 Layout。
	Type ValueType
	// Layout 是 Date/DateTime/Time 的 Go 时间布局，例如 "2006-01-02"。
	// 它是文本的解析与输出格式。
	Layout string
	// Required 为 true 时，读取到空字段会返回 *RowError。写出时忽略。
	Required bool
}

// ReadOptions 控制一次读取。
type ReadOptions struct {
	// Delimiter 是字段分隔符，不能为空，且不能包含 \n 或 \r。
	Delimiter string
	// NoHeader 为 true 时首行即数据，列按位置对应；为 false 时按 Header 匹配表头。
	NoHeader bool
	// TrailingDelimiter 声明每条非空行（含表头）都以 Delimiter 结尾。
	// 为 true 时缺少末尾分隔符会返回 *RowError；为 false 时按普通切分处理。
	TrailingDelimiter bool
	// SkipRows 是表头/数据之前要跳过的物理行数，用于标题行、说明行；不能为负。
	SkipRows int
	// TrimSpace 为 true 时去掉数据字段首尾空白。表头始终去 BOM 与首尾空白。
	TrimSpace bool
	// Columns 声明要读取的列；map 层不能为空，结构体层必须为空。
	Columns []Column
	// Validate 在交给调用方之前逐行调用。line 是物理行号（1-based）。
	// map 层的键是 Column.Key，值已类型规范化；结构体层的键是 tag 表头，
	// 值是原始文本（TrimSpace 之后）。返回 ErrStop 表示成功提前结束；
	// 返回其他错误会被包装成 *RowError；若返回的已是 *RowError，则原样透传。
	Validate func(line int, row map[string]string) error
}

// WriteOptions 控制一次写出。
type WriteOptions struct {
	// Delimiter 是字段分隔符，不能为空，且不能包含 \n 或 \r。
	Delimiter string
	// NoHeader 为 true 时不写表头行。
	NoHeader bool
	// TrailingDelimiter 为 true 时每行末尾（换行之前）补一个分隔符。
	TrailingDelimiter bool
	// LineEnding 是行尾序列，只接受 ""（等同 "\n"）、"\n" 或 "\r\n"。
	LineEnding string
	// Columns 声明 map 层写出的列顺序；结构体层必须为空。
	Columns []Column
	// Sanitize 在每个字段（含表头文本）拼接前调用，用于把换行、分隔符等
	// 替换成安全字符。为 nil 表示不处理；处理后仍含分隔符或换行会报错。
	Sanitize func(value string) string
}

// RowError 是带位置的读取/写出行级错误。
//
// Line 是物理行号（1-based，含表头与跳过行）；Column 是出错列的 Key（结构体层
// 为 tag 表头），整行错误时为空。用 errors.AsType 取出，再用 Unwrap/errors.Is
// 判断原因。
type RowError struct {
	Line   int
	Column string
	Err    error
}

// Error 实现 error，信息包含行号与列名。
func (e *RowError) Error() string {
	if e.Column != "" {
		return fmt.Sprintf("dsv: 第 %d 行第 %q 列: %v", e.Line, e.Column, e.Err)
	}
	return fmt.Sprintf("dsv: 第 %d 行: %v", e.Line, e.Err)
}

// Unwrap 返回底层错误，供 errors.Is/errors.AsType 使用。
func (e *RowError) Unwrap() error {
	return e.Err
}

// 装配期与读取期的哨兵错误。调用方用 errors.Is 判断。
var (
	// ErrInvalidOption 表示读取/写出选项本身不合法（分隔符、行尾、跳过行数等）。
	ErrInvalidOption = errors.New("dsv: 选项无效")
	// ErrInvalidColumn 表示列声明无效，例如 Key 为空、类型未知或日期类型缺 Layout。
	ErrInvalidColumn = errors.New("dsv: 列声明无效")
	// ErrMissingColumn 表示声明的列在文件表头中不存在。
	ErrMissingColumn = errors.New("dsv: 声明的列在表头中不存在")
	// ErrDuplicateColumn 表示列声明重复，或声明的表头在文件中出现多次。
	ErrDuplicateColumn = errors.New("dsv: 列声明重复或表头重复")
	// ErrColumnMismatch 表示记录的字段数与表头/声明列数不一致。
	ErrColumnMismatch = errors.New("dsv: 字段数与声明不一致")
	// ErrMissingKey 表示写出的数据行缺少声明的 Key。
	ErrMissingKey = errors.New("dsv: 数据行缺少声明的键")
	// ErrIllegalField 表示字段包含分隔符或换行，写出的文件将无法读回。
	ErrIllegalField = errors.New("dsv: 字段包含分隔符或换行")
	// ErrEmptyFile 表示有表头模式下文件为空或没有任何非空行。
	ErrEmptyFile = errors.New("dsv: 文件为空或没有表头行")
	// ErrStop 由校验或读取回调返回，表示成功的提前结束，不是错误。
	ErrStop = errors.New("dsv: 停止读取")
	// ErrClosed 表示 Writer/StructWriter 已经关闭或放弃。
	ErrClosed = errors.New("dsv: 已关闭")
)

// normalizeColumns 填充 Key 并校验列声明，返回可安全使用的副本。
//
// requireHeader 为 true 时要求 Header 非空且不重复；无表头读取时应为 false。
// cleanHeaders 为 true 时清洗声明表头（去 BOM 与首尾空白），用于读取路径；
// 写出路径保留调用方给定的表头文本。
func normalizeColumns(cols []Column, requireHeader, cleanHeaders bool) ([]Column, error) {
	if len(cols) == 0 {
		return nil, fmt.Errorf("%w: 未声明任何列", ErrInvalidColumn)
	}
	out := make([]Column, len(cols))
	keys := make(map[string]struct{}, len(cols))
	headers := make(map[string]struct{}, len(cols))
	for i, c := range cols {
		if cleanHeaders {
			c.Header = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.Header), "\ufeff"))
		}
		if c.Key == "" {
			c.Key = c.Header
		}
		if c.Key == "" {
			return nil, fmt.Errorf("%w: 第 %d 列的 Key 为空", ErrInvalidColumn, i+1)
		}
		if _, ok := keys[c.Key]; ok {
			return nil, fmt.Errorf("%w: Key %q 重复", ErrDuplicateColumn, c.Key)
		}
		keys[c.Key] = struct{}{}

		switch c.Type {
		case Text, Int, Float, Bool:
		case Date, DateTime, Time:
			if c.Layout == "" {
				return nil, fmt.Errorf("%w: 列 %q 的类型 %s 需要 Layout", ErrInvalidColumn, c.Key, c.Type)
			}
		default:
			return nil, fmt.Errorf("%w: 列 %q 的类型 %q 未知", ErrInvalidColumn, c.Key, c.Type)
		}

		if requireHeader {
			if c.Header == "" {
				return nil, fmt.Errorf("%w: 列 %q 缺少 Header", ErrInvalidColumn, c.Key)
			}
			if _, ok := headers[c.Header]; ok {
				return nil, fmt.Errorf("%w: 表头 %q 重复", ErrDuplicateColumn, c.Header)
			}
			headers[c.Header] = struct{}{}
		}
		out[i] = c
	}
	return out, nil
}

// checkDelimiter 校验读写共用的分隔符约束。
func checkDelimiter(delimiter string) error {
	if delimiter == "" {
		return fmt.Errorf("%w: 分隔符为空", ErrInvalidOption)
	}
	if strings.ContainsAny(delimiter, "\r\n") {
		return fmt.Errorf("%w: 分隔符不能包含换行", ErrInvalidOption)
	}
	return nil
}

// checkBase 校验读取选项里与列声明无关的部分。
func (o ReadOptions) checkBase() error {
	if err := checkDelimiter(o.Delimiter); err != nil {
		return err
	}
	if o.SkipRows < 0 {
		return fmt.Errorf("%w: SkipRows 不能为负", ErrInvalidOption)
	}
	return nil
}

// prepare 校验读取选项并返回规范化后的列声明。
func (o ReadOptions) prepare() ([]Column, error) {
	if err := o.checkBase(); err != nil {
		return nil, err
	}
	return normalizeColumns(o.Columns, !o.NoHeader, true)
}

// checkBase 校验写出选项里与列声明无关的部分。
func (o WriteOptions) checkBase() error {
	if err := checkDelimiter(o.Delimiter); err != nil {
		return err
	}
	switch o.LineEnding {
	case "", "\n", "\r\n":
		return nil
	default:
		return fmt.Errorf("%w: LineEnding 只能是 \\n 或 \\r\\n", ErrInvalidOption)
	}
}
