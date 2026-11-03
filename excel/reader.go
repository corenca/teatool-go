package excel

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/xuri/excelize/v2"
)

// OLE2 复合文档魔数，旧版 .xls 与加密的 OOXML 都以它开头。
var ole2Magic = []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1}

// ZIP 容器魔数。xlsx/xlsm/xltx/xltm 都是 ZIP 包。
var zipMagics = [][]byte{
	{'P', 'K', 0x03, 0x04}, // 普通 ZIP
	{'P', 'K', 0x05, 0x06}, // 空 ZIP
	{'P', 'K', 0x07, 0x08}, // 分卷 ZIP
}

// sniffFormat 检查前 8 字节，只接受 ZIP 容器。
//
// 使用 Peek 而不是 Read，不消耗调用方的 reader，方便随后交给 Excelize。
// 旧版 .xls 会得到带说明的 ErrUnsupportedFormat。
func sniffFormat(br *bufio.Reader) error {
	header, err := br.Peek(8)
	if err != nil {
		return fmt.Errorf("%w: 文件头不足 8 字节", ErrUnsupportedFormat)
	}
	if bytes.HasPrefix(header, ole2Magic) {
		return fmt.Errorf("%w: .xls 或加密工作簿不支持，请先转存为 xlsx", ErrUnsupportedFormat)
	}
	for _, magic := range zipMagics {
		if bytes.HasPrefix(header, magic) {
			return nil
		}
	}
	return fmt.Errorf("%w: 仅支持 xlsx/xlsm/xltx/xltm", ErrUnsupportedFormat)
}

// openWorkbook 从流打开工作簿。
//
// 注意：Excelize 的 OpenReader 会把整个流读入内存；大文件请用 ReadFile，
// 它走 OpenFile 路径。
func openWorkbook(r io.Reader) (*excelize.File, error) {
	br := bufio.NewReader(r)
	if err := sniffFormat(br); err != nil {
		return nil, err
	}
	f, err := excelize.OpenReader(br)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
	}
	return f, nil
}

// openWorkbookFile 从路径打开工作簿，先看扩展名再按魔数确认。
func openWorkbookFile(path string) (*excelize.File, error) {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".xls":
		return nil, fmt.Errorf("%w: .xls 不支持，请先转存为 xlsx", ErrUnsupportedFormat)
	case ".csv":
		return nil, fmt.Errorf("%w: csv 不支持，请先转存为 xlsx", ErrUnsupportedFormat)
	}

	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	if err := sniffFormat(bufio.NewReader(file)); err != nil {
		return nil, err
	}
	f, err := excelize.OpenFile(path)
	if err != nil {
		return nil, fmt.Errorf("%w: %w", ErrUnsupportedFormat, err)
	}
	return f, nil
}

// Open 从流打开工作簿。
//
// 注意：Excelize 的 OpenReader 会把整个流读入内存；大文件请用 OpenFile。
// 返回的 Reader 必须 Close。
func Open(r io.Reader) (*Reader, error) {
	f, err := openWorkbook(r)
	if err != nil {
		return nil, err
	}
	return &Reader{file: f}, nil
}

// OpenFile 从文件路径打开工作簿，不把整个文件读入内存。
func OpenFile(path string) (*Reader, error) {
	f, err := openWorkbookFile(path)
	if err != nil {
		return nil, err
	}
	return &Reader{file: f}, nil
}

// resolveSheet 返回规范工作表名：空名取第一个，否则按 Excelize 的大小写
// 不敏感规则查找并返回工作簿里的名字。
func resolveSheet(f *excelize.File, name string) (string, error) {
	if name == "" {
		first := f.GetSheetName(0)
		if first == "" {
			return "", ErrEmptySheet
		}
		return first, nil
	}
	idx, err := f.GetSheetIndex(name)
	if err != nil || idx < 0 {
		return "", fmt.Errorf("%w: %s", ErrSheetNotFound, name)
	}
	canonical := f.GetSheetName(idx)
	if canonical == "" {
		return "", fmt.Errorf("%w: %s", ErrSheetNotFound, name)
	}
	return canonical, nil
}

// Close 释放底层工作簿。幂等；关闭后读取操作返回 ErrClosed。
func (r *Reader) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.file.Close()
}

// check 在读取前确认 Reader 尚未关闭。
func (r *Reader) check() error {
	if r.closed {
		return ErrClosed
	}
	return nil
}

// Sheets 按工作簿顺序返回全部 sheet 条目。
//
// 关闭后返回 ErrClosed；读取可见性失败时返回底层错误。
func (r *Reader) Sheets() ([]SheetInfo, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	names := r.file.GetSheetList()
	infos := make([]SheetInfo, 0, len(names))
	for _, name := range names {
		idx, err := r.file.GetSheetIndex(name)
		if err != nil {
			return nil, err
		}
		visible, err := r.file.GetSheetVisible(name)
		if err != nil {
			return nil, err
		}
		infos = append(infos, SheetInfo{Name: name, Index: idx, Visible: visible})
	}
	return infos, nil
}

// Sheet 返回指定工作表的读取句柄；name 为空时取第一个。
//
// 关闭后返回 ErrClosed；工作簿没有 sheet 返回 ErrEmptySheet；名字不存在
// 返回 ErrSheetNotFound。
func (r *Reader) Sheet(name string) (*SheetReader, error) {
	if err := r.check(); err != nil {
		return nil, err
	}
	sheet, err := resolveSheet(r.file, name)
	if err != nil {
		return nil, err
	}
	idx, err := r.file.GetSheetIndex(sheet)
	if err != nil {
		return nil, err
	}
	visible, err := r.file.GetSheetVisible(sheet)
	if err != nil {
		return nil, err
	}
	return &SheetReader{
		reader: r,
		name:   sheet,
		info:   SheetInfo{Name: sheet, Index: idx, Visible: visible},
	}, nil
}

// date1904For 按需读取并缓存工作簿的 1904 日期系统标记。
func (r *Reader) date1904For(need bool) (bool, error) {
	if !need {
		return false, nil
	}
	if !r.propsLoaded {
		props, err := r.file.GetWorkbookProps()
		if err != nil {
			return false, err
		}
		r.date1904 = props.Date1904 != nil && *props.Date1904
		r.propsLoaded = true
	}
	return r.date1904, nil
}

// Info 返回创建句柄时缓存的工作表信息，Reader 关闭后仍可读取。
func (s *SheetReader) Info() SheetInfo { return s.info }

// Read 读取整张工作表并返回按行排列的结果。
//
// 每个 map 的键来自 Column.Key；声明之外的多余列忽略，声明的列在表头中
// 不存在时返回 ErrMissingColumn。空行会被跳过，但行号继续推进。
// 校验回调返回 ErrStop 时返回已收集的行与 nil；Reader 已关闭时返回 ErrClosed。
// 行级错误以 *RowError 返回。
func (s *SheetReader) Read(opts ReadOptions) ([]map[string]string, error) {
	var out []map[string]string
	err := s.read(opts, false, func(_ int, row map[string]string, _ bool) error {
		out = append(out, row)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// Each 逐行读取并回调，不把全部数据行保留在内存。
//
// rowNum 是 Excel 物理行号（1-based，含表头与跳过行）；返回 ErrStop 表示
// 成功提前结束，其他错误会以 *RowError 包装；Reader 已关闭时返回 ErrClosed。
func (s *SheetReader) Each(opts ReadOptions, fn func(rowNum int, row map[string]string) error) error {
	return s.read(opts, false, func(rowNum int, row map[string]string, _ bool) error {
		return fn(rowNum, row)
	})
}

// read 是 Read/Each 的公共实现。
func (s *SheetReader) read(opts ReadOptions, forceDate1904 bool, fn rowFunc) error {
	if err := s.reader.check(); err != nil {
		return err
	}
	cols, err := prepareRead(opts)
	if err != nil {
		return err
	}
	need := forceDate1904 || hasDateType(cols)
	date1904, err := s.reader.date1904For(need)
	if err != nil {
		return err
	}
	return readRows(s.reader.file, s.name, opts, cols, date1904, fn)
}

// hasDateType 判断列声明里是否有需要 1904 标记的日期类型。
func hasDateType(cols []Column) bool {
	for _, c := range cols {
		if isDateType(c.Type) {
			return true
		}
	}
	return false
}

// rowFunc 是内部行回调，额外携带工作簿的 1904 日期系统标记。
type rowFunc func(rowNum int, row map[string]string, date1904 bool) error

// prepareRead 校验选项并返回填充过 Key 的列声明。
func prepareRead(opts ReadOptions) ([]Column, error) {
	return opts.prepare()
}

// readRows 逐行读取并交给 fn，负责表头解析、空行跳过与行号维护。
func readRows(f *excelize.File, sheet string, opts ReadOptions, cols []Column, date1904 bool, fn rowFunc) (err error) {
	rows, err := f.Rows(sheet)
	if err != nil {
		return err
	}
	defer func() {
		if cerr := rows.Close(); cerr != nil {
			err = errors.Join(err, cerr)
		}
	}()

	rowNum := 0
	for i := 0; i < opts.SkipRows; i++ {
		if !rows.Next() {
			return ErrEmptySheet
		}
		rowNum++
	}

	indexes := make([]int, len(cols))
	if opts.NoHeader {
		for i := range indexes {
			indexes[i] = i
		}
	} else {
		if !rows.Next() {
			return ErrEmptySheet
		}
		rowNum++
		cells, cErr := rows.Columns()
		if cErr != nil {
			return cErr
		}
		resolved, rErr := resolveHeader(cells, cols)
		if rErr != nil {
			return rErr
		}
		indexes = resolved
	}

	sawData, maxWidth := false, 0
	for rows.Next() {
		rowNum++
		cells, cErr := rows.Columns(excelize.Options{RawCellValue: opts.RawValues})
		if cErr != nil {
			return cErr
		}
		if opts.TrimSpace {
			for i := range cells {
				cells[i] = strings.TrimSpace(cells[i])
			}
		}
		if opts.NoHeader {
			if len(cells) > len(cols) {
				return fmt.Errorf("%w: 第 %d 行有 %d 列，声明 %d 列", ErrColumnMismatch, rowNum, len(cells), len(cols))
			}
			if len(cells) > maxWidth {
				maxWidth = len(cells)
			}
		}
		if isBlankRow(cells) {
			continue
		}
		sawData = true

		row := make(map[string]string, len(cols))
		for i, c := range cols {
			value := ""
			if idx := indexes[i]; idx >= 0 && idx < len(cells) {
				value = cells[idx]
			}
			if value == "" {
				if c.Required {
					return &RowError{Row: rowNum, Column: c.Key, Err: errors.New("必填列为空")}
				}
				row[c.Key] = ""
				continue
			}
			converted, convErr := convertValue(c, value, date1904)
			if convErr != nil {
				return &RowError{Row: rowNum, Column: c.Key, Err: convErr}
			}
			row[c.Key] = converted
		}
		if opts.Validate != nil {
			if vErr := opts.Validate(rowNum, row); vErr != nil {
				if errors.Is(vErr, ErrStop) {
					return nil
				}
				if _, ok := errors.AsType[*RowError](vErr); ok {
					return vErr
				}
				return &RowError{Row: rowNum, Err: vErr}
			}
		}
		if fErr := fn(rowNum, row, date1904); fErr != nil {
			if errors.Is(fErr, ErrStop) {
				return nil
			}
			if _, ok := errors.AsType[*RowError](fErr); ok {
				return fErr
			}
			return &RowError{Row: rowNum, Err: fErr}
		}
	}
	if err := rows.Error(); err != nil {
		return err
	}
	if opts.NoHeader && sawData && maxWidth < len(cols) {
		return fmt.Errorf("%w: 文件最宽行 %d 列，声明 %d 列", ErrColumnMismatch, maxWidth, len(cols))
	}
	return nil
}

// resolveHeader 把声明的表头映射到单元格下标。
func resolveHeader(cells []string, cols []Column) ([]int, error) {
	index := make(map[string][]int, len(cells))
	for i, cell := range cells {
		name := cleanHeader(cell)
		if name == "" {
			continue
		}
		index[name] = append(index[name], i)
	}

	indexes := make([]int, len(cols))
	var missing []string
	for i, c := range cols {
		switch idxs := index[c.Header]; {
		case len(idxs) == 0:
			missing = append(missing, c.Header)
			indexes[i] = -1
		case len(idxs) > 1:
			return nil, fmt.Errorf("%w: 表头 %q 出现 %d 次", ErrDuplicateColumn, c.Header, len(idxs))
		default:
			indexes[i] = idxs[0]
		}
	}
	if len(missing) > 0 {
		return nil, fmt.Errorf("%w: %s", ErrMissingColumn, strings.Join(missing, ", "))
	}
	return indexes, nil
}

// cleanHeader 去掉 UTF-8 BOM 与首尾空白。
func cleanHeader(s string) string {
	return strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "\ufeff"))
}

// isBlankRow 判断一行是否所有单元格都为空。
func isBlankRow(cells []string) bool {
	for _, cell := range cells {
		if cell != "" {
			return false
		}
	}
	return true
}
