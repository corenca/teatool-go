package excel_test

import (
	"bytes"
	"errors"
	"testing"
	"time"

	"github.com/corenca/teatool-go/excel"
	"github.com/xuri/excelize/v2"
)

// order 是结构体层的主要测试类型。
type order struct {
	No     string    `excel:"订单号"`
	Amount float64   `excel:"金额"`
	Date   time.Time `excel:"日期,layout:2006-01-02"`
	Note   *string   `excel:"备注"`
	Skip   string    `excel:"-"`
}

// TestStructRoundTrip 覆盖结构体写出后读回。
func TestStructRoundTrip(t *testing.T) {
	note := "hello"
	in := []order{{
		No:     "A001",
		Amount: 12.5,
		Date:   time.Date(2024, 1, 30, 0, 0, 0, 0, time.UTC),
		Note:   &note,
		Skip:   "不应出现",
	}}

	var buf bytes.Buffer
	if err := excel.WriteFrom(&buf, in, excel.WriteOptions{HeaderBold: true}); err != nil {
		t.Fatal(err)
	}
	sheet := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", sheet)
	if len(sheet[0]) != 4 || sheet[0][0] != "订单号" || sheet[0][3] != "备注" {
		t.Fatalf("表头=%v", sheet[0])
	}

	out, err := excel.ReadInto[order](bytes.NewReader(buf.Bytes()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("in=%+v out=%+v", in, out)
	if len(out) != 1 {
		t.Fatalf("out=%+v", out)
	}
	got := out[0]
	if got.No != "A001" || got.Amount != 12.5 || got.Date.Format("2006-01-02") != "2024-01-30" {
		t.Fatalf("out=%+v", got)
	}
	if got.Note == nil || *got.Note != "hello" {
		t.Fatalf("Note=%v", got.Note)
	}
	if got.Skip != "" {
		t.Fatalf("Skip=%q", got.Skip)
	}
}

// TestStructRawDate 覆盖真实日期单元格（原始序列号）的读取。
func TestStructRawDate(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"日期"})
		f.SetCellValue("Sheet1", "A2", 45292)
	})
	type row struct {
		Date time.Time `excel:"日期,layout:2006-01-02"`
	}
	out, err := excel.ReadInto[row](bytes.NewReader(data), excel.ReadOptions{RawValues: true})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("out=%+v", out)
	if out[0].Date.Format("2006-01-02") != "2024-01-01" {
		t.Fatalf("out=%+v", out)
	}
}

// TestStructColumnsRejected 确认结构体层不接受 Columns，且在打开文件前报错。
func TestStructColumnsRejected(t *testing.T) {
	_, err := excel.ReadInto[order](nil, excel.ReadOptions{Columns: []excel.Column{{Header: "x"}}})
	t.Logf("err=%v", err)
	if !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("err=%v", err)
	}
}

// TestStructUnsupportedType 确认不支持的字段类型在装配期报错。
func TestStructUnsupportedType(t *testing.T) {
	type bad struct {
		X []string `excel:"x"`
	}
	var buf bytes.Buffer
	err := excel.WriteFrom(&buf, []bad{{}}, excel.WriteOptions{})
	t.Logf("err=%v", err)
	if !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("err=%v", err)
	}
}

// TestStructRequired 确认 required 字段为空时报行级错误。
func TestStructRequired(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"x", "y"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"", "keep"})
	})
	type req struct {
		X string `excel:"x,required"`
	}
	_, err := excel.ReadInto[req](bytes.NewReader(data), excel.ReadOptions{})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) || rowErr.Row != 2 || rowErr.Column != "x" {
		t.Fatalf("err=%v", err)
	}
}

// TestStructPointers 确认空单元格映射为 nil，非空映射为指针。
func TestStructPointers(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"x", "备注"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"a", ""})
		f.SetSheetRow("Sheet1", "A3", &[]any{"b", "hi"})
	})
	type noteRow struct {
		Note *string `excel:"备注"`
	}
	out, err := excel.ReadInto[noteRow](bytes.NewReader(data), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("out=%v %v", out[0].Note, *out[1].Note)
	if out[0].Note != nil || out[1].Note == nil || *out[1].Note != "hi" {
		t.Fatalf("out=%+v", out)
	}
}

// TestStructValidateInterface 确认 Validate() error 在每行后调用。
func TestStructValidateInterface(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"x"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"ok"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"bad"})
	})
	_, err := excel.ReadInto[validatedRow](bytes.NewReader(data), excel.ReadOptions{})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) || rowErr.Row != 3 {
		t.Fatalf("err=%v", err)
	}
}

// validatedRow 用于验证 Validate 接口。
type validatedRow struct {
	X string `excel:"x"`
}

func (v validatedRow) Validate() error {
	if v.X == "bad" {
		return errors.New("值不能为 bad")
	}
	return nil
}

// TestStructWidthTag 确认 width tag 生效。
func TestStructWidthTag(t *testing.T) {
	type wide struct {
		X string `excel:"x,width:20"`
	}
	var buf bytes.Buffer
	if err := excel.WriteFrom(&buf, []wide{{X: "1"}}, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	width, err := f.GetColWidth("Sheet1", "A")
	t.Logf("width=%v err=%v", width, err)
	if err != nil || width != 20 {
		t.Fatalf("width=%v err=%v", width, err)
	}
}

// TestStructNoTagAndUint 确认无 tag 用字段名，uint64 全范围可读。
func TestStructNoTagAndUint(t *testing.T) {
	type number struct {
		Value uint64
	}
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"Value"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"18446744073709551615"})
	})
	out, err := excel.ReadInto[number](bytes.NewReader(data), excel.ReadOptions{})
	t.Logf("out=%+v err=%v", out, err)
	if err != nil || out[0].Value != 18446744073709551615 {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

// TestWriteFileFrom 确认结构体写文件后可读回。
func TestWriteFileFrom(t *testing.T) {
	path := t.TempDir() + "/out.xlsx"
	note := "n"
	rows := []order{{No: "A001", Amount: 1, Date: time.Date(2024, 1, 30, 0, 0, 0, 0, time.UTC), Note: &note}}
	if err := excel.WriteFileFrom(path, rows, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	out, err := excel.ReadFileInto[order](path, excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("out=%+v", out)
	if len(out) != 1 || out[0].No != "A001" {
		t.Fatalf("out=%+v", out)
	}
}

// TestStructNumericTextDate 确认纯数字文本日期按 layout 解析，不被当成序列号。
func TestStructNumericTextDate(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"日期"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"20240130"})
	})
	type row struct {
		Date time.Time `excel:"日期,layout:20060102"`
	}
	out, err := excel.ReadInto[row](bytes.NewReader(data), excel.ReadOptions{})
	t.Logf("out=%+v err=%v", out, err)
	if err != nil || out[0].Date.Format("2006-01-02") != "2024-01-30" {
		t.Fatalf("out=%+v err=%v", out, err)
	}
}

// TestStructSkipTagWithOptions 确认 excel:"-,xxx" 仍表示跳过。
func TestStructSkipTagWithOptions(t *testing.T) {
	type row struct {
		Keep string `excel:"keep"`
		Skip string `excel:"-,required"`
	}
	var buf bytes.Buffer
	if err := excel.WriteFrom(&buf, []row{{Keep: "k", Skip: "s"}}, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	sheet := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", sheet)
	if len(sheet[0]) != 1 || sheet[0][0] != "keep" {
		t.Fatalf("sheet=%v", sheet)
	}
}

// allTypes 覆盖结构体层支持的全部标量类型。
type allTypes struct {
	S   string    `excel:"s"`
	B   bool      `excel:"b"`
	I8  int8      `excel:"i8"`
	I64 int64     `excel:"i64"`
	U32 uint32    `excel:"u32"`
	U64 uint64    `excel:"u64"`
	F32 float32   `excel:"f32"`
	F64 float64   `excel:"f64"`
	T   time.Time `excel:"t,layout:2006-01-02 15:04:05"`
}

// TestStructAllTypesRoundTrip 覆盖各类型写出后读回。
func TestStructAllTypesRoundTrip(t *testing.T) {
	in := allTypes{
		S: "文本", B: true, I8: -8, I64: -1 << 40,
		U32: 1 << 31, U64: 1<<63 + 1, F32: 1.5, F64: -2.25,
		T: time.Date(2024, 1, 30, 12, 34, 56, 0, time.UTC),
	}
	var buf bytes.Buffer
	if err := excel.WriteFrom(&buf, []allTypes{in}, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	out, err := excel.ReadInto[allTypes](bytes.NewReader(buf.Bytes()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("in=%+v out=%+v", in, out[0])
	if out[0] != in {
		t.Fatalf("往返不一致\nin =%+v\nout=%+v", in, out[0])
	}
}
