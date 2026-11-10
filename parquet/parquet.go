// Package parquet 提供 parquet 列式文件的类型化读写。
//
// 读写的底层实现是 github.com/parquet-go/parquet-go，本包在此之上补齐四件事：
// 构造与读写入口不 panic（上游的用户错误一律 panic）、读取前做投影/缺列/类型
// 兼容校验、文件写出走临时文件 + rename 保证原子性、压缩等选项面向配置友好。
//
// 列声明直接使用上游的 struct tag（例如 `parquet:"id"`、
// `parquet:"ts,timestamp(microsecond)"`），Schema 类型是上游类型的别名，因此
// 需要高级能力时可以继续 import 上游，两者类型互通。
//
// 结构体是主路径：正常业务读写都用结构体 + tag 表达 schema。T 为
// map[string]any 的动态模式只服务探查、格式转换这类编译期 schema 未知的场景，
// 返回的是 parquet 物理值（详见 README）。
//
// Reader 与 Writer 都不是并发安全的：每个实例只应被一个 goroutine 使用。
// 同一进程内可以同时存在多个独立的 Reader/Writer。
package parquet

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	pq "github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/compress"
)

// 上游类型的别名。别名而不是包装，保证与 github.com/parquet-go/parquet-go
// 的类型完全互通，调用方不必在两个包之间转换。
type (
	// Schema 是 parquet 文件的 schema，可由结构体 tag 推导或显式构造。
	Schema = pq.Schema
	// Field 是 schema 中的字段。
	Field = pq.Field
	// Value 是列式表示中的单个值。
	Value = pq.Value
	// Row 是一行按列展开的值序列。
	Row = pq.Row
)

var (
	// ErrNotParquet 表示输入不是可读的 parquet 文件（长度不足、首尾魔数不对、
	// 或使用了不受支持的加密 footer）。
	ErrNotParquet = errors.New("parquet: 不是 parquet 文件")
	// ErrInvalidOption 表示选项非法，例如未知的压缩名、负数行数、
	// 结构体模式给 ReadOptions/WriteOptions 传了 Schema。
	ErrInvalidOption = errors.New("parquet: 选项非法")
	// ErrInvalidSchema 表示类型或 schema 不可用：T 不是结构体/map、结构体没有
	// 导出列、动态写出没有提供 Schema。
	ErrInvalidSchema = errors.New("parquet: schema 非法")
	// ErrUnknownColumn 表示 ReadOptions.Columns 中的列路径在文件里不存在。
	ErrUnknownColumn = errors.New("parquet: 未知列")
	// ErrMissingColumn 表示目标类型需要的列在文件里不存在，详见
	// *MissingColumnsError。
	ErrMissingColumn = errors.New("parquet: 缺少列")
	// ErrIncompatibleColumn 表示同名列的物理类型或逻辑类型对不上，详见
	// *IncompatibleColumnsError。
	ErrIncompatibleColumn = errors.New("parquet: 列类型不兼容")
	// ErrClosed 表示 Reader 或 Writer 已经关闭。
	ErrClosed = errors.New("parquet: 已关闭")
	// ErrStop 只在 Reader.Each 的回调里使用：回调返回它表示"已经取够了"，
	// Each 停止遍历并返回 nil，而不是把这个错误抛给调用方。
	// 用 errors.Is 判断，因此包装后仍然有效。
	ErrStop = errors.New("parquet: 停止遍历")
)

// MissingColumnsError 是读取时目标类型需要、但文件里不存在的列集合。
//
// 用 errors.Is(err, ErrMissingColumn) 判断类型，用 errors.AsType 取出明细：
// Columns 是点号分隔的列路径，按字典序排列。
type MissingColumnsError struct {
	// Columns 是缺失的列路径，例如 ["age", "user.city"]。
	Columns []string
}

// Error 实现 error。
func (e *MissingColumnsError) Error() string {
	return "parquet: 文件缺少列: " + joinColumns(e.Columns)
}

// Unwrap 让 errors.Is 命中 ErrMissingColumn。
func (e *MissingColumnsError) Unwrap() error { return ErrMissingColumn }

// ColumnMismatch 描述一列的兼容性问题。
type ColumnMismatch struct {
	// Path 是点号分隔的列路径。
	Path string
	// Target 是目标 schema 中的类型，例如 "timestamp(microsecond:utc)"。
	Target string
	// File 是文件中的类型。
	File string
	// Hint 是建议的 tag 片段（如 "timestamp(microsecond)"），只对时间戳/日期给出。
	Hint string
}

// IncompatibleColumnsError 是同名列类型不兼容的汇总错误。
//
// 用 errors.Is(err, ErrIncompatibleColumn) 判断类型。时间戳与日期的建议 tag
// 会写进 Error() 的文本里。
type IncompatibleColumnsError struct {
	// Columns 是按列路径排序的不兼容列。
	Columns []ColumnMismatch
}

// Error 实现 error。
func (e *IncompatibleColumnsError) Error() string {
	msg := "parquet: 列类型不兼容:"
	for _, c := range e.Columns {
		msg += " 列 " + quote(c.Path) + " 文件 " + c.File + "，目标 " + c.Target
		if c.Hint != "" {
			msg += "；请改为 parquet:\"" + c.Path + "," + c.Hint + "\""
		}
		msg += ";"
	}
	return msg
}

// Unwrap 让 errors.Is 命中 ErrIncompatibleColumn。
func (e *IncompatibleColumnsError) Unwrap() error { return ErrIncompatibleColumn }

// Meta 是文件的 footer 元信息，读取它不需要扫描数据页。
type Meta struct {
	// NumRows 是文件总行数。
	NumRows int64
	// RowGroups 是行组数量。
	RowGroups int
	// Size 是文件字节数。
	Size int64
	// CreatedBy 是写入端记录的工具与版本信息，可能为空。
	CreatedBy string
	// KeyValue 是文件级 key/value 元信息，没有时为 nil。
	KeyValue map[string]string
}

// ReadOptions 控制一次读取。
type ReadOptions struct {
	// Columns 是列投影，元素是点号分隔的列路径（如 "user.name"），空表示
	// 读全部列。列名在文件中不存在时返回包装 ErrUnknownColumn 的错误。
	//
	// 投影是显式裁剪：没有被投影的字段读出来是零值，不会因此报缺列。
	Columns []string
	// Schema 只用于动态读取（T = map[string]any）：为空时使用文件 schema，
	// 非空时作为目标 schema（只允许相对文件增删列）。T 是结构体时非空会
	// 返回 ErrInvalidOption。
	Schema *Schema
	// AllowMissingColumns 为 false（默认）时，目标类型需要但文件里没有的列
	// 会让 OpenReader 返回 *MissingColumnsError，避免静默读出零值。
	AllowMissingColumns bool
	// SkipRows 是读取前跳过的行数，用上游 SeekToRow 定位，不扫描跳过的数据。
	// 为负数返回 ErrInvalidOption。
	SkipRows int64
}

// WriteOptions 控制一次写出。
type WriteOptions struct {
	// Schema 是写出 schema。T 是结构体时省略（由 tag 推导），非空返回
	// ErrInvalidOption；T 是 map[string]any 时必填，否则返回 ErrInvalidSchema。
	Schema *Schema
	// Compression 是列默认压缩方式："" 等同 "snappy"，另支持 "zstd"、"gzip"、
	// "lz4"、"none"。struct tag 里单独声明了压缩的列以 tag 为准；未知取值
	// 返回 ErrInvalidOption。
	Compression string
	// MaxRowsPerRowGroup 是每个行组的最大行数，0 表示不限制。
	MaxRowsPerRowGroup int64
	// PageBufferSize 是页缓冲大小（字节），0 表示使用上游默认 256 KiB。
	PageBufferSize int
	// WriteBufferSize 是写缓冲大小（字节），0 表示使用上游默认 32 KiB。
	WriteBufferSize int
	// KeyValueMetadata 写入文件级 key/value 元信息，可为空。
	KeyValueMetadata map[string]string
}

// compressionCodec 把配置里的压缩名映射成上游 codec；空字符串等同 snappy。
func compressionCodec(name string) (compress.Codec, error) {
	switch name {
	case "", "snappy":
		return &pq.Snappy, nil
	case "zstd":
		return &pq.Zstd, nil
	case "gzip":
		return &pq.Gzip, nil
	case "lz4":
		return &pq.Lz4Raw, nil
	case "none", "uncompressed":
		return &pq.Uncompressed, nil
	default:
		return nil, fmt.Errorf("%w: 未知的压缩方式 %q", ErrInvalidOption, name)
	}
}

// quote 给列名/取值加双引号，避免错误文本里出现歧义。
func quote(s string) string { return strconv.Quote(s) }

// joinColumns 把列路径拼成逗号分隔的文本。
func joinColumns(cols []string) string { return strings.Join(cols, ", ") }
