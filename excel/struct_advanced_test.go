package excel_test

import (
	"bytes"
	"errors"
	"strconv"
	"testing"
	"time"

	"github.com/corenca/teatool-go/excel"
	"github.com/xuri/excelize/v2"
)

var (
	errTestMarshal   = errors.New("测试 MarshalText 失败")
	errTestUnmarshal = errors.New("测试 UnmarshalText 失败")
)

// testMoney 是带文本编解码的自定义结构体类型，模拟金额等业务类型。
type testMoney struct {
	Cents int64
}

// MarshalText 把金额写成十进制分。
func (m *testMoney) MarshalText() ([]byte, error) {
	return []byte(strconv.FormatInt(m.Cents, 10)), nil
}

// UnmarshalText 从十进制分解析金额。
func (m *testMoney) UnmarshalText(text []byte) error {
	n, err := strconv.ParseInt(string(text), 10, 64)
	if err != nil {
		return err
	}
	m.Cents = n
	return nil
}

// testFailMoney 用于覆盖写出时 MarshalText 报错。
type testFailMoney struct{}

// MarshalText 固定返回错误。
func (testFailMoney) MarshalText() ([]byte, error) { return nil, errTestMarshal }

// UnmarshalText 仅用于让类型在结构体层被识别为文本类型。
func (*testFailMoney) UnmarshalText([]byte) error { return nil }

// testFailUnmarshal 用于覆盖读取时 UnmarshalText 报错。
type testFailUnmarshal struct{}

// MarshalText 仅用于让类型在结构体层被识别为文本类型。
func (testFailUnmarshal) MarshalText() ([]byte, error) { return []byte("x"), nil }

// UnmarshalText 固定返回错误。
func (*testFailUnmarshal) UnmarshalText([]byte) error { return errTestUnmarshal }

// onlyMarshal 只实现写方向，读取应在装配期报错。
type onlyMarshal struct {
	Value int
}

// MarshalText 返回固定文本。
func (onlyMarshal) MarshalText() ([]byte, error) { return []byte("m"), nil }

// onlyUnmarshal 只实现读方向，写出应在装配期报错。
type onlyUnmarshal struct {
	Value string
}

// UnmarshalText 保存原始文本。
func (o *onlyUnmarshal) UnmarshalText(text []byte) error {
	o.Value = string(text)
	return nil
}

// testAddress 是嵌套结构体测试用的值对象。
type testAddress struct {
	Province string `excel:"省"`
	City     string `excel:"市"`
}

// testNestedUser 覆盖父 tag 作为列前缀。
type testNestedUser struct {
	Name    string      `excel:"姓名"`
	Address testAddress `excel:"地址"`
}

// testNestedNoPrefix 覆盖嵌套结构体默认不加前缀。
type testNestedNoPrefix struct {
	Name    string `excel:"姓名"`
	Address testAddress
}

// TestBase 是匿名字段测试用的导出基础类型。
type TestBase struct {
	ID int `excel:"ID"`
}

// testEmbeddedUser 覆盖匿名字段自动展平。
type testEmbeddedUser struct {
	TestBase
	Name string `excel:"姓名"`
}

// testPtrNestedUser 覆盖嵌套指针的 nil 与按需分配。
type testPtrNestedUser struct {
	Name    string       `excel:"姓名"`
	Address *testAddress `excel:"地址"`
}

// testDupNested 覆盖两个嵌套结构体产生重复叶子表头。
type testDupNested struct {
	A testAddress
	B testAddress
}

// testBadNested 覆盖嵌套容器 tag 带选项时的装配期错误。
type testBadNested struct {
	Address testAddress `excel:"地址,required"`
}

// testNestedSkip 覆盖嵌套字段整体跳过。
type testNestedSkip struct {
	Address testAddress `excel:"-"`
	Name    string      `excel:"姓名"`
}

// testNestedDetail 是嵌套结构体里包含自定义文本类型与日期的叶子容器。
type testNestedDetail struct {
	Price testMoney `excel:"金额"`
	At    time.Time `excel:"时间,layout:2006-01-02"`
}

// testNestedCustom 覆盖嵌套容器内同时存在自定义文本类型与 time.Time。
type testNestedCustom struct {
	Name   string           `excel:"姓名"`
	Detail testNestedDetail `excel:"详情"`
}

// TestStructCustomTextRoundTrip 场景：实现 TextMarshaler/Unmarshaler 的结构体字段可往返。
func TestStructCustomTextRoundTrip(t *testing.T) {
	type row struct {
		Price testMoney `excel:"金额"`
		Note  string    `excel:"备注"`
	}
	in := row{Price: testMoney{Cents: 12345}, Note: "n"}

	var buf bytes.Buffer
	if err := writeFromTest(t, &buf, "", []row{in}, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	sheet := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", sheet)
	if len(sheet) != 2 || sheet[0][0] != "金额" || sheet[1][0] != "12345" {
		t.Fatalf("sheet=%v", sheet)
	}

	out, err := readIntoTest[row](t, bytes.NewReader(buf.Bytes()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("in=%+v out=%+v", in, out)
	if len(out) != 1 || out[0].Price.Cents != 12345 || out[0].Note != "n" {
		t.Fatalf("out=%+v", out)
	}
}

// TestStructCustomTextPointer 场景：自定义文本类型的指针字段空值与值语义正确。
func TestStructCustomTextPointer(t *testing.T) {
	type row struct {
		Name  string     `excel:"姓名"`
		Price *testMoney `excel:"金额"`
	}
	in := []row{{Name: "a"}, {Name: "b", Price: &testMoney{Cents: 7}}}

	var buf bytes.Buffer
	if err := writeFromTest(t, &buf, "", in, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	out, err := readIntoTest[row](t, bytes.NewReader(buf.Bytes()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("in=%+v out=%+v", in, out)
	if len(out) != 2 || out[0].Price != nil || out[1].Price == nil || out[1].Price.Cents != 7 {
		t.Fatalf("out=%+v", out)
	}
}

// TestStructCustomMarshalError 场景：MarshalText 报错时返回带列名的行级错误且不落盘。
func TestStructCustomMarshalError(t *testing.T) {
	type row struct {
		Price testFailMoney `excel:"金额"`
	}
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	w, err := b.AddSheetFrom[row]("A", excel.WriteOptions{})
	if err != nil {
		t.Fatal(err)
	}
	err = w.Write(row{})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) || rowErr.Row != 2 || rowErr.Column != "金额" {
		t.Fatalf("err=%v", err)
	}
	if !errors.Is(err, errTestMarshal) {
		t.Fatalf("err=%v, want errTestMarshal", err)
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

// TestStructCustomUnmarshalError 场景：UnmarshalText 报错时返回带行号与列名的错误。
func TestStructCustomUnmarshalError(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"金额"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"bad"})
	})
	type row struct {
		Price testFailUnmarshal `excel:"金额"`
	}
	_, err := readIntoTest[row](t, bytes.NewReader(data), excel.ReadOptions{})
	var rowErr *excel.RowError
	t.Logf("err=%v", err)
	if !errors.As(err, &rowErr) || rowErr.Row != 2 || rowErr.Column != "金额" {
		t.Fatalf("err=%v", err)
	}
	if !errors.Is(err, errTestUnmarshal) {
		t.Fatalf("err=%v, want errTestUnmarshal", err)
	}
}

// TestStructTextDirectionValidation 场景：只实现单方向文本接口时，另一方向装配期报错。
func TestStructTextDirectionValidation(t *testing.T) {
	type writeOnlyRow struct {
		V onlyMarshal `excel:"v"`
	}
	type readOnlyRow struct {
		V onlyUnmarshal `excel:"v"`
	}

	var buf bytes.Buffer
	if err := writeFromTest(t, &buf, "", []writeOnlyRow{{V: onlyMarshal{Value: 1}}}, excel.WriteOptions{}); err != nil {
		t.Fatalf("write onlyMarshal: %v", err)
	}
	if err := writeFromTest(t, &buf, "", []readOnlyRow{{}}, excel.WriteOptions{}); !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("write onlyUnmarshal err=%v, want ErrInvalidColumn", err)
	}

	readData := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"v"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"x"})
	})
	if _, err := readIntoTest[writeOnlyRow](t, bytes.NewReader(readData), excel.ReadOptions{}); !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("read onlyMarshal err=%v, want ErrInvalidColumn", err)
	}
	out, err := readIntoTest[readOnlyRow](t, bytes.NewReader(readData), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("readOnly out=%+v", out)
	if len(out) != 1 || out[0].V.Value != "x" {
		t.Fatalf("out=%+v", out)
	}
}

// TestStructNestedPrefix 场景：嵌套结构体默认展平，父 tag 作为叶子表头前缀。
func TestStructNestedPrefix(t *testing.T) {
	in := []testNestedUser{{Name: "张三", Address: testAddress{Province: "广东", City: "深圳"}}}

	var buf bytes.Buffer
	if err := writeFromTest(t, &buf, "", in, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	sheet := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", sheet)
	if len(sheet[0]) != 3 || sheet[0][1] != "地址.省" || sheet[0][2] != "地址.市" {
		t.Fatalf("header=%v", sheet[0])
	}

	out, err := readIntoTest[testNestedUser](t, bytes.NewReader(buf.Bytes()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("out=%+v", out)
	if len(out) != 1 || out[0].Address.Province != "广东" || out[0].Address.City != "深圳" {
		t.Fatalf("out=%+v", out)
	}
}

// TestStructNestedNoPrefix 场景：嵌套结构体无父 tag 时叶子表头保持原样。
func TestStructNestedNoPrefix(t *testing.T) {
	in := []testNestedNoPrefix{{Name: "李四", Address: testAddress{Province: "江苏", City: "南京"}}}

	var buf bytes.Buffer
	if err := writeFromTest(t, &buf, "", in, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	sheet := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", sheet)
	if len(sheet[0]) != 3 || sheet[0][1] != "省" || sheet[0][2] != "市" {
		t.Fatalf("header=%v", sheet[0])
	}
}

// TestStructNestedEmbedded 场景：匿名字段自动展平，叶子顺序按字段声明。
func TestStructNestedEmbedded(t *testing.T) {
	in := []testEmbeddedUser{{ID: 7, Name: "王五"}}

	var buf bytes.Buffer
	if err := writeFromTest(t, &buf, "", in, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	sheet := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", sheet)
	if len(sheet[0]) != 2 || sheet[0][0] != "ID" || sheet[0][1] != "姓名" {
		t.Fatalf("header=%v", sheet[0])
	}

	out, err := readIntoTest[testEmbeddedUser](t, bytes.NewReader(buf.Bytes()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 1 || out[0].ID != 7 || out[0].Name != "王五" {
		t.Fatalf("out=%+v", out)
	}
}

// TestStructNestedPointerNil 场景：嵌套指针全空读成 nil，有值才分配。
func TestStructNestedPointerNil(t *testing.T) {
	in := []testPtrNestedUser{
		{Name: "a"},
		{Name: "b", Address: &testAddress{Province: "浙江", City: "杭州"}},
	}

	var buf bytes.Buffer
	if err := writeFromTest(t, &buf, "", in, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	out, err := readIntoTest[testPtrNestedUser](t, bytes.NewReader(buf.Bytes()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("out=%+v", out)
	if len(out) != 2 || out[0].Address != nil {
		t.Fatalf("out=%+v", out)
	}
	if out[1].Address == nil || out[1].Address.Province != "浙江" || out[1].Address.City != "杭州" {
		t.Fatalf("out[1]=%+v", out[1])
	}
}

// TestStructNestedDuplicateHeader 场景：两个嵌套结构体产生重复叶子表头时装配期报错。
func TestStructNestedDuplicateHeader(t *testing.T) {
	var buf bytes.Buffer
	err := writeFromTest(t, &buf, "", []testDupNested{{}}, excel.WriteOptions{})
	if !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("err=%v, want ErrInvalidColumn", err)
	}
}

// TestStructNestedTagOptionsRejected 场景：嵌套容器 tag 带选项时装配期报错。
func TestStructNestedTagOptionsRejected(t *testing.T) {
	var buf bytes.Buffer
	err := writeFromTest(t, &buf, "", []testBadNested{{}}, excel.WriteOptions{})
	if !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("err=%v, want ErrInvalidColumn", err)
	}
}

// TestStructNestedSkip 场景：嵌套字段 tag 为 - 时整组字段跳过。
func TestStructNestedSkip(t *testing.T) {
	in := []testNestedSkip{{Address: testAddress{Province: "x", City: "y"}, Name: "z"}}
	var buf bytes.Buffer
	if err := writeFromTest(t, &buf, "", in, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	sheet := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", sheet)
	if len(sheet[0]) != 1 || sheet[0][0] != "姓名" {
		t.Fatalf("header=%v", sheet[0])
	}
}

// TestStructNestedCustomAndTime 场景：嵌套容器内的自定义文本类型与 time.Time 正常展平。
func TestStructNestedCustomAndTime(t *testing.T) {
	in := []testNestedCustom{{
		Name: "张三",
		Detail: testNestedDetail{
			Price: testMoney{Cents: 99},
			At:    time.Date(2024, 1, 30, 0, 0, 0, 0, time.UTC),
		},
	}}
	var buf bytes.Buffer
	if err := writeFromTest(t, &buf, "", in, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	sheet := readBack(t, buf.Bytes())
	t.Logf("sheet=%v", sheet)
	if len(sheet[0]) != 3 || sheet[0][1] != "详情.金额" || sheet[0][2] != "详情.时间" {
		t.Fatalf("header=%v", sheet[0])
	}
	out, err := readIntoTest[testNestedCustom](t, bytes.NewReader(buf.Bytes()), excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("out=%+v", out)
	if len(out) != 1 || out[0].Detail.Price.Cents != 99 || out[0].Detail.At.Format("2006-01-02") != "2024-01-30" {
		t.Fatalf("out=%+v", out)
	}
}

// TestFileNestedCustomRoundTrip 场景：文件一行入口同样支持嵌套字段与自定义文本类型。
func TestFileNestedCustomRoundTrip(t *testing.T) {
	path := t.TempDir() + "/out.xlsx"
	in := []testNestedCustom{{
		Name: "李四",
		Detail: testNestedDetail{
			Price: testMoney{Cents: 66},
			At:    time.Date(2024, 2, 1, 0, 0, 0, 0, time.UTC),
		},
	}}
	if err := excel.WriteFileFrom(path, "详情", in, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	out, err := excel.ReadFileInto[testNestedCustom](path, "详情", excel.ReadOptions{})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("in=%+v out=%+v", in, out)
	if len(out) != 1 || out[0].Detail.Price.Cents != 66 || out[0].Detail.At.Format("2006-01-02") != "2024-02-01" {
		t.Fatalf("out=%+v", out)
	}
}
