package excel

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// writeFileAtomic 在目标目录写临时文件，成功后重命名到 path。
//
// 失败时删除临时文件；成功后的文件权限为 0644。
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

// NewBuilder 创建一个写入 io.Writer 的工作簿 Builder。
//
// w 为 nil 时返回 ErrInvalidColumn。Builder 不会在 Close 前向 w 写任何东西。
func NewBuilder(w io.Writer) (*Builder, error) {
	if w == nil {
		return nil, fmt.Errorf("%w: 输出 writer 为 nil", ErrInvalidColumn)
	}
	return &Builder{
		target: w,
		file:   excelize.NewFile(),
	}, nil
}

// NewFileBuilder 创建一个写入文件路径的 Builder。
//
// 文件在 Close 时原子写出（临时文件 + rename，权限 0644）；Abort 不产出文件。
func NewFileBuilder(path string) (*Builder, error) {
	if path == "" {
		return nil, fmt.Errorf("%w: 输出路径为空", ErrInvalidColumn)
	}
	return &Builder{
		path: path,
		file: excelize.NewFile(),
	}, nil
}

// check 在写操作前确认 Builder 可用。
func (b *Builder) check() error {
	if b.closed {
		return ErrClosed
	}
	if b.failed != nil {
		return b.failed
	}
	return nil
}

// fail 保存第一次失败的错误。
func (b *Builder) fail(err error) {
	if b.failed == nil {
		b.failed = err
	}
}

// AddSheet 添加一个工作表并返回流式写出句柄。
//
// name 必须非空且符合 Excel 规则；重复（大小写不敏感）返回 ErrSheetExists，
// 非法名返回 ErrInvalidSheetName，列声明无效返回 ErrInvalidColumn。这些
// 校验失败不会污染 Builder。若已有活动 sheet，会先 Close 它。Builder 已关闭
// 或失败时返回 ErrClosed/原错误。
func (b *Builder) AddSheet(name string, opts WriteOptions) (*SheetWriter, error) {
	if err := b.check(); err != nil {
		return nil, err
	}
	cols, err := opts.prepare()
	if err != nil {
		return nil, err
	}
	if err := validateSheetName(name); err != nil {
		return nil, err
	}
	for _, existing := range b.added {
		if strings.EqualFold(existing, name) {
			return nil, fmt.Errorf("%w: %s", ErrSheetExists, name)
		}
	}

	if b.active != nil {
		if err := b.active.Close(); err != nil {
			return nil, err
		}
	}

	if b.sheets == 0 {
		if name != "Sheet1" {
			if err := b.file.SetSheetName("Sheet1", name); err != nil {
				b.fail(err)
				return nil, err
			}
		}
	} else {
		if _, err := b.file.NewSheet(name); err != nil {
			b.fail(err)
			return nil, err
		}
	}

	stream, err := b.file.NewStreamWriter(name)
	if err != nil {
		b.fail(err)
		return nil, err
	}

	styleID := 0
	if opts.HeaderBold {
		if !b.styleOk {
			b.style, b.styleErr = b.file.NewStyle(&excelize.Style{
				Font: &excelize.Font{Bold: true},
			})
			b.styleOk = true
		}
		if b.styleErr != nil {
			b.fail(b.styleErr)
			return nil, b.styleErr
		}
		styleID = b.style
	}

	for i, col := range cols {
		if col.Width > 0 {
			if err := stream.SetColWidth(i+1, i+1, col.Width); err != nil {
				b.fail(err)
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
			b.fail(err)
			return nil, err
		}
	}

	rowNum := 1
	if !opts.NoHeader {
		values := make([]any, len(cols))
		for i, col := range cols {
			header := col.Header
			if header == "" {
				header = col.Key
			}
			if styleID != 0 {
				values[i] = excelize.Cell{StyleID: styleID, Value: header}
			} else {
				values[i] = header
			}
		}
		if err := stream.SetRow("A1", values); err != nil {
			b.fail(err)
			return nil, err
		}
		rowNum = 2
	}

	sw := &SheetWriter{
		builder: b,
		stream:  stream,
		columns: cols,
		rowNum:  rowNum,
	}
	b.active = sw
	b.sheets++
	b.added = append(b.added, name)
	return sw, nil
}

// AddSheetRows 添加一个工作表并批量写入 map 行，成功后关闭该 sheet。
//
// 等价于 AddSheet + 逐行 Write + Close；错误同 AddSheet/SheetWriter.Write。
// 任一行失败会让 Builder 进入失败态，调用方应 Abort 或 Close 以丢弃半成品。
func (b *Builder) AddSheetRows(name string, rows []map[string]string, opts WriteOptions) error {
	sw, err := b.AddSheet(name, opts)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := sw.Write(row); err != nil {
			return err
		}
	}
	return sw.Close()
}

// validateSheetName 与 Excelize checkSheetName 等价，并补齐重名检查所需的前提。
func validateSheetName(name string) error {
	if name == "" {
		return fmt.Errorf("%w: 名称为空", ErrInvalidSheetName)
	}
	if cellLength(name) > 31 {
		return fmt.Errorf("%w: %q 超过 31 个字符", ErrInvalidSheetName, name)
	}
	if strings.HasPrefix(name, "'") || strings.HasSuffix(name, "'") {
		return fmt.Errorf("%w: %q 首尾不能是单引号", ErrInvalidSheetName, name)
	}
	if strings.ContainsAny(name, ":\\/?*[]") {
		return fmt.Errorf("%w: %q 含非法字符", ErrInvalidSheetName, name)
	}
	return nil
}

// Write 追加一行。
//
// 缺 Key、单元格超长、SetRow 失败返回 *RowError，并让 Builder 进入失败态。
// sheet 已关闭或 Builder 已关闭时返回 ErrClosed；Builder 已失败时返回原错误。
func (s *SheetWriter) Write(row map[string]string) error {
	if s.closed {
		return ErrClosed
	}
	if err := s.builder.check(); err != nil {
		return err
	}
	values := make([]any, len(s.columns))
	for i, col := range s.columns {
		value, ok := row[col.Key]
		if !ok {
			err := &RowError{Row: s.rowNum, Column: col.Key, Err: ErrMissingKey}
			s.builder.fail(err)
			return err
		}
		if n := cellLength(value); n > excelize.TotalCellChars {
			err := &RowError{
				Row:    s.rowNum,
				Column: col.Key,
				Err:    fmt.Errorf("单元格长度 %d 超过上限 %d", n, excelize.TotalCellChars),
			}
			s.builder.fail(err)
			return err
		}
		values[i] = value
	}
	cell, err := excelize.CoordinatesToCellName(1, s.rowNum)
	if err != nil {
		re := &RowError{Row: s.rowNum, Err: err}
		s.builder.fail(re)
		return re
	}
	if err := s.stream.SetRow(cell, values); err != nil {
		re := &RowError{Row: s.rowNum, Err: err}
		s.builder.fail(re)
		return re
	}
	s.rowNum++
	return nil
}

// Close 只 Flush 当前 sheet，幂等。
//
// Flush 是终态，必须用 closed 标志保证只调用一次；Builder.Close 会自动
// 关闭未关闭的 sheet。Flush 失败时返回错误并让 Builder 进入失败态。
func (s *SheetWriter) Close() error {
	if s.closed {
		return nil
	}
	s.closed = true
	if s.builder.active == s {
		s.builder.active = nil
	}
	if s.builder.failed != nil {
		return s.builder.failed
	}
	if err := s.stream.Flush(); err != nil {
		s.builder.fail(err)
		return err
	}
	return nil
}

// Close 自动结束活动 sheet 并写出工作簿。
//
// 无 sheet 时返回 ErrEmptyWorkbook 且不关闭，允许 AddSheet 后重试；失败态
// 返回原错误且不写出；成功或 Abort 后再次 Close 返回 ErrClosed。目标为
// io.Writer 时失败可能留下部分输出，文件场景请用 NewFileBuilder。
func (b *Builder) Close() error {
	if b.closed {
		return ErrClosed
	}
	if b.failed != nil {
		b.closed = true
		if b.active != nil {
			b.active.closed = true
			b.active = nil
		}
		b.file.Close()
		return b.failed
	}
	if b.sheets == 0 {
		return ErrEmptyWorkbook
	}
	if b.active != nil {
		if err := b.active.Close(); err != nil {
			b.closed = true
			b.file.Close()
			return err
		}
	}
	b.closed = true
	if b.path != "" {
		return writeFileAtomic(b.path, b.writeTo)
	}
	return b.writeTo(b.target)
}

// Abort 丢弃工作簿、清理临时资源，不产出文件。已关闭时是 no-op。
//
// 返回底层文件清理错误；正常丢弃时通常为 nil。
func (b *Builder) Abort() error {
	if b.closed {
		return nil
	}
	b.closed = true
	if b.active != nil {
		b.active.closed = true
		b.active = nil
	}
	return b.file.Close()
}

// writeTo 把工作簿写入 w 并关闭底层文件。
func (b *Builder) writeTo(w io.Writer) error {
	if err := b.file.Write(w); err != nil {
		b.file.Close()
		return err
	}
	return b.file.Close()
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
