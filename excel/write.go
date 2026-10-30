package excel

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/xuri/excelize/v2"
)

// Write 把全部行写入一个 xlsx 并输出到 w。
//
// 列顺序取自 WriteOptions.Columns，表头取 Column.Header（为空时用 Key）。
// 每一行必须包含全部声明的 Key，缺一个就返回 *RowError（ErrMissingKey），
// 此时不会写出文件；多余键忽略。以 "=" 开头的值按文本写入，不生成公式。
//
// 这是全量入口；行数很大时用 NewWriter 流式写。
func Write(w io.Writer, rows []map[string]string, opts WriteOptions) error {
	writer, err := NewWriter(w, opts)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := writer.Write(row); err != nil {
			writer.abort()
			return err
		}
	}
	return writer.Close()
}

// WriteFile 把全部行写入 path，先写临时文件再重命名。
//
// 失败时删除临时文件，避免留下半截文件。目标文件会被覆盖。临时文件由
// os.CreateTemp 创建，最终文件权限会调整为 0644。
func WriteFile(path string, rows []map[string]string, opts WriteOptions) error {
	return writeFileAtomic(path, func(w io.Writer) error {
		return Write(w, rows, opts)
	})
}

// writeFileAtomic 在目标目录写临时文件，成功后重命名到 path。
func writeFileAtomic(path string, write func(io.Writer) error) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".excel-*.xlsx")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() {
		if tmpName != "" {
			os.Remove(tmpName)
		}
	}()

	if err := write(tmp); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		return err
	}
	tmpName = ""
	return nil
}

// Writer 是流式写出器：按调用顺序追加行，Close 时提交。
//
// Writer 不是并发安全的。未调用 Close 时目标 io.Writer 不会收到完整文件；
// Write 失败后应丢弃 Writer，不要继续写或 Close，否则会提交一个不完整的
// 工作簿。
type Writer struct {
	target  io.Writer
	file    *excelize.File
	stream  *excelize.StreamWriter
	columns []Column
	rowNum  int
	closed  bool
}

// NewWriter 创建工作簿并写入表头，随后可以用 Write 逐行追加。
//
// 表头、列宽、冻结与样式在构造时一次完成；列宽必须在写行之前设置，因此
// 不支持自动列宽。返回的错误来自选项校验或工作簿装配。
func NewWriter(w io.Writer, opts WriteOptions) (*Writer, error) {
	if w == nil {
		return nil, fmt.Errorf("%w: 输出 writer 为 nil", ErrInvalidColumn)
	}
	if err := opts.validate(); err != nil {
		return nil, err
	}
	cols, err := normalizeColumns(opts.Columns)
	if err != nil {
		return nil, err
	}

	sheet := opts.Sheet
	if sheet == "" {
		sheet = "Sheet1"
	}
	f := excelize.NewFile()
	if sheet != "Sheet1" {
		if err := f.SetSheetName("Sheet1", sheet); err != nil {
			f.Close()
			return nil, err
		}
	}
	stream, err := f.NewStreamWriter(sheet)
	if err != nil {
		f.Close()
		return nil, err
	}

	headerStyle := 0
	if opts.HeaderBold {
		headerStyle, err = f.NewStyle(&excelize.Style{Font: &excelize.Font{Bold: true}})
		if err != nil {
			f.Close()
			return nil, err
		}
	}
	for i, col := range cols {
		if col.Width > 0 {
			if err := stream.SetColWidth(i+1, i+1, col.Width); err != nil {
				f.Close()
				return nil, err
			}
		}
	}
	if opts.FreezeHeader && !opts.NoHeader {
		if err := stream.SetPanes(&excelize.Panes{
			Freeze:      true,
			YSplit:      1,
			TopLeftCell: "A2",
			ActivePane:  "bottomLeft",
		}); err != nil {
			f.Close()
			return nil, err
		}
	}

	writer := &Writer{target: w, file: f, stream: stream, columns: cols, rowNum: 1}
	if !opts.NoHeader {
		values := make([]any, len(cols))
		for i, col := range cols {
			header := col.Header
			if header == "" {
				header = col.Key
			}
			if headerStyle != 0 {
				values[i] = excelize.Cell{StyleID: headerStyle, Value: header}
			} else {
				values[i] = header
			}
		}
		if err := stream.SetRow("A1", values); err != nil {
			writer.abort()
			return nil, err
		}
		writer.rowNum = 2
	}
	return writer, nil
}

// Write 追加一行。
//
// 返回 *RowError 时该行没有被写入（所有声明 Key 在写行之前已检查完）；
// Close 之后再调用返回普通错误。
func (w *Writer) Write(row map[string]string) error {
	if w.closed {
		return errors.New("excel: writer 已关闭")
	}
	values := make([]any, len(w.columns))
	for i, col := range w.columns {
		value, ok := row[col.Key]
		if !ok {
			return &RowError{Row: w.rowNum, Column: col.Key, Err: ErrMissingKey}
		}
		if n := cellLength(value); n > excelize.TotalCellChars {
			return &RowError{
				Row:    w.rowNum,
				Column: col.Key,
				Err:    fmt.Errorf("单元格长度 %d 超过上限 %d", n, excelize.TotalCellChars),
			}
		}
		values[i] = value
	}
	cell, err := excelize.CoordinatesToCellName(1, w.rowNum)
	if err != nil {
		return &RowError{Row: w.rowNum, Err: err}
	}
	if err := w.stream.SetRow(cell, values); err != nil {
		return &RowError{Row: w.rowNum, Err: err}
	}
	w.rowNum++
	return nil
}

// Close 提交工作簿并写入目标 io.Writer。
//
// 只能调用一次；重复调用返回错误。Close 之后不能再 Write。
func (w *Writer) Close() error {
	if w.closed {
		return errors.New("excel: writer 已关闭")
	}
	w.closed = true
	if err := w.stream.Flush(); err != nil {
		w.file.Close()
		return err
	}
	if err := w.file.Write(w.target); err != nil {
		w.file.Close()
		return err
	}
	return w.file.Close()
}

// abort 释放临时资源但不输出文件，供全量 Write 失败时使用。
func (w *Writer) abort() {
	w.closed = true
	w.file.Close()
}

// cellLength 按 Excel 的口径统计字符数：BMP 之外的字符占 2 个 UTF-16 单元。
func cellLength(s string) int {
	n := 0
	for _, r := range s {
		if r > 0xFFFF {
			n += 2
		} else {
			n++
		}
	}
	return n
}
