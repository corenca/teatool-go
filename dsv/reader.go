package dsv

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"unicode"
)

// utf8BOM 是 UTF-8 文件可能带的字节序标记，只从第一物理行剥离。
const utf8BOM = "\xEF\xBB\xBF"

// Read 读取 r 中的全部记录，返回以 Column.Key 为键的 map 切片。
//
// opts 必须声明 Delimiter 与 Columns；有表头时按 Header 匹配，NoHeader 时按位置
// 对应。任何一条记录出错都会返回错误（此时结果为 nil），行级错误是 *RowError。
// 配置 OnRowError 后改为容错读取：坏行跳过并交给处理器，返回的切片只含成功行，
// 整体 error 为 nil。大文件请用 Each，避免把所有记录留在内存。
func Read(r io.Reader, opts ReadOptions) ([]map[string]string, error) {
	var rows []map[string]string
	err := Each(r, opts, func(_ int, row map[string]string) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// Each 逐行读取 r 并回调，不把全部记录留在内存。
//
// fn 收到的 row 以 Column.Key 为键，值已经过类型规范化与 Required 校验；
// line 是物理行号（1-based，含表头与跳过行）。fn 返回 ErrStop 表示成功提前
// 结束，其他错误会被包装成 *RowError。fn 不能为 nil。
//
// opts 非法返回 ErrInvalidOption/ErrInvalidColumn；表头缺列返回
// ErrMissingColumn，表头重复返回 ErrDuplicateColumn。配置 OnRowError 后，
// 行级错误会跳过该行并继续，详见 ReadOptions.OnRowError。
func Each(r io.Reader, opts ReadOptions, fn func(line int, row map[string]string) error) error {
	cols, err := opts.prepare()
	if err != nil {
		return err
	}
	if fn == nil {
		return fmt.Errorf("%w: 回调为 nil", ErrInvalidOption)
	}
	return eachRecord(r, opts, cols, func(line int, fields []string) error {
		row := make(map[string]string, len(cols))
		problems := make([]fieldProblem, 0, len(cols))
		for i, c := range cols {
			raw := fields[i]
			if raw == "" && c.Required {
				problems = append(problems, fieldProblem{column: c.Key, err: errors.New("必填字段为空")})
				continue
			}
			value, err := normalizeValue(c, raw)
			if err != nil {
				problems = append(problems, fieldProblem{column: c.Key, err: err})
				continue
			}
			row[c.Key] = value
		}
		if err := joinFieldErrors(line, problems); err != nil {
			return err
		}
		if opts.Validate != nil {
			if err := opts.Validate(line, row); err != nil {
				if errors.Is(err, ErrStop) {
					return ErrStop
				}
				if _, ok := errors.AsType[*RowError](err); ok {
					return err
				}
				return &RowError{Line: line, Err: err}
			}
		}
		return fn(line, row)
	})
}

// ReadFile 打开 path 并读取全部记录，文件由本函数负责关闭。
//
// 语义与 Read 相同；打开、读取与关闭错误会用 errors.Join 合并返回。
func ReadFile(path string, opts ReadOptions) ([]map[string]string, error) {
	var rows []map[string]string
	err := EachFile(path, opts, func(_ int, row map[string]string) error {
		rows = append(rows, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return rows, nil
}

// EachFile 打开 path 并逐行回调，文件由本函数负责关闭。
//
// 语义与 Each 相同；打开、读取与关闭错误会用 errors.Join 合并返回。
func EachFile(path string, opts ReadOptions, fn func(line int, row map[string]string) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(Each(f, opts, fn), f.Close())
}

// lineReader 逐物理行读取，处理 BOM 与 \r\n。
//
// 使用 bufio.Reader.ReadString 而不是 bufio.Scanner，避免默认 64KB 的行长上限。
type lineReader struct {
	br    *bufio.Reader
	line  int
	first bool
}

// newLineReader 包装 r 并标记首行（唯一会剥离 BOM 的行）。
func newLineReader(r io.Reader) *lineReader {
	return &lineReader{br: bufio.NewReader(r), first: true}
}

// next 返回下一物理行（不含换行），ok 为 false 表示到达文件末尾。
// 行尾接受 \n 与 \r\n；最后一行没有换行也照常返回。
func (s *lineReader) next() (string, bool, error) {
	raw, err := s.br.ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false, err
	}
	if len(raw) == 0 {
		return "", false, nil
	}
	s.line++
	if s.first {
		s.first = false
		raw = strings.TrimPrefix(raw, utf8BOM)
	}
	raw = strings.TrimSuffix(raw, "\n")
	raw = strings.TrimSuffix(raw, "\r")
	if err != nil && !errors.Is(err, io.EOF) {
		return "", false, err
	}
	return raw, true, nil
}

// eachRecord 完成跳过行、表头解析、切分、宽度校验与列重排。
//
// 交给 fn 的 fields 与 cols 等长且一一对应；line 是当前物理行号。
func eachRecord(r io.Reader, opts ReadOptions, cols []Column, fn func(line int, fields []string) error) error {
	lr := newLineReader(r)

	for range opts.SkipRows {
		if _, ok, err := lr.next(); err != nil {
			return err
		} else if !ok {
			if opts.NoHeader {
				return nil
			}
			return ErrEmptyFile
		}
	}

	width := len(cols)
	positions := make([]int, len(cols))
	if opts.NoHeader {
		for i := range positions {
			positions[i] = i
		}
	} else {
		header := ""
		for {
			raw, ok, err := lr.next()
			if err != nil {
				return err
			}
			if !ok {
				return ErrEmptyFile
			}
			if raw == "" {
				continue
			}
			header = raw
			break
		}
		fields, err := splitRecord(header, opts, true)
		if err != nil {
			return &RowError{Line: lr.line, Text: header, Err: err}
		}
		width = len(fields)
		if err := mapHeader(cols, fields, positions); err != nil {
			return err
		}
	}

	for {
		raw, ok, err := lr.next()
		if err != nil {
			return err
		}
		if !ok {
			return nil
		}
		if raw == "" {
			continue
		}
		fields, err := splitRecord(raw, opts, opts.TrimSpace)
		if err != nil {
			rowErr := &RowError{Line: lr.line, Err: err}
			if herr := opts.handleRowError(lr.line, raw, rowErr); herr != nil {
				return herr
			}
			continue
		}
		if len(fields) != width {
			rowErr := &RowError{Line: lr.line, Err: fmt.Errorf(
				"%w: 需要 %d 个字段，实际 %d 个", ErrColumnMismatch, width, len(fields))}
			if herr := opts.handleRowError(lr.line, raw, rowErr); herr != nil {
				return herr
			}
			continue
		}
		picked := make([]string, len(cols))
		for i, p := range positions {
			picked[i] = fields[p]
		}
		if err := fn(lr.line, picked); err != nil {
			if errors.Is(err, ErrStop) {
				return nil
			}
			if rowErr, ok := errors.AsType[*RowError](err); ok {
				if herr := opts.handleRowError(lr.line, raw, rowErr); herr != nil {
					return herr
				}
				continue
			}
			return &RowError{Line: lr.line, Text: raw, Err: err}
		}
	}
}

// handleRowError 补齐行级错误的行号与原文，并按 OnRowError 决定继续或中止。
//
// 返回 nil 表示错误已交给调用方处理、可以继续读下一行；否则是要向上返回的
// 错误。未配置 OnRowError 时返回补全后的错误，保持快速失败语义。
func (o ReadOptions) handleRowError(line int, text string, rowErr *RowError) error {
	if rowErr.Line == 0 || rowErr.Text == "" {
		clone := *rowErr
		if clone.Line == 0 {
			clone.Line = line
		}
		if clone.Text == "" {
			clone.Text = text
		}
		rowErr = &clone
	}
	if o.OnRowError == nil {
		return rowErr
	}
	if err := o.OnRowError(rowErr); err != nil {
		return err
	}
	return nil
}

// mapHeader 建立「声明列 → 记录位置」映射。
//
// 表头文本已去空白；精确匹配，区分大小写。重复的声明表头返回
// ErrDuplicateColumn，缺失返回 ErrMissingColumn。
func mapHeader(cols []Column, header []string, positions []int) error {
	index := make(map[string]int, len(header))
	duplicate := make(map[string]bool)
	for i, h := range header {
		if _, ok := index[h]; ok {
			duplicate[h] = true
			continue
		}
		index[h] = i
	}
	for i, c := range cols {
		p, ok := index[c.Header]
		if !ok {
			return fmt.Errorf("%w: %q", ErrMissingColumn, c.Header)
		}
		if duplicate[c.Header] {
			return fmt.Errorf("%w: 表头 %q", ErrDuplicateColumn, c.Header)
		}
		positions[i] = p
	}
	return nil
}

// splitRecord 按选项切分一条记录。
//
// trim 为 true 时先去行尾空白（避免行末空格导致末尾分隔符校验失败），
// 切分后再去每个字段首尾空白。声明了 TrailingDelimiter 时，行必须以分隔符
// 结尾，且只去掉一个；缺少时返回 ErrIllegalField，由调用方补充行号。
func splitRecord(line string, opts ReadOptions, trim bool) ([]string, error) {
	if opts.TrailingDelimiter {
		matched := strings.HasSuffix(line, opts.Delimiter)
		if !matched && trim {
			// 行尾空白可能跟在分隔符后面；从后往前找「分隔符 + 仅空白尾巴」
			// 的位置，避免裁掉分隔符自身的尾部空白。
			for i := strings.LastIndex(line, opts.Delimiter); i >= 0; i = strings.LastIndex(line[:i], opts.Delimiter) {
				if strings.TrimSpace(line[i+len(opts.Delimiter):]) == "" {
					line = line[:i+len(opts.Delimiter)]
					matched = true
					break
				}
			}
		}
		if !matched {
			return nil, fmt.Errorf("%w: 行末尾缺少分隔符 %q", ErrIllegalField, opts.Delimiter)
		}
		line = strings.TrimSuffix(line, opts.Delimiter)
	} else if trim {
		line = strings.TrimRightFunc(line, unicode.IsSpace)
	}
	fields := strings.Split(line, opts.Delimiter)
	if trim {
		for i := range fields {
			fields[i] = strings.TrimSpace(fields[i])
		}
	}
	return fields, nil
}
