package excel_test

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/corenca/teatool-go/excel"
	"github.com/xuri/excelize/v2"
)

// streamData 构造“订单号/金额”两列、两行数据的 xlsx。
func streamData(t *testing.T) []byte {
	t.Helper()
	return xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"订单号", "金额"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"A001", "12.5"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"A002", "3"})
	})
}

// openFirstSheet 打开内存 xlsx 的第一个工作表，并在测试结束时关闭 Reader。
func openFirstSheet(t *testing.T, data []byte) (*excel.Reader, *excel.SheetReader) {
	t.Helper()
	r, err := excel.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = r.Close() })
	s, err := r.Sheet("")
	if err != nil {
		t.Fatal(err)
	}
	return r, s
}

// TestEachIntoStreams 场景：逐行把 map 转成结构体回调，检查值与物理行号。
func TestEachIntoStreams(t *testing.T) {
	_, s := openFirstSheet(t, streamData(t))

	var got []readerOrder
	var rows []int
	err := s.EachInto[readerOrder](excel.ReadOptions{}, func(rowNum int, value readerOrder) error {
		t.Logf("row=%d value=%+v", rowNum, value)
		got = append(got, value)
		rows = append(rows, rowNum)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("got=%+v", got)
	}
	if got[0] != (readerOrder{No: "A001", Amount: 12.5}) || got[1] != (readerOrder{No: "A002", Amount: 3}) {
		t.Fatalf("got=%+v", got)
	}
	if len(rows) != 2 || rows[0] != 2 || rows[1] != 3 {
		t.Fatalf("rows=%v, want [2 3]", rows)
	}
}

// TestEachIntoStop 场景：回调返回 ErrStop 时成功提前结束，不再读取后续行。
func TestEachIntoStop(t *testing.T) {
	_, s := openFirstSheet(t, streamData(t))

	count := 0
	err := s.EachInto[readerOrder](excel.ReadOptions{}, func(rowNum int, value readerOrder) error {
		count++
		t.Logf("row=%d value=%+v", rowNum, value)
		return excel.ErrStop
	})
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if count != 1 {
		t.Fatalf("count=%d, want 1", count)
	}
}

// TestEachIntoCallbackError 场景：回调返回普通错误时包装成带行号的 *RowError。
func TestEachIntoCallbackError(t *testing.T) {
	_, s := openFirstSheet(t, streamData(t))

	sentinel := errors.New("回调失败")
	count := 0
	err := s.EachInto[readerOrder](excel.ReadOptions{}, func(rowNum int, _ readerOrder) error {
		count++
		if count == 2 {
			return sentinel
		}
		return nil
	})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) {
		t.Fatalf("err=%v, want *RowError", err)
	}
	if rowErr.Row != 3 || rowErr.Column != "" {
		t.Fatalf("rowErr=%+v, want row 3 without column", rowErr)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v, want sentinel", err)
	}
}

// TestEachIntoConversionError 场景：字段解析失败时报行号与列名，且不调用回调。
func TestEachIntoConversionError(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"金额"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"bad"})
	})
	_, s := openFirstSheet(t, data)

	type amountRow struct {
		Amount int `excel:"金额"`
	}
	count := 0
	err := s.EachInto[amountRow](excel.ReadOptions{}, func(rowNum int, value amountRow) error {
		count++
		return nil
	})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) {
		t.Fatalf("err=%v, want *RowError", err)
	}
	if rowErr.Row != 2 || rowErr.Column != "金额" {
		t.Fatalf("rowErr=%+v, want row 2 column 金额", rowErr)
	}
	if count != 0 {
		t.Fatalf("count=%d, want 0", count)
	}
}

// TestEachIntoValidateError 场景：结构体 Validate 失败时报 *RowError，坏行不回调。
func TestEachIntoValidateError(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"x"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"ok"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"bad"})
	})
	_, s := openFirstSheet(t, data)

	count := 0
	err := s.EachInto[validatedRow](excel.ReadOptions{}, func(rowNum int, value validatedRow) error {
		count++
		return nil
	})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) {
		t.Fatalf("err=%v, want *RowError", err)
	}
	if rowErr.Row != 3 {
		t.Fatalf("rowErr=%+v, want row 3", rowErr)
	}
	if count != 1 {
		t.Fatalf("count=%d, want 1", count)
	}
}

// TestEachIntoNoHeader 场景：无表头模式按位置映射结构体字段。
func TestEachIntoNoHeader(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"A001", "12.5"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"A002", "3"})
	})
	_, s := openFirstSheet(t, data)

	var got []readerOrder
	err := s.EachInto[readerOrder](excel.ReadOptions{NoHeader: true}, func(rowNum int, value readerOrder) error {
		t.Logf("row=%d value=%+v", rowNum, value)
		got = append(got, value)
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0] != (readerOrder{No: "A001", Amount: 12.5}) {
		t.Fatalf("got=%+v", got)
	}
}

// TestEachIntoColumnsRejected 场景：结构体层拒绝 opts.Columns，且不调用回调。
func TestEachIntoColumnsRejected(t *testing.T) {
	_, s := openFirstSheet(t, streamData(t))

	count := 0
	err := s.EachInto[readerOrder](excel.ReadOptions{Columns: []excel.Column{{Header: "x"}}}, func(rowNum int, value readerOrder) error {
		count++
		return nil
	})
	if !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("err=%v, want ErrInvalidColumn", err)
	}
	if count != 0 {
		t.Fatalf("count=%d, want 0", count)
	}
}

// TestEachIntoAfterClose 场景：Reader 关闭后 EachInto 返回 ErrClosed。
func TestEachIntoAfterClose(t *testing.T) {
	r, s := openFirstSheet(t, streamData(t))
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}

	err := s.EachInto[readerOrder](excel.ReadOptions{}, func(rowNum int, value readerOrder) error {
		return nil
	})
	if !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("err=%v, want ErrClosed", err)
	}
}

// TestEachIntoCallbackRowError 场景：回调返回 *RowError 时原样返回，不再二次包装。
func TestEachIntoCallbackRowError(t *testing.T) {
	_, s := openFirstSheet(t, streamData(t))

	sentinel := errors.New("自定义行错误")
	err := s.EachInto[readerOrder](excel.ReadOptions{}, func(rowNum int, value readerOrder) error {
		return &excel.RowError{Row: 99, Column: "自定义", Err: sentinel}
	})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) {
		t.Fatalf("err=%v, want *RowError", err)
	}
	if rowErr.Row != 99 || rowErr.Column != "自定义" {
		t.Fatalf("rowErr=%+v, want row 99 column 自定义", rowErr)
	}
	if !errors.Is(err, sentinel) {
		t.Fatalf("err=%v, want sentinel", err)
	}
}

// TestReadIntoStopKeepsCollected 场景：Validate 返回 ErrStop 时保留已收集的结构体。
func TestReadIntoStopKeepsCollected(t *testing.T) {
	_, s := openFirstSheet(t, streamData(t))

	rows, err := s.ReadInto[readerOrder](excel.ReadOptions{
		Validate: func(rowNum int, row map[string]string) error {
			if rowNum == 3 {
				return excel.ErrStop
			}
			return nil
		},
	})
	t.Logf("rows=%+v err=%v", rows, err)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(rows) != 1 || rows[0].No != "A001" {
		t.Fatalf("rows=%+v, want only A001", rows)
	}
}

// TestAddSheetFromStreams 场景：AddSheetFrom 返回泛型句柄，逐行写结构体后读回。
func TestAddSheetFromStreams(t *testing.T) {
	note := "hello"
	in := []order{
		{
			No: "A001", Amount: 12.5,
			Date: time.Date(2024, 1, 30, 0, 0, 0, 0, time.UTC),
			Note: &note,
		},
		{
			No: "A002", Amount: 3,
			Date: time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.AddSheetFrom[order]("订单", excel.WriteOptions{HeaderBold: true})
	if err != nil {
		t.Fatal(err)
	}
	for i := range in {
		if err := w.Write(in[i]); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	sheet := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", sheet)
	if len(sheet) != 3 || sheet[0][0] != "订单号" || sheet[0][3] != "备注" {
		t.Fatalf("sheet=%v", sheet)
	}

	out, err := readIntoTest[order](t, bytes.NewReader(buf.Bytes()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("out=%+v", out)
	if len(out) != 2 {
		t.Fatalf("out=%+v", out)
	}
	if out[0].No != "A001" || out[0].Amount != 12.5 || out[0].Note == nil || *out[0].Note != "hello" {
		t.Fatalf("out[0]=%+v", out[0])
	}
	if out[1].No != "A002" || out[1].Note != nil {
		t.Fatalf("out[1]=%+v", out[1])
	}
}

// TestAddSheetFromMultipleTypes 场景：同一个 Builder 上不同 sheet 使用不同结构体类型。
func TestAddSheetFromMultipleTypes(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	uw, err := b.AddSheetFrom[builderUser]("用户", excel.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := uw.Write(builderUser{Name: "张三", Age: 18}); err != nil {
		t.Fatal(err)
	}
	ow, err := b.AddSheetFrom[readerOrder]("订单", excel.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := ow.Write(readerOrder{No: "A001", Amount: 12.5}); err != nil {
		t.Fatal(err)
	}
	// 不调用 ow.Close，交给 Builder.Close 自动结束活动 sheet。
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := excel.Open(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	us, err := r.Sheet("用户")
	if err != nil {
		t.Fatal(err)
	}
	users, err := us.ReadInto[builderUser](excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	os, err := r.Sheet("订单")
	if err != nil {
		t.Fatal(err)
	}
	orders, err := os.ReadInto[readerOrder](excel.ReadOptions{})
	t.Logf("users=%+v orders=%+v", users, orders)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0] != (builderUser{Name: "张三", Age: 18}) {
		t.Fatalf("users=%+v", users)
	}
	if len(orders) != 1 || orders[0] != (readerOrder{No: "A001", Amount: 12.5}) {
		t.Fatalf("orders=%+v", orders)
	}
}

// TestAddSheetFromNoHeader 场景：结构体流式写支持 NoHeader，首行即数据。
func TestAddSheetFromNoHeader(t *testing.T) {
	type pair struct {
		A string `excel:"a"`
		B int    `excel:"b"`
	}
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.AddSheetFrom[pair]("A", excel.WriteOptions{NoHeader: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(pair{A: "1", B: 2}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	sheet := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", sheet)
	if len(sheet) != 1 || len(sheet[0]) != 2 || sheet[0][0] != "1" || sheet[0][1] != "2" {
		t.Fatalf("sheet=%v", sheet)
	}
}

// TestAddSheetFromAutoClose 场景：不调用 StructSheetWriter.Close，Builder.Close 自动结束。
func TestAddSheetFromAutoClose(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.AddSheetFrom[builderUser]("用户", excel.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(builderUser{Name: "张三", Age: 18}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	sheet := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", sheet)
	if len(sheet) != 2 || sheet[1][0] != "张三" {
		t.Fatalf("sheet=%v", sheet)
	}
}

// TestAddSheetFromColumnsRejected 场景：结构体层拒绝 opts.Columns，且 Builder 不被污染。
func TestAddSheetFromColumnsRejected(t *testing.T) {
	b, err := excel.NewBuilder(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.AddSheetFrom[builderUser]("用户", excel.WriteOptions{Columns: builderCols()})
	if !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("err=%v, want ErrInvalidColumn", err)
	}
	w, err := b.AddSheetFrom[builderUser]("用户", excel.WriteOptions{})
	if err != nil {
		t.Fatalf("after rejected Columns: %v", err)
	}
	if err := w.Write(builderUser{Name: "张三", Age: 18}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestAddSheetFromInvalidType 场景：字段类型不支持时装配期报错，且不污染 Builder。
func TestAddSheetFromInvalidType(t *testing.T) {
	type bad struct {
		X []string `excel:"x"`
	}
	b, err := excel.NewBuilder(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.AddSheetFrom[bad]("用户", excel.WriteOptions{})
	if !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("err=%v, want ErrInvalidColumn", err)
	}
	w, err := b.AddSheetFrom[builderUser]("用户", excel.WriteOptions{})
	if err != nil {
		t.Fatalf("after invalid type: %v", err)
	}
	if err := w.Write(builderUser{Name: "张三", Age: 18}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestAddSheetFromInvalidSheetName 场景：非法 sheet 名报错后仍可换名重试。
func TestAddSheetFromInvalidSheetName(t *testing.T) {
	b, err := excel.NewBuilder(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	_, err = b.AddSheetFrom[builderUser]("", excel.WriteOptions{})
	if !errors.Is(err, excel.ErrInvalidSheetName) {
		t.Fatalf("err=%v, want ErrInvalidSheetName", err)
	}
	w, err := b.AddSheetFrom[builderUser]("ok", excel.WriteOptions{})
	if err != nil {
		t.Fatalf("after invalid name: %v", err)
	}
	if err := w.Write(builderUser{Name: "张三", Age: 18}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestAddSheetFromAfterClose 场景：Builder 关闭后 AddSheetFrom 返回 ErrClosed。
func TestAddSheetFromAfterClose(t *testing.T) {
	b, err := excel.NewBuilder(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.AddSheetFrom[builderUser]("用户", excel.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(builderUser{Name: "张三", Age: 18}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	_, err = b.AddSheetFrom[builderUser]("其他", excel.WriteOptions{})
	if !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("err=%v, want ErrClosed", err)
	}
}

// TestStructSheetWriterWriteAfterClose 场景：StructSheetWriter 关闭后 Write 返回 ErrClosed。
func TestStructSheetWriterWriteAfterClose(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.AddSheetFrom[builderUser]("用户", excel.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(builderUser{Name: "张三", Age: 18}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Write(builderUser{Name: "李四", Age: 20}); !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("err=%v, want ErrClosed", err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestStructSheetWriterCloseTwice 场景：StructSheetWriter.Close 幂等。
func TestStructSheetWriterCloseTwice(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.AddSheetFrom[builderUser]("用户", excel.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(builderUser{Name: "张三", Age: 18}); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("second Close err=%v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestStructSheetWriterWriteTooLong 场景：超长单元格报行级错误，Builder 失败后不产出。
func TestStructSheetWriterWriteTooLong(t *testing.T) {
	type cell struct {
		X string `excel:"x"`
	}
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.AddSheetFrom[cell]("A", excel.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	err = w.Write(cell{X: strings.Repeat("a", excelize.TotalCellChars+1)})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) {
		t.Fatalf("err=%v, want *RowError", err)
	}
	if rowErr.Row != 2 || rowErr.Column != "x" {
		t.Fatalf("rowErr=%+v, want row 2 column x", rowErr)
	}
	if err := w.Close(); err == nil {
		t.Fatal("Close err=nil, want failure")
	}
	if err := b.Close(); err == nil {
		t.Fatal("Builder.Close err=nil, want failure")
	}
	if buf.Len() != 0 {
		t.Fatalf("buf len=%d, want 0", buf.Len())
	}
}

// TestAddSheetFromDuplicate 场景：重名报 ErrSheetExists 后，原活动 sheet 仍可继续写。
func TestAddSheetFromDuplicate(t *testing.T) {
	b, err := excel.NewBuilder(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.AddSheetFrom[builderUser]("用户", excel.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Write(builderUser{Name: "张三", Age: 18}); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddSheetFrom[builderUser]("用户", excel.WriteOptions{}); !errors.Is(err, excel.ErrSheetExists) {
		t.Fatalf("err=%v, want ErrSheetExists", err)
	}
	if err := w.Write(builderUser{Name: "李四", Age: 20}); err != nil {
		t.Fatalf("write after duplicate: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestStructSheetWriterPreviousSheetClosed 场景：AddSheetFrom 切换 sheet 后旧句柄不可写。
func TestStructSheetWriterPreviousSheetClosed(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	w1, err := b.AddSheetFrom[builderUser]("用户", excel.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w1.Write(builderUser{Name: "张三", Age: 18}); err != nil {
		t.Fatal(err)
	}
	w2, err := b.AddSheetFrom[readerOrder]("订单", excel.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if err := w1.Write(builderUser{Name: "李四", Age: 20}); !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("old writer err=%v, want ErrClosed", err)
	}
	if err := w2.Write(readerOrder{No: "A001", Amount: 12.5}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	r, err := excel.Open(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s1, err := r.Sheet("用户")
	if err != nil {
		t.Fatal(err)
	}
	users, err := s1.ReadInto[builderUser](excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	s2, err := r.Sheet("订单")
	if err != nil {
		t.Fatal(err)
	}
	orders, err := s2.ReadInto[readerOrder](excel.ReadOptions{})
	t.Logf("users=%+v orders=%+v", users, orders)
	if err != nil {
		t.Fatal(err)
	}
	if len(users) != 1 || users[0].Name != "张三" {
		t.Fatalf("users=%+v", users)
	}
	if len(orders) != 1 || orders[0].No != "A001" {
		t.Fatalf("orders=%+v", orders)
	}
}
