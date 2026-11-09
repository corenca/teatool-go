package dsv_test

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/dsv"
)

// headerOptions 返回最常用的有表头两列声明，避免每个用例重复。
func headerOptions() dsv.ReadOptions {
	return dsv.ReadOptions{
		Delimiter: "!^",
		Columns: []dsv.Column{
			{Header: "姓名", Key: "name"},
			{Header: "年龄", Key: "age"},
		},
	}
}

// 有表头：两列两行，按表头名映射成 map。
func TestReadHeaderRows(t *testing.T) {
	rows, err := dsv.Read(strings.NewReader("姓名!^年龄\n张三!^30\n李四!^40\n"), headerOptions())
	if err != nil {
		t.Fatalf("Read 返回错误: %v", err)
	}
	want := []map[string]string{
		{"name": "张三", "age": "30"},
		{"name": "李四", "age": "40"},
	}
	t.Logf("读到的行: %#v", rows)
	if !reflect.DeepEqual(rows, want) {
		t.Fatalf("结果不符\n得到: %#v\n期望: %#v", rows, want)
	}
}

// 有表头：声明列在文件中不存在，返回 ErrMissingColumn。
func TestReadMissingColumn(t *testing.T) {
	opts := dsv.ReadOptions{Delimiter: "!^", Columns: []dsv.Column{{Header: "城市"}}}
	_, err := dsv.Read(strings.NewReader("姓名!^年龄\n"), opts)
	t.Logf("错误: %v", err)
	if !errors.Is(err, dsv.ErrMissingColumn) {
		t.Fatalf("期望 ErrMissingColumn，得到 %v", err)
	}
}

// 有表头：文件表头重复声明列，返回 ErrDuplicateColumn。
func TestReadDuplicateHeader(t *testing.T) {
	opts := dsv.ReadOptions{Delimiter: "!^", Columns: []dsv.Column{{Header: "姓名"}}}
	_, err := dsv.Read(strings.NewReader("姓名!^姓名\n"), opts)
	t.Logf("错误: %v", err)
	if !errors.Is(err, dsv.ErrDuplicateColumn) {
		t.Fatalf("期望 ErrDuplicateColumn，得到 %v", err)
	}
}

// 无表头：按 Columns 位置对应，Header 被忽略。
func TestReadNoHeader(t *testing.T) {
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		NoHeader:  true,
		Columns:   []dsv.Column{{Header: "忽略", Key: "name"}, {Key: "age"}},
	}
	rows, err := dsv.Read(strings.NewReader("张三!^30\n李四!^40\n"), opts)
	if err != nil {
		t.Fatalf("Read 返回错误: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if len(rows) != 2 || rows[1]["name"] != "李四" || rows[0]["age"] != "30" {
		t.Fatalf("无表头解析结果不符: %#v", rows)
	}
}

// 数据行字段数与表头不一致：*RowError，Line 指向该行，errors.Is ErrColumnMismatch。
func TestReadColumnMismatch(t *testing.T) {
	_, err := dsv.Read(strings.NewReader("姓名!^年龄\n张三!^30!^多余\n"), headerOptions())
	t.Logf("错误: %v", err)
	if !errors.Is(err, dsv.ErrColumnMismatch) {
		t.Fatalf("期望 ErrColumnMismatch，得到 %v", err)
	}
	var rowErr *dsv.RowError
	if !errors.As(err, &rowErr) {
		t.Fatalf("期望 *RowError，得到 %T", err)
	}
	if rowErr.Line != 2 {
		t.Fatalf("行号应为 2，得到 %d", rowErr.Line)
	}
}

// 空文件（有表头模式）返回 ErrEmptyFile；空文件（无表头模式）返回空结果。
func TestReadEmptyFile(t *testing.T) {
	if _, err := dsv.Read(strings.NewReader(""), headerOptions()); !errors.Is(err, dsv.ErrEmptyFile) {
		t.Fatalf("有表头空文件期望 ErrEmptyFile，得到 %v", err)
	}

	opts := dsv.ReadOptions{Delimiter: "!^", NoHeader: true, Columns: []dsv.Column{{Key: "name"}}}
	rows, err := dsv.Read(strings.NewReader(""), opts)
	if err != nil {
		t.Fatalf("无表头空文件不应报错: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("无表头空文件应返回空结果，得到 %#v", rows)
	}
}

// 只有表头没有数据：返回空结果，不报错。
func TestReadHeaderOnly(t *testing.T) {
	rows, err := dsv.Read(strings.NewReader("姓名!^年龄\n"), headerOptions())
	if err != nil {
		t.Fatalf("只有表头不应报错: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("只有表头应返回空结果，得到 %#v", rows)
	}
}

// 中间与结尾的空行不产生记录。
func TestReadBlankLinesSkipped(t *testing.T) {
	rows, err := dsv.Read(strings.NewReader("\n\n姓名!^年龄\n\n张三!^30\n\n"), headerOptions())
	if err != nil {
		t.Fatalf("空行不应造成错误: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if len(rows) != 1 || rows[0]["name"] != "张三" {
		t.Fatalf("空行处理结果不符: %#v", rows)
	}
}

// 回调返回 ErrStop：提前成功结束，不返回错误。
func TestReadCallbackStop(t *testing.T) {
	count := 0
	err := dsv.Each(strings.NewReader("姓名!^年龄\n张三!^30\n李四!^40\n"), headerOptions(),
		func(_ int, _ map[string]string) error {
			count++
			return dsv.ErrStop
		})
	if err != nil {
		t.Fatalf("ErrStop 不应作为错误返回: %v", err)
	}
	if count != 1 {
		t.Fatalf("应在第一行停止，实际回调 %d 次", count)
	}
}

// 回调返回普通错误：包成 *RowError 并保留行号。
func TestReadCallbackError(t *testing.T) {
	boom := errors.New("业务校验失败")
	var gotLine int
	err := dsv.Each(strings.NewReader("姓名!^年龄\n张三!^30\n李四!^40\n"), headerOptions(),
		func(line int, _ map[string]string) error {
			gotLine = line
			return boom
		})
	t.Logf("错误: %v，回调行号: %d", err, gotLine)
	if !errors.Is(err, boom) {
		t.Fatalf("应保留回调错误，得到 %v", err)
	}
	var rowErr *dsv.RowError
	if !errors.As(err, &rowErr) || rowErr.Line != 2 {
		t.Fatalf("应包成第 2 行的 *RowError，得到 %v", err)
	}
}

// 选项非法：分隔符为空/含换行、无列声明、SkipRows 为负、Key 为空、
// Key 重复、日期类型缺 Layout，均返回对应哨兵。
func TestReadInvalidOptions(t *testing.T) {
	valid := dsv.Column{Key: "a"}
	tests := []struct {
		name string
		opts dsv.ReadOptions
		want error
	}{
		{"分隔符为空", dsv.ReadOptions{Columns: []dsv.Column{valid}}, dsv.ErrInvalidOption},
		{"分隔符含换行", dsv.ReadOptions{Delimiter: "a\n", Columns: []dsv.Column{valid}}, dsv.ErrInvalidOption},
		{"未声明任何列", dsv.ReadOptions{Delimiter: "!^"}, dsv.ErrInvalidColumn},
		{"SkipRows 为负", dsv.ReadOptions{Delimiter: "!^", SkipRows: -1, Columns: []dsv.Column{valid}}, dsv.ErrInvalidOption},
		{"Key 与 Header 都为空", dsv.ReadOptions{Delimiter: "!^", Columns: []dsv.Column{{}}}, dsv.ErrInvalidColumn},
		{"Key 重复", dsv.ReadOptions{Delimiter: "!^", NoHeader: true, Columns: []dsv.Column{{Key: "a"}, {Key: "a"}}}, dsv.ErrDuplicateColumn},
		{"日期缺 Layout", dsv.ReadOptions{Delimiter: "!^", Columns: []dsv.Column{{Key: "d", Type: dsv.Date}}}, dsv.ErrInvalidColumn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := dsv.Read(strings.NewReader(""), tt.opts)
			t.Logf("错误: %v", err)
			if !errors.Is(err, tt.want) {
				t.Fatalf("期望 %v，得到 %v", tt.want, err)
			}
		})
	}

	if err := dsv.Each(strings.NewReader("a\n"), dsv.ReadOptions{Delimiter: "!^", NoHeader: true, Columns: []dsv.Column{valid}}, nil); !errors.Is(err, dsv.ErrInvalidOption) {
		t.Fatalf("nil 回调期望 ErrInvalidOption，得到 %v", err)
	}
}

// ReadFile 与 EachFile：内容与 Read 一致，文件由函数自行关闭。
func TestReadFileEntries(t *testing.T) {
	path := tempFile(t, "姓名!^年龄\n张三!^30\n")

	rows, err := dsv.ReadFile(path, headerOptions())
	if err != nil {
		t.Fatalf("ReadFile 返回错误: %v", err)
	}
	t.Logf("ReadFile 读到: %#v", rows)
	if len(rows) != 1 || rows[0]["age"] != "30" {
		t.Fatalf("ReadFile 结果不符: %#v", rows)
	}

	var count int
	if err := dsv.EachFile(path, headerOptions(), func(_ int, _ map[string]string) error {
		count++
		return nil
	}); err != nil {
		t.Fatalf("EachFile 返回错误: %v", err)
	}
	if count != 1 {
		t.Fatalf("EachFile 回调次数应为 1，得到 %d", count)
	}
}

// 超长行（单个字段 1MB）仍能读取，证明没有 Scanner 令牌上限。
func TestReadLongLine(t *testing.T) {
	long := strings.Repeat("a", 1<<20)
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		Columns:   []dsv.Column{{Header: "h1", Key: "a"}, {Header: "h2", Key: "b"}},
	}
	rows, err := dsv.Read(strings.NewReader("h1!^h2\n"+long+"!^末尾\n"), opts)
	if err != nil {
		t.Fatalf("超长行不应报错: %v", err)
	}
	if len(rows) != 1 || len(rows[0]["a"]) != len(long) || rows[0]["b"] != "末尾" {
		t.Fatalf("超长行结果不符: 行数=%d", len(rows))
	}
}

// 声明列表头带首尾空白或 BOM：读取路径应清洗后精确匹配，Key 回退也要用清洗后的表头。
func TestReadDeclaredHeaderCleaned(t *testing.T) {
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		Columns: []dsv.Column{
			{Header: " 姓名 "},
			{Header: "\ufeff年龄"},
		},
	}
	rows, err := dsv.Read(strings.NewReader("姓名!^年龄\n张三!^30\n"), opts)
	if err != nil {
		t.Fatalf("声明表头带空白/BOM 应能匹配: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if rows[0]["姓名"] != "张三" || rows[0]["年龄"] != "30" {
		t.Fatalf("清洗后的 Key/值不符: %#v", rows)
	}
}

// 文件表头有未声明的列：只按声明取值，多余列忽略且不影响宽度校验。
func TestReadExtraFileColumnsIgnored(t *testing.T) {
	rows, err := dsv.Read(strings.NewReader("姓名!^年龄!^备注\n张三!^30!^备注值\n"), headerOptions())
	if err != nil {
		t.Fatalf("多余列应被忽略: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if len(rows) != 1 || rows[0]["name"] != "张三" || rows[0]["age"] != "30" {
		t.Fatalf("结果不符: %#v", rows)
	}
	if _, ok := rows[0]["备注"]; ok {
		t.Fatalf("未声明的列不应出现在结果里: %#v", rows[0])
	}
}
