package excel_test

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/excel"
	"github.com/xuri/excelize/v2"
)

// TestSniffFormat 覆盖格式嗅探的接受与拒绝分支。
func TestSniffFormat(t *testing.T) {
	ok := xlsxBytes(t, func(f *excelize.File) { f.SetCellValue("Sheet1", "A1", "x") })

	cases := []struct {
		name string
		data []byte
		want error
	}{
		{"xlsx", ok, nil},
		{"xls", []byte{0xD0, 0xCF, 0x11, 0xE0, 0xA1, 0xB1, 0x1A, 0xE1, 0, 0}, excel.ErrUnsupportedFormat},
		{"csv", []byte("a,b\n1,2\n"), excel.ErrUnsupportedFormat},
		{"空流", nil, excel.ErrUnsupportedFormat},
	}
	for _, c := range cases {
		err := excel.SniffFormatForTest(bufio.NewReader(bytes.NewReader(c.data)))
		t.Logf("%s: err=%v", c.name, err)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: err=%v, want %v", c.name, err, c.want)
		}
	}
}

// TestSniffDoesNotConsume 确认嗅探使用 Peek，不消耗调用方数据。
func TestSniffDoesNotConsume(t *testing.T) {
	original := []byte("not an excel file at all")
	br := bufio.NewReader(bytes.NewReader(original))
	if err := excel.SniffFormatForTest(br); !errors.Is(err, excel.ErrUnsupportedFormat) {
		t.Fatalf("err=%v", err)
	}
	rest, _ := br.Peek(br.Buffered())
	t.Logf("嗅探后剩余 %d 字节:%q", len(rest), rest)
	if !bytes.Equal(rest, original) {
		t.Fatalf("reader 被消耗: %q", rest)
	}
}

// TestOpenWorkbookFileFormat 确认扩展名与内容双重校验。
func TestOpenWorkbookFileFormat(t *testing.T) {
	dir := t.TempDir()
	zipData := xlsxBytes(t, func(f *excelize.File) { f.SetCellValue("Sheet1", "A1", "x") })

	// 扩展名是 .xls，内容即使是 zip 也拒绝。
	xlsPath := filepath.Join(dir, "fake.xls")
	if err := os.WriteFile(xlsPath, zipData, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := excel.OpenWorkbookFileForTest(xlsPath); !errors.Is(err, excel.ErrUnsupportedFormat) {
		t.Fatalf(".xls 扩展名: err=%v", err)
	}

	// 扩展名是 .xlsx，内容不是 zip 也拒绝。
	fakePath := filepath.Join(dir, "fake.xlsx")
	if err := os.WriteFile(fakePath, []byte("plain text"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := excel.OpenWorkbookFileForTest(fakePath); !errors.Is(err, excel.ErrUnsupportedFormat) {
		t.Fatalf("伪造 .xlsx: err=%v", err)
	}

	// 正常文件可以打开。
	goodPath := filepath.Join(dir, "good.xlsx")
	if err := os.WriteFile(goodPath, zipData, 0o600); err != nil {
		t.Fatal(err)
	}
	f, err := excel.OpenWorkbookFileForTest(goodPath)
	if err != nil {
		t.Fatal(err)
	}
	f.Close()
}

// TestResolveSheet 覆盖默认工作表与不存在的工作表。
func TestResolveSheet(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetName("Sheet1", "数据")
		f.SetCellValue("数据", "A1", "x")
	})
	f, err := excel.OpenWorkbookForTest(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	got, err := excel.ResolveSheetForTest(f, "")
	t.Logf("默认工作表: %q err=%v", got, err)
	if err != nil || got != "数据" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if got, err = excel.ResolveSheetForTest(f, "数据"); err != nil || got != "数据" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if _, err = excel.ResolveSheetForTest(f, "不存在"); !errors.Is(err, excel.ErrSheetNotFound) {
		t.Fatalf("err=%v", err)
	}
}

// TestOpenWorkbookCorrupt 确认 ZIP 魔数正确但内容损坏时也归为不支持。
func TestOpenWorkbookCorrupt(t *testing.T) {
	corrupt := append([]byte{'P', 'K', 0x03, 0x04}, []byte(strings.Repeat("x", 64))...)
	_, err := excel.OpenWorkbookForTest(bytes.NewReader(corrupt))
	if !errors.Is(err, excel.ErrUnsupportedFormat) {
		t.Fatalf("err=%v", err)
	}
}

// TestReadHeader 覆盖按表头读取、Key 重命名、多余列忽略与空单元格。
func TestReadHeader(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"订单号", "金额", "忽略"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"A001", "12.5", "x"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"A002", "", "y"})
	})
	rows, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{Columns: []excel.Column{
		{Header: "订单号", Key: "order_no"},
		{Header: "金额", Key: "amount"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rows=%v", rows)
	if len(rows) != 2 {
		t.Fatalf("rows=%d", len(rows))
	}
	if rows[0]["order_no"] != "A001" || rows[1]["amount"] != "" {
		t.Fatalf("rows=%v", rows)
	}
	if _, ok := rows[0]["忽略"]; ok {
		t.Fatalf("未声明的列不应出现: %v", rows[0])
	}
}

// TestReadMissingColumns 确认一次报出所有缺失列。
func TestReadMissingColumns(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"A", "B"})
	})
	_, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{Columns: []excel.Column{
		{Header: "A"}, {Header: "C"}, {Header: "D"},
	}})
	t.Logf("err=%v", err)
	if !errors.Is(err, excel.ErrMissingColumn) {
		t.Fatalf("err=%v", err)
	}
	if !strings.Contains(err.Error(), "C") || !strings.Contains(err.Error(), "D") {
		t.Fatalf("错误未列出全部缺失列: %v", err)
	}
}

// TestReadDuplicateFileHeader 确认声明列在文件表头中出现多次时报错。
func TestReadDuplicateFileHeader(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"A", "A"})
	})
	_, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{Columns: []excel.Column{{Header: "A"}}})
	if !errors.Is(err, excel.ErrDuplicateColumn) {
		t.Fatalf("err=%v", err)
	}
}

// TestReadSkipRows 确认标题行可以被跳过。
func TestReadSkipRows(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"月度报表"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"生成时间：2026-10-30"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"订单号", "金额"})
		f.SetSheetRow("Sheet1", "A4", &[]any{"A001", "9"})
	})
	rows, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{
		SkipRows: 2,
		Columns:  []excel.Column{{Header: "订单号", Key: "no"}, {Header: "金额", Key: "amt"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rows=%v", rows)
	if len(rows) != 1 || rows[0]["no"] != "A001" {
		t.Fatalf("rows=%v", rows)
	}
}

// TestReadTrimAndBOM 确认表头始终清洗，数据由 TrimSpace 控制。
func TestReadTrimAndBOM(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"\ufeff 订单号 "})
		f.SetSheetRow("Sheet1", "A2", &[]any{"  A001  "})
	})
	col := excel.Column{Header: "订单号", Key: "no"}

	rows, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{Columns: []excel.Column{col}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("未 trim: %q", rows[0]["no"])
	if rows[0]["no"] != "  A001  " {
		t.Fatalf("got=%q", rows[0]["no"])
	}

	rows, err = excel.Read(bytes.NewReader(data), excel.ReadOptions{Columns: []excel.Column{col}, TrimSpace: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("trim: %q", rows[0]["no"])
	if rows[0]["no"] != "A001" {
		t.Fatalf("got=%q", rows[0]["no"])
	}
}

// TestReadRaggedRow 确认行比表头短时补空字符串，不报错。
func TestReadRaggedRow(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"A", "B"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"only-a"})
	})
	rows, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{Columns: []excel.Column{{Header: "A"}, {Header: "B"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rows=%v", rows)
	if rows[0]["B"] != "" {
		t.Fatalf("rows=%v", rows)
	}
}

// TestReadEmptySheetAndMissingSheet 覆盖空表与不存在的工作表。
func TestReadEmptySheetAndMissingSheet(t *testing.T) {
	empty := xlsxBytes(t, func(f *excelize.File) {})
	_, err := excel.Read(bytes.NewReader(empty), excel.ReadOptions{Columns: []excel.Column{{Header: "A"}}})
	if !errors.Is(err, excel.ErrEmptySheet) {
		t.Fatalf("empty: %v", err)
	}
	_, err = excel.Read(bytes.NewReader(empty), excel.ReadOptions{Sheet: "不存在", Columns: []excel.Column{{Header: "A"}}})
	if !errors.Is(err, excel.ErrSheetNotFound) {
		t.Fatalf("missing sheet: %v", err)
	}
}

// TestReadFile 覆盖路径读取。
func TestReadFile(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"h"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"v"})
	})
	path := filepath.Join(t.TempDir(), "in.xlsx")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	rows, err := excel.ReadFile(path, excel.ReadOptions{Columns: []excel.Column{{Header: "h"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rows=%v", rows)
	if len(rows) != 1 || rows[0]["h"] != "v" {
		t.Fatalf("rows=%v", rows)
	}
}

// TestReadNoHeader 覆盖无表头模式按位置对应。
func TestReadNoHeader(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"A001", "12.5"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"A002", "13"})
	})
	rows, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{
		NoHeader: true,
		Columns:  []excel.Column{{Key: "order_no"}, {Key: "amount"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rows=%v", rows)
	if len(rows) != 2 || rows[0]["order_no"] != "A001" || rows[1]["amount"] != "13" {
		t.Fatalf("rows=%v", rows)
	}
}

// TestReadNoHeaderWidthMismatch 覆盖声明列数与实际列宽严格相等。
func TestReadNoHeaderWidthMismatch(t *testing.T) {
	// 文件 3 列，声明 2 列 → 更宽。
	wide := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"a", "b", "c"})
	})
	_, err := excel.Read(bytes.NewReader(wide), excel.ReadOptions{
		NoHeader: true,
		Columns:  []excel.Column{{Key: "x"}, {Key: "y"}},
	})
	t.Logf("更宽: err=%v", err)
	if !errors.Is(err, excel.ErrColumnMismatch) {
		t.Fatalf("wide: %v", err)
	}

	// 文件有效宽度 1 列（尾部空列已被裁剪），声明 2 列 → 更窄。
	narrow := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"a", "", ""})
	})
	_, err = excel.Read(bytes.NewReader(narrow), excel.ReadOptions{
		NoHeader: true,
		Columns:  []excel.Column{{Key: "x"}, {Key: "y"}},
	})
	t.Logf("更窄: err=%v", err)
	if !errors.Is(err, excel.ErrColumnMismatch) {
		t.Fatalf("narrow: %v", err)
	}
}

// TestReadNoHeaderEmptySheet 确认空工作表在无表头模式下返回空结果。
func TestReadNoHeaderEmptySheet(t *testing.T) {
	empty := xlsxBytes(t, func(f *excelize.File) {})
	rows, err := excel.Read(bytes.NewReader(empty), excel.ReadOptions{
		NoHeader: true,
		Columns:  []excel.Column{{Key: "x"}},
	})
	t.Logf("rows=%v err=%v", rows, err)
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

// TestReadTypedRaw 覆盖日期列必须取原始序列号。
func TestReadTypedRaw(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"日期"})
		f.SetCellValue("Sheet1", "A2", 45292)
	})
	col := excel.Column{Header: "日期", Key: "date", Type: excel.Date, Layout: "2006-01-02"}

	// 未开 RawValues：装配期报错。
	if _, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{Columns: []excel.Column{col}}); !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("未开 RawValues: %v", err)
	}

	rows, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{Columns: []excel.Column{col}, RawValues: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rows=%v", rows)
	if rows[0]["date"] != "2024-01-01" {
		t.Fatalf("rows=%v", rows)
	}
}

// TestRead1904 确认工作簿的 1904 日期系统被读取。
func TestRead1904(t *testing.T) {
	enabled := true
	data := xlsxBytes(t, func(f *excelize.File) {
		if err := f.SetWorkbookProps(&excelize.WorkbookPropsOptions{Date1904: &enabled}); err != nil {
			t.Fatal(err)
		}
		f.SetSheetRow("Sheet1", "A1", &[]any{"日期"})
		f.SetCellValue("Sheet1", "A2", 45292)
	})
	rows, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{
		RawValues: true,
		Columns:   []excel.Column{{Header: "日期", Key: "date", Type: excel.Date, Layout: "2006-01-02"}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rows=%v", rows)
	if rows[0]["date"] != "2028-01-02" {
		t.Fatalf("rows=%v", rows)
	}
}

// TestReadSparseTyped 确认空值短路类型解析，Required 单独报错。
func TestReadSparseTyped(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"单号", "金额"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"A1", ""})
		f.SetSheetRow("Sheet1", "A3", &[]any{"A2", "42"})
	})
	base := []excel.Column{
		{Header: "单号", Key: "no"},
		{Header: "金额", Key: "amount", Type: excel.Int},
	}

	rows, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{Columns: base})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("Required=false rows=%v", rows)
	if len(rows) != 2 || rows[0]["amount"] != "" || rows[1]["amount"] != "42" {
		t.Fatalf("rows=%v", rows)
	}

	required := append([]excel.Column(nil), base...)
	required[1].Required = true
	_, err = excel.Read(bytes.NewReader(data), excel.ReadOptions{Columns: required})
	var rowErr *excel.RowError
	t.Logf("Required=true err=%v", err)
	if !errors.As(err, &rowErr) || rowErr.Row != 2 || rowErr.Column != "amount" {
		t.Fatalf("err=%v", err)
	}
}

// TestReadTypedError 确认解析失败带行号与列名。
func TestReadTypedError(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"金额"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"abc"})
	})
	_, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{
		Columns: []excel.Column{{Header: "金额", Key: "amount", Type: excel.Int}},
	})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) || rowErr.Row != 2 || rowErr.Column != "amount" || !strings.Contains(err.Error(), "abc") {
		t.Fatalf("err=%v", err)
	}
}

// TestReadBoolRaw 确认原始布尔值 "1"/"0" 可被 Bool 接受。
func TestReadBoolRaw(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"有效"})
		f.SetCellValue("Sheet1", "A2", true)
		f.SetCellValue("Sheet1", "A3", false)
	})
	rows, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{
		RawValues: true,
		Columns:   []excel.Column{{Header: "有效", Key: "ok", Type: excel.Bool}},
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rows=%v", rows)
	if rows[0]["ok"] != "true" || rows[1]["ok"] != "false" {
		t.Fatalf("rows=%v", rows)
	}
}

// TestReadEachStop 确认 ErrStop 提前结束且不报错。
func TestReadEachStop(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"h"})
		for i := 2; i <= 5; i++ {
			f.SetCellValue("Sheet1", "A"+fmt.Sprint(i), i)
		}
	})
	var seen []int
	err := excel.ReadEach(bytes.NewReader(data), excel.ReadOptions{Columns: []excel.Column{{Header: "h"}}},
		func(rowNum int, row map[string]string) error {
			seen = append(seen, rowNum)
			if len(seen) == 2 {
				return excel.ErrStop
			}
			return nil
		})
	t.Logf("seen=%v err=%v", seen, err)
	if err != nil || fmt.Sprint(seen) != "[2 3]" {
		t.Fatalf("seen=%v err=%v", seen, err)
	}
}

// TestReadValidateErrorHasRow 确认业务校验错误带物理行号。
func TestReadValidateErrorHasRow(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"h"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"ok"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"bad"})
	})
	_, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{
		Columns: []excel.Column{{Header: "h"}},
		Validate: func(rowNum int, row map[string]string) error {
			if row["h"] == "bad" {
				return errors.New("业务校验失败")
			}
			return nil
		},
	})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) || rowErr.Row != 3 {
		t.Fatalf("err=%v", err)
	}
}

// TestReadRowNumbersWithGaps 确认文件中间缺行时行号仍是物理行号。
func TestReadRowNumbersWithGaps(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetCellValue("Sheet1", "A1", "h")
		f.SetCellValue("Sheet1", "A3", "r3")
		f.SetCellValue("Sheet1", "A5", "r5")
	})
	var seen []int
	err := excel.ReadEach(bytes.NewReader(data), excel.ReadOptions{Columns: []excel.Column{{Header: "h"}}},
		func(rowNum int, row map[string]string) error {
			seen = append(seen, rowNum)
			return nil
		})
	t.Logf("seen=%v err=%v", seen, err)
	if err != nil || fmt.Sprint(seen) != "[3 5]" {
		t.Fatalf("seen=%v err=%v", seen, err)
	}
}

// TestReadStopReturnsCollected 确认 Read 遇到 ErrStop 返回已收集的行。
func TestReadStopReturnsCollected(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"h"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"r1"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"r2"})
	})
	rows, err := excel.Read(bytes.NewReader(data), excel.ReadOptions{
		Columns: []excel.Column{{Header: "h"}},
		Validate: func(rowNum int, row map[string]string) error {
			if row["h"] == "r2" {
				return excel.ErrStop
			}
			return nil
		},
	})
	t.Logf("rows=%v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0]["h"] != "r1" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

// TestReadEachFile 确认文件路径流式入口可用。
func TestReadEachFile(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"h"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"v1"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"v2"})
	})
	path := filepath.Join(t.TempDir(), "big.xlsx")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	var seen []string
	err := excel.ReadEachFile(path, excel.ReadOptions{Columns: []excel.Column{{Header: "h"}}},
		func(rowNum int, row map[string]string) error {
			seen = append(seen, row["h"])
			return nil
		})
	t.Logf("seen=%v err=%v", seen, err)
	if err != nil || fmt.Sprint(seen) != "[v1 v2]" {
		t.Fatalf("seen=%v err=%v", seen, err)
	}
}
