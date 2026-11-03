package excel_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/excel"
	"github.com/xuri/excelize/v2"
)

// TestWriteBasic 覆盖列顺序、表头、多余键忽略与尾部空单元格。
func TestWriteBasic(t *testing.T) {
	var buf bytes.Buffer
	err := writeAll(t, &buf, "", []map[string]string{
		{"order_no": "A001", "amount": "12.5", "extra": "忽略"},
		{"order_no": "A002", "amount": ""},
	}, excel.WriteOptions{Columns: []excel.Column{
		{Header: "订单号", Key: "order_no"},
		{Header: "金额", Key: "amount"},
	}})
	if err != nil {
		t.Fatal(err)
	}
	got := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", got)
	if len(got) != 3 || got[0][0] != "订单号" || got[1][0] != "A001" || got[2][0] != "A002" {
		t.Fatalf("got=%v", got)
	}
	// 尾部空单元格会被 GetRows 裁掉，用 GetCellValue 单独确认。
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if v, _ := f.GetCellValue("Sheet1", "B3"); v != "" {
		t.Fatalf("B3=%q", v)
	}
	if _, present := rowHasCell(got[2], "金额"); present {
		t.Fatalf("不应出现金额列: %v", got[2])
	}
}

// TestWriteMissingKey 确认缺声明键时报错且不输出文件。
func TestWriteMissingKey(t *testing.T) {
	var buf bytes.Buffer
	err := writeAll(t, &buf, "", []map[string]string{{"a": "1"}}, excel.WriteOptions{
		Columns: []excel.Column{{Key: "a"}, {Key: "b"}},
	})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) || !errors.Is(err, excel.ErrMissingKey) || rowErr.Column != "b" || rowErr.Row != 2 {
		t.Fatalf("err=%v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("失败时不应输出文件，len=%d", buf.Len())
	}
}

// TestWriteNoHeader 确认不写表头。
func TestWriteNoHeader(t *testing.T) {
	var buf bytes.Buffer
	if err := writeAll(t, &buf, "", []map[string]string{{"a": "1"}}, excel.WriteOptions{
		NoHeader: true,
		Columns:  []excel.Column{{Header: "A", Key: "a"}},
	}); err != nil {
		t.Fatal(err)
	}
	got := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", got)
	if len(got) != 1 || got[0][0] != "1" {
		t.Fatalf("got=%v", got)
	}
}

// TestWriteFormulaLikeText 确认 = 开头按文本写入。
func TestWriteFormulaLikeText(t *testing.T) {
	var buf bytes.Buffer
	if err := writeAll(t, &buf, "", []map[string]string{{"v": "=1+1"}}, excel.WriteOptions{
		Columns: []excel.Column{{Header: "值", Key: "v"}},
	}); err != nil {
		t.Fatal(err)
	}
	got := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", got)
	if got[1][0] != "=1+1" {
		t.Fatalf("got=%v", got)
	}
}

// TestWriteLayout 确认列宽、冻结首行与表头加粗。
func TestWriteLayout(t *testing.T) {
	var buf bytes.Buffer
	if err := writeAll(t, &buf, "", []map[string]string{{"a": "1"}}, excel.WriteOptions{
		Columns:      []excel.Column{{Header: "A", Key: "a", Width: 24}},
		HeaderBold:   true,
		FreezeHeader: true,
	}); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()

	width, err := f.GetColWidth("Sheet1", "A")
	if err != nil || width != 24 {
		t.Fatalf("width=%v err=%v", width, err)
	}
	panes, err := f.GetPanes("Sheet1")
	if err != nil || !panes.Freeze || panes.YSplit != 1 {
		t.Fatalf("panes=%+v err=%v", panes, err)
	}
	styleID, err := f.GetCellStyle("Sheet1", "A1")
	if err != nil {
		t.Fatal(err)
	}
	style, err := f.GetStyle(styleID)
	if err != nil || style.Font == nil || !style.Font.Bold {
		t.Fatalf("style=%+v err=%v", style, err)
	}
}

// TestWriteCellTooLong 确认超长单元格报行级错误。
func TestWriteCellTooLong(t *testing.T) {
	var buf bytes.Buffer
	err := writeAll(t, &buf, "", []map[string]string{{"v": strings.Repeat("x", excelize.TotalCellChars+1)}}, excel.WriteOptions{
		Columns: []excel.Column{{Header: "值", Key: "v"}},
	})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) || rowErr.Row != 2 || rowErr.Column != "v" {
		t.Fatalf("err=%v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("失败时不应输出文件，len=%d", buf.Len())
	}
}

// TestWriteFile 确认写文件后可被 ReadFile 读回。
func TestWriteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.xlsx")
	if err := excel.WriteFile(path, "Sheet1", []map[string]string{{"a": "1"}}, excel.WriteOptions{
		Columns: []excel.Column{{Header: "A", Key: "a"}},
	}); err != nil {
		t.Fatal(err)
	}
	rows, err := excel.ReadFile(path, "", excel.ReadOptions{Columns: []excel.Column{{Header: "A", Key: "a"}}})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("rows=%v", rows)
	if len(rows) != 1 || rows[0]["a"] != "1" {
		t.Fatalf("rows=%v", rows)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("目标文件不存在: %v", err)
	}
}

// TestSheetWriterStreaming 覆盖 SheetWriter 流式写与关闭后的行为。
func TestSheetWriterStreaming(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	sw, err := b.AddSheet("Sheet1", excel.WriteOptions{Columns: []excel.Column{{Header: "h", Key: "h"}}})
	if err != nil {
		t.Fatal(err)
	}
	for i := range 3 {
		if err := sw.Write(map[string]string{"h": string(rune('0' + i))}); err != nil {
			t.Fatal(err)
		}
	}
	if err := sw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	got := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", got)
	if len(got) != 4 {
		t.Fatalf("got=%v", got)
	}
	if err := sw.Write(map[string]string{"h": "x"}); !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("Close 后 Write err=%v, want ErrClosed", err)
	}
	if err := b.Close(); !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("重复 Close err=%v, want ErrClosed", err)
	}
}

// TestBuilderWithoutCloseWritesNothing 确认未 Close 不产生输出。
func TestBuilderWithoutCloseWritesNothing(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddSheet("Sheet1", excel.WriteOptions{Columns: []excel.Column{{Key: "h"}}}); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("未 Close 不应输出，len=%d", buf.Len())
	}
	if err := b.Abort(); err != nil {
		t.Fatal(err)
	}
}

// TestWriteSheetName 确认工作表名生效。
func TestWriteSheetName(t *testing.T) {
	var buf bytes.Buffer
	if err := writeAll(t, &buf, "数据", []map[string]string{{"a": "1"}}, excel.WriteOptions{
		Columns: []excel.Column{{Header: "A", Key: "a"}},
	}); err != nil {
		t.Fatal(err)
	}
	got := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", got)
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if f.GetSheetName(0) != "数据" {
		t.Fatalf("sheet=%q", f.GetSheetName(0))
	}
}

// rowHasCell 判断一行里是否出现指定值。
func rowHasCell(row []string, want string) (int, bool) {
	for i, v := range row {
		if v == want {
			return i, true
		}
	}
	return -1, false
}

// TestWriteFilePermissions 确认产物权限为 0644。
func TestWriteFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不支持 POSIX 权限")
	}
	path := filepath.Join(t.TempDir(), "perm.xlsx")
	if err := excel.WriteFile(path, "Sheet1", []map[string]string{{"a": "1"}}, excel.WriteOptions{
		Columns: []excel.Column{{Header: "A", Key: "a"}},
	}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("perm=%04o", info.Mode().Perm())
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Fatalf("perm=%04o, want 0644", perm)
	}
}

// writeAll 是旧 Write 的测试辅助：单 sheet 批量写。
func writeAll(t *testing.T, w io.Writer, sheet string, rows []map[string]string, opts excel.WriteOptions) error {
	t.Helper()
	if sheet == "" {
		sheet = "Sheet1"
	}
	b, err := excel.NewBuilder(w)
	if err != nil {
		return err
	}
	if err := b.AddSheetRows(sheet, rows, opts); err != nil {
		b.Abort()
		return err
	}
	return b.Close()
}
