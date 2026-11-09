package dsv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Writer 是 map 层的文本写出器。
//
// 用 NewWriter/NewFileWriter 创建；构造期校验选项并写出表头（NoHeader 为假时），
// 之后用 Write 逐行追加，Close 提交，Abort 放弃。任一行失败后进入失败态：
// 后续 Write 返回第一次的错误，Close 返回该错误。Writer 不是并发安全的。
type Writer struct {
	bw      *bufio.Writer
	columns []Column
	opts    WriteOptions
	line    int
	failed  error
	closed  bool
	file    *os.File
	tmp     string
	target  string
}

// prepare 校验写出选项并返回规范化后的列声明。
func (o WriteOptions) prepare() ([]Column, error) {
	if err := o.checkBase(); err != nil {
		return nil, err
	}
	cols, err := normalizeColumns(o.Columns, false, false)
	if err != nil {
		return nil, err
	}
	headers := make(map[string]struct{}, len(cols))
	for _, c := range cols {
		h := c.Header
		if h == "" {
			h = c.Key
		}
		if _, ok := headers[h]; ok {
			return nil, fmt.Errorf("%w: 写出的表头 %q 重复", ErrDuplicateColumn, h)
		}
		headers[h] = struct{}{}
	}
	return cols, nil
}

// NewWriter 创建写到 w 的 map 层写出器。
//
// opts 必须声明 Delimiter 与 Columns；构造期会写出表头（NoHeader 为假时）。
// 选项/列声明非法返回 ErrInvalidOption/ErrInvalidColumn/ErrDuplicateColumn。
// 写到 io.Writer 时无法回滚已写出的内容，失败由调用方处理；需要原子落盘时
// 使用 NewFileWriter。
func NewWriter(w io.Writer, opts WriteOptions) (*Writer, error) {
	cols, err := opts.prepare()
	if err != nil {
		return nil, err
	}
	return newWriter(w, opts, cols)
}

// NewFileWriter 创建原子写 path 的 map 层写出器。
//
// 写出过程中数据落在同目录临时文件，Close 成功后 chmod 0644 并 rename 到
// path；写入失败或 Abort 时删除临时文件，path 原内容保持不变。
func NewFileWriter(path string, opts WriteOptions) (*Writer, error) {
	cols, err := opts.prepare()
	if err != nil {
		return nil, err
	}
	return newFileWriter(path, opts, cols)
}

// newFileWriter 创建临时文件并初始化写出器。
func newFileWriter(path string, opts WriteOptions, cols []Column) (*Writer, error) {
	file, err := os.CreateTemp(filepath.Dir(path), ".dsv-*")
	if err != nil {
		return nil, err
	}
	w, err := newWriter(file, opts, cols)
	if err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	w.file = file
	w.tmp = file.Name()
	w.target = path
	return w, nil
}

// newWriter 完成两个构造入口共用的初始化，并按需写出表头。
func newWriter(out io.Writer, opts WriteOptions, cols []Column) (*Writer, error) {
	w := &Writer{
		bw:      bufio.NewWriter(out),
		columns: cols,
		opts:    opts,
		line:    1,
	}
	if opts.NoHeader {
		return w, nil
	}
	headers := make([]string, len(cols))
	for i, c := range cols {
		headers[i] = c.Header
		if headers[i] == "" {
			headers[i] = c.Key
		}
	}
	if err := w.writeFields(headers, headers); err != nil {
		w.failed = err
		return nil, err
	}
	w.line++
	return w, nil
}

// Write 按 Columns 顺序追加一条记录。
//
// 缺少任一 Key 返回 *RowError（ErrMissingKey）；字段在 Sanitize 之后仍含
// 分隔符或换行返回 *RowError（ErrIllegalField）。任一行失败后 Writer 进入
// 失败态；Close 或 Abort 后调用返回 ErrClosed。
func (w *Writer) Write(row map[string]string) error {
	if w.closed {
		return ErrClosed
	}
	if w.failed != nil {
		return w.failed
	}
	fields := make([]string, len(w.columns))
	keys := make([]string, len(w.columns))
	for i, c := range w.columns {
		value, ok := row[c.Key]
		if !ok {
			err := &RowError{Line: w.line, Column: c.Key, Err: ErrMissingKey}
			w.failed = err
			return err
		}
		fields[i] = value
		keys[i] = c.Key
	}
	if err := w.writeFields(fields, keys); err != nil {
		w.failed = err
		return err
	}
	w.line++
	return nil
}

// Close 提交写出，幂等。
//
// 文件句柄会把临时文件 rename 到目标路径；失败态或刷新失败时删除临时文件并
// 返回错误，清理错误会与原错误合并，目标文件保持不变。重复调用返回 nil。
func (w *Writer) Close() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.failed != nil {
		return errors.Join(w.failed, w.cleanup())
	}
	if err := w.bw.Flush(); err != nil {
		return errors.Join(err, w.cleanup())
	}
	if w.file == nil {
		return nil
	}
	if err := w.file.Chmod(0o644); err != nil {
		return errors.Join(err, w.cleanup())
	}
	file := w.file
	w.file = nil
	if err := file.Close(); err != nil {
		return errors.Join(err, w.removeTemp())
	}
	if err := os.Rename(w.tmp, w.target); err != nil {
		return errors.Join(err, w.removeTemp())
	}
	return nil
}

// Abort 放弃写出并删除临时文件，幂等。
//
// 已存在的目标文件保持不变；删除失败时返回该错误（可用 errors.Is 判断）。
// Abort 之后 Write 返回 ErrClosed。
func (w *Writer) Abort() error {
	if w.closed {
		return nil
	}
	w.closed = true
	return w.cleanup()
}

// cleanup 关闭并删除临时文件，幂等；返回关闭与删除的错误。
func (w *Writer) cleanup() error {
	if w.file == nil {
		return nil
	}
	file := w.file
	w.file = nil
	return errors.Join(file.Close(), w.removeTemp())
}

// removeTemp 删除临时文件；文件已不存在时视为成功。
func (w *Writer) removeTemp() error {
	if w.tmp == "" {
		return nil
	}
	if err := os.Remove(w.tmp); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// writeFields 对字段做替换与非法字符检查，拼接后写入缓冲。
//
// columns 仅用于错误定位（map 层是 Key，表头行是表头文本）。
func (w *Writer) writeFields(fields, columns []string) error {
	for i, value := range fields {
		if w.opts.Sanitize != nil {
			value = w.opts.Sanitize(value)
		}
		if strings.Contains(value, w.opts.Delimiter) || strings.ContainsAny(value, "\r\n") {
			return &RowError{Line: w.line, Column: columns[i], Err: ErrIllegalField}
		}
		fields[i] = value
	}
	line := strings.Join(fields, w.opts.Delimiter)
	if w.opts.TrailingDelimiter {
		line += w.opts.Delimiter
	}
	line += w.lineEnding()
	_, err := w.bw.WriteString(line)
	return err
}

// lineEnding 返回实际使用的行尾序列，默认 \n。
func (w *Writer) lineEnding() string {
	if w.opts.LineEnding == "" {
		return "\n"
	}
	return w.opts.LineEnding
}

// WriteFile 把 map 行原子写入 path。
//
// 语义与 NewFileWriter + Write + Close 相同；任一行失败时放弃临时文件并返回
// 错误，path 原内容保持不变。
func WriteFile(path string, rows []map[string]string, opts WriteOptions) error {
	w, err := NewFileWriter(path, opts)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return errors.Join(err, w.Abort())
		}
	}
	return w.Close()
}
