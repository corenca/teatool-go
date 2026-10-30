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

// resolveSheet 返回要操作的工作表名：空名取第一个，否则校验存在性。
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
	return name, nil
}

// Read 读取整张工作表并返回按行排列的结果。
//
// 每个 map 的键来自 Column.Key；声明之外的多余列忽略，声明的列在表头中
// 不存在时返回 ErrMissingColumn。空行会被跳过，但行号继续推进。
// 读取过程中的行级错误以 *RowError 返回；校验回调返回 ErrStop 时返回已经
// 收集到的行与 nil。
func Read(r io.Reader, opts ReadOptions) ([]map[string]string, error) {
	return readAll(func() (*excelize.File, error) { return openWorkbook(r) }, opts)
}

// ReadFile 从文件路径读取整张工作表。
//
// 与 Read 的区别是走 Excelize 的 OpenFile 路径，不把整个文件读入内存；
// 大文件优先使用本函数。扩展名与文件魔数都会校验。
func ReadFile(path string, opts ReadOptions) ([]map[string]string, error) {
	return readAll(func() (*excelize.File, error) { return openWorkbookFile(path) }, opts)
}

// ReadEach 逐行读取并回调，不把全部数据行保留在内存。
//
// 注意：io.Reader 入口沿用 Excelize 的 OpenReader，它会把整个流读入内存；
// 真正的大文件请用 ReadEachFile。fn 收到的 rowNum 是 Excel 物理行号
// （1-based，含表头与跳过行）；返回 ErrStop 表示成功提前结束，返回其他错误
// 会以 *RowError 包装。每次回调的 map 都是新分配的，回调返回后归调用方所有。
func ReadEach(r io.Reader, opts ReadOptions, fn func(rowNum int, row map[string]string) error) error {
	return readEach(func() (*excelize.File, error) { return openWorkbook(r) }, opts, fn)
}

// ReadEachFile 从文件路径逐行读取，是大文件的首选入口。
//
// 走 Excelize 的 OpenFile 路径，文件不整体读入内存；其余语义同 ReadEach。
func ReadEachFile(path string, opts ReadOptions, fn func(rowNum int, row map[string]string) error) error {
	return readEach(func() (*excelize.File, error) { return openWorkbookFile(path) }, opts, fn)
}

// readAll 收集 ReadEach 的结果。
func readAll(open func() (*excelize.File, error), opts ReadOptions) ([]map[string]string, error) {
	var out []map[string]string
	if err := readEach(open, opts, func(_ int, row map[string]string) error {
		out = append(out, row)
		return nil
	}); err != nil {
		return nil, err
	}
	return out, nil
}

// rowFunc 是内部行回调，额外携带工作簿的 1904 日期系统标记。
type rowFunc func(rowNum int, row map[string]string, date1904 bool) error

// readEach 打开工作簿并交给 readRows。
func readEach(open func() (*excelize.File, error), opts ReadOptions, fn func(int, map[string]string) error) error {
	return readEachRows(open, opts, func(rowNum int, row map[string]string, _ bool) error {
		return fn(rowNum, row)
	})
}

// readEachRows 与 readEach 相同，但把 1904 标记传给回调。
func readEachRows(open func() (*excelize.File, error), opts ReadOptions, fn rowFunc) error {
	cols, err := prepareRead(opts)
	if err != nil {
		return err
	}
	f, err := open()
	if err != nil {
		return err
	}
	defer f.Close()

	sheet, err := resolveSheet(f, opts.Sheet)
	if err != nil {
		return err
	}
	props, err := f.GetWorkbookProps()
	if err != nil {
		return err
	}
	date1904 := props.Date1904 != nil && *props.Date1904
	return readRows(f, sheet, opts, cols, date1904, fn)
}

// prepareRead 校验选项并返回填充过 Key 的列声明。
func prepareRead(opts ReadOptions) ([]Column, error) {
	if err := opts.validate(); err != nil {
		return nil, err
	}
	return normalizeColumns(opts.Columns)
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
			if opts.TrimSpace {
				value = strings.TrimSpace(value)
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
