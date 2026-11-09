package dsv_test

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/corenca/teatool-go/dsv"
)

// userRow 是结构体读取的主用例类型：普通字段、必填、指针、时间与忽略字段。
type userRow struct {
	Name  string    `dsv:"姓名"`
	Age   int       `dsv:"年龄,required"`
	Score *float64  `dsv:"分数"`
	Birth time.Time `dsv:"生日,layout:2006-01-02"`
	Nick  string    `dsv:"-"`
	note  string
}

// emptyRow 用于验证空值与非指针零值。
type emptyRow struct {
	A int    `dsv:"a"`
	B string `dsv:"b"`
	C *int   `dsv:"c"`
}

// upperText 是 encoding.TextUnmarshaler 自定义类型。
type upperText string

// UnmarshalText 把文本转成大写，验证结构体层会调用文本接口。
func (u *upperText) UnmarshalText(b []byte) error {
	*u = upperText(strings.ToUpper(string(b)))
	return nil
}

// textRow 使用自定义文本类型。
type textRow struct {
	V upperText `dsv:"v"`
}

// scoreRow 用于验证 Validate() error。
type scoreRow struct {
	Score int `dsv:"分数"`
}

// Validate 要求分数不能为负。
func (r scoreRow) Validate() error {
	if r.Score < 0 {
		return fmt.Errorf("分数不能为负: %d", r.Score)
	}
	return nil
}

// structOptions 返回有表头、无 Columns 的选项（结构体层列来自 tag）。
func structOptions() dsv.ReadOptions {
	return dsv.ReadOptions{Delimiter: "!^"}
}

// 有表头 + tag 映射：值、指针、time.Time 都正确；Nick 不参与映射。
func TestReadIntoBasic(t *testing.T) {
	rows, err := dsv.ReadInto[userRow](strings.NewReader(
		"姓名!^年龄!^分数!^生日\n张三!^30!^98.5!^2000-01-02\n"), structOptions())
	if err != nil {
		t.Fatalf("ReadInto 失败: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应有 1 行，得到 %d", len(rows))
	}
	got := rows[0]
	t.Logf("读到的结构体: %+v, Score=%v", got, *got.Score)
	if got.Name != "张三" || got.Age != 30 || got.Score == nil || *got.Score != 98.5 {
		t.Fatalf("字段值不符: %+v", got)
	}
	want := time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)
	if !got.Birth.Equal(want) {
		t.Fatalf("生日应为 %v，得到 %v", want, got.Birth)
	}
	if got.Nick != "" {
		t.Fatalf("tag 为 - 的字段不应被赋值，得到 %q", got.Nick)
	}
}

// 无表头：按结构体字段顺序对应字段，宽度按字段数校验。
func TestReadIntoNoHeader(t *testing.T) {
	opts := structOptions()
	opts.NoHeader = true
	rows, err := dsv.ReadInto[userRow](strings.NewReader("张三!^30!^98.5!^2000-01-02\n"), opts)
	if err != nil {
		t.Fatalf("无表头 ReadInto 失败: %v", err)
	}
	t.Logf("读到的结构体: %+v", rows[0])
	if rows[0].Name != "张三" || rows[0].Age != 30 {
		t.Fatalf("无表头字段对应不符: %+v", rows[0])
	}
}

// 指针：空字段 -> nil；"0" -> 指向 0；空值与非空值区分正确。
func TestReadIntoNullable(t *testing.T) {
	rows, err := dsv.ReadInto[userRow](strings.NewReader(
		"姓名!^年龄!^分数!^生日\n李四!^40!^!^2000-01-02\n王五!^50!^0!^2000-01-02\n"), structOptions())
	if err != nil {
		t.Fatalf("ReadInto 失败: %v", err)
	}
	t.Logf("空值: %+v，零值: %+v", rows[0].Score, rows[1].Score)
	if rows[0].Score != nil {
		t.Fatalf("空字段应得到 nil，得到 %v", *rows[0].Score)
	}
	if rows[1].Score == nil || *rows[1].Score != 0 {
		t.Fatalf("0 应得到非 nil 的 0，得到 %v", rows[1].Score)
	}
}

// 空值与非指针零值：int 空字段保持 0，string 空字段保持 ""，指针保持 nil。
func TestReadIntoEmptyNonPointer(t *testing.T) {
	rows, err := dsv.ReadInto[emptyRow](strings.NewReader("a!^b!^c\n!^!^\n"), structOptions())
	if err != nil {
		t.Fatalf("ReadInto 失败: %v", err)
	}
	t.Logf("读到的结构体: %+v", rows[0])
	if rows[0].A != 0 || rows[0].B != "" || rows[0].C != nil {
		t.Fatalf("空值应保持零值: %+v", rows[0])
	}
}

// TextUnmarshaler 自定义类型可读。
func TestReadIntoTextUnmarshaler(t *testing.T) {
	rows, err := dsv.ReadInto[textRow](strings.NewReader("v\nabc\n"), structOptions())
	if err != nil {
		t.Fatalf("ReadInto 失败: %v", err)
	}
	t.Logf("自定义类型值: %q", rows[0].V)
	if rows[0].V != "ABC" {
		t.Fatalf("应调用 UnmarshalText 转成 ABC，得到 %q", rows[0].V)
	}
}

// Validate() error：返回的错误包成 *RowError。
func TestReadIntoValidate(t *testing.T) {
	_, err := dsv.ReadInto[scoreRow](strings.NewReader("分数\n-1\n"), structOptions())
	t.Logf("错误: %v", err)
	var rowErr *dsv.RowError
	if !errors.As(err, &rowErr) || rowErr.Line != 2 || rowErr.Column != "" {
		t.Fatalf("期望第 2 行的整行 *RowError，得到 %v", err)
	}
	if !strings.Contains(rowErr.Error(), "分数不能为负") {
		t.Fatalf("应保留 Validate 的错误信息: %v", rowErr)
	}
}

// 非法 tag：嵌套结构体、多层指针、未知选项、time.Time 缺 layout、
// 重复表头、opts.Columns 非空 —— 全部 ErrInvalidColumn/ErrDuplicateColumn。
func TestReadIntoInvalidTags(t *testing.T) {
	type nested struct {
		City string `dsv:"城市"`
	}
	type nestedRow struct {
		Addr nested `dsv:"地址"`
	}
	type doublePointerRow struct {
		A **int `dsv:"a"`
	}
	type unknownOptionRow struct {
		A int `dsv:"a,sort"`
	}
	type timeNoLayoutRow struct {
		At time.Time `dsv:"at"`
	}
	type duplicateRow struct {
		A int `dsv:"x"`
		B int `dsv:"x"`
	}
	type unsupportedRow struct {
		A []byte `dsv:"a"`
	}

	tests := []struct {
		name string
		read func() error
		want error
	}{
		{"嵌套结构体", func() error {
			_, err := dsv.ReadInto[nestedRow](strings.NewReader("城市\n北京\n"), structOptions())
			return err
		}, dsv.ErrInvalidColumn},
		{"多层指针", func() error {
			_, err := dsv.ReadInto[doublePointerRow](strings.NewReader("a\n1\n"), structOptions())
			return err
		}, dsv.ErrInvalidColumn},
		{"未知 tag 选项", func() error {
			_, err := dsv.ReadInto[unknownOptionRow](strings.NewReader("a\n1\n"), structOptions())
			return err
		}, dsv.ErrInvalidColumn},
		{"time 缺 layout", func() error {
			_, err := dsv.ReadInto[timeNoLayoutRow](strings.NewReader("at\n2000-01-02\n"), structOptions())
			return err
		}, dsv.ErrInvalidColumn},
		{"重复表头", func() error {
			_, err := dsv.ReadInto[duplicateRow](strings.NewReader("x\n1\n"), structOptions())
			return err
		}, dsv.ErrDuplicateColumn},
		{"不支持的类型", func() error {
			_, err := dsv.ReadInto[unsupportedRow](strings.NewReader("a\n1\n"), structOptions())
			return err
		}, dsv.ErrInvalidColumn},
		{"Columns 非空", func() error {
			opts := structOptions()
			opts.Columns = []dsv.Column{{Header: "a"}}
			_, err := dsv.ReadInto[emptyRow](strings.NewReader("a!^b!^c\n1!^2!^3\n"), opts)
			return err
		}, dsv.ErrInvalidColumn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.read()
			t.Logf("错误: %v", err)
			if !errors.Is(err, tt.want) {
				t.Fatalf("期望 %v，得到 %v", tt.want, err)
			}
		})
	}
}

// EachInto 回调返回 ErrStop 成功结束；ReadFileInto 走文件入口。
func TestEachIntoStopAndFile(t *testing.T) {
	var count int
	err := dsv.EachInto[userRow](strings.NewReader(
		"姓名!^年龄!^分数!^生日\n张三!^30!^98.5!^2000-01-02\n李四!^40!^90!^2001-01-02\n"), structOptions(),
		func(_ int, _ userRow) error {
			count++
			return dsv.ErrStop
		})
	if err != nil {
		t.Fatalf("ErrStop 不应作为错误返回: %v", err)
	}
	if count != 1 {
		t.Fatalf("应在第一行停止，实际回调 %d 次", count)
	}

	path := tempFile(t, "姓名!^年龄!^分数!^生日\n张三!^30!^98.5!^2000-01-02\n")
	rows, err := dsv.ReadFileInto[userRow](path, structOptions())
	if err != nil {
		t.Fatalf("ReadFileInto 失败: %v", err)
	}
	t.Logf("ReadFileInto 读到: %+v", rows)
	if len(rows) != 1 || rows[0].Name != "张三" {
		t.Fatalf("ReadFileInto 结果不符: %+v", rows)
	}

	var fileCount int
	if err := dsv.EachFileInto[userRow](path, structOptions(), func(_ int, _ userRow) error {
		fileCount++
		return nil
	}); err != nil {
		t.Fatalf("EachFileInto 失败: %v", err)
	}
	if fileCount != 1 {
		t.Fatalf("EachFileInto 回调次数应为 1，得到 %d", fileCount)
	}
}

// 结构体层也应执行 opts.Validate：收到的是以 tag 表头为键的原始字段 map。
func TestEachIntoValidate(t *testing.T) {
	opts := structOptions()
	var seen map[string]string
	opts.Validate = func(_ int, row map[string]string) error {
		seen = row
		return nil
	}
	rows, err := dsv.ReadInto[userRow](strings.NewReader(
		"姓名!^年龄!^分数!^生日\n张三!^30!^98.5!^2000-01-02\n"), opts)
	if err != nil {
		t.Fatalf("结构体层 Validate 读取失败: %v", err)
	}
	t.Logf("Validate 收到的行: %#v，结构体: %+v", seen, rows[0])
	if seen["姓名"] != "张三" || seen["年龄"] != "30" {
		t.Fatalf("Validate 应收到以表头为键的原始值: %#v", seen)
	}

	boom := errors.New("结构体层业务校验失败")
	opts.Validate = func(_ int, _ map[string]string) error { return boom }
	_, err = dsv.ReadInto[userRow](strings.NewReader(
		"姓名!^年龄!^分数!^生日\n张三!^30!^98.5!^2000-01-02\n"), opts)
	t.Logf("校验错误: %v", err)
	var rowErr *dsv.RowError
	if !errors.As(err, &rowErr) || rowErr.Line != 2 || !errors.Is(err, boom) {
		t.Fatalf("期望包装 boom 的第 2 行 *RowError，得到 %v", err)
	}

	opts.Validate = func(_ int, _ map[string]string) error { return dsv.ErrStop }
	rows, err = dsv.ReadInto[userRow](strings.NewReader(
		"姓名!^年龄!^分数!^生日\n张三!^30!^98.5!^2000-01-02\n"), opts)
	if err != nil {
		t.Fatalf("ErrStop 不应作为错误返回: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("ErrStop 应在交付前生效，应交付 0 行，得到 %d", len(rows))
	}
}

// 结构体层 Validate 返回 *RowError：同样原样透传。
func TestEachIntoValidateRowErrorPassthrough(t *testing.T) {
	opts := structOptions()
	opts.Validate = func(_ int, _ map[string]string) error {
		return &dsv.RowError{Line: 99, Column: "自定义列", Err: errors.New("跨行校验失败")}
	}
	_, err := dsv.ReadInto[userRow](strings.NewReader(
		"姓名!^年龄!^分数!^生日\n张三!^30!^98.5!^2000-01-02\n"), opts)
	t.Logf("错误: %v", err)
	var rowErr *dsv.RowError
	if !errors.As(err, &rowErr) || rowErr.Line != 99 || rowErr.Column != "自定义列" {
		t.Fatalf("结构体层业务指定的位置被覆盖: %+v", rowErr)
	}
}

// interface 类型字段应在装配期拒绝，而不是解码/写出行级报错。
func TestStructRejectsInterfaceFields(t *testing.T) {
	type unmarshaler interface{ UnmarshalText([]byte) error }
	type readRow struct {
		V unmarshaler `dsv:"v"`
	}
	_, err := dsv.ReadInto[readRow](strings.NewReader("v\nabc\n"), structOptions())
	t.Logf("读取错误: %v", err)
	if !errors.Is(err, dsv.ErrInvalidColumn) {
		t.Fatalf("读取应返回 ErrInvalidColumn，得到 %v", err)
	}

	type marshaler interface{ MarshalText() ([]byte, error) }
	type writeRow struct {
		V marshaler `dsv:"v"`
	}
	var buf bytes.Buffer
	_, err = dsv.NewStructWriter[writeRow](&buf, dsv.WriteOptions{Delimiter: "!^"})
	t.Logf("写出错误: %v", err)
	if !errors.Is(err, dsv.ErrInvalidColumn) {
		t.Fatalf("写出应返回 ErrInvalidColumn，得到 %v", err)
	}
}

// EachInto 同时收到 nil 回调与非法 tag 时，先报配置错误（与 Each 一致）。
func TestEachIntoNilCallbackOrder(t *testing.T) {
	type badRow struct {
		A **int `dsv:"a"`
	}
	err := dsv.EachInto[badRow](strings.NewReader("a\n1\n"), structOptions(), nil)
	t.Logf("非法 tag + nil 回调: %v", err)
	if !errors.Is(err, dsv.ErrInvalidColumn) {
		t.Fatalf("应先报 ErrInvalidColumn，得到 %v", err)
	}

	err = dsv.EachInto[emptyRow](strings.NewReader("a!^b!^c\n1!^2!^3\n"), structOptions(), nil)
	t.Logf("合法 tag + nil 回调: %v", err)
	if !errors.Is(err, dsv.ErrInvalidOption) {
		t.Fatalf("合法配置应报 ErrInvalidOption，得到 %v", err)
	}
}

// 无表头结构体读取：字段数与结构体映射列数不一致时报 ErrColumnMismatch 并带行号。
func TestReadIntoNoHeaderWidthMismatch(t *testing.T) {
	opts := structOptions()
	opts.NoHeader = true
	_, err := dsv.ReadInto[userRow](strings.NewReader("张三!^30!^98.5\n"), opts)
	t.Logf("错误: %v", err)
	var rowErr *dsv.RowError
	if !errors.Is(err, dsv.ErrColumnMismatch) || !errors.As(err, &rowErr) || rowErr.Line != 1 {
		t.Fatalf("期望第 1 行 ErrColumnMismatch，得到 %v", err)
	}
}

// 有表头结构体读取：表头缺少 tag 声明的列时报 ErrMissingColumn。
func TestReadIntoMissingColumn(t *testing.T) {
	_, err := dsv.ReadInto[userRow](strings.NewReader(
		"姓名!^年龄!^分数\n张三!^30!^98.5\n"), structOptions())
	t.Logf("错误: %v", err)
	if !errors.Is(err, dsv.ErrMissingColumn) {
		t.Fatalf("期望 ErrMissingColumn，得到 %v", err)
	}
}
