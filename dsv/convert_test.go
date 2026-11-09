package dsv_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/dsv"
)

// 各类型的合法输入与规范化输出。
func TestReadValueTypes(t *testing.T) {
	tests := []struct {
		name  string
		col   dsv.Column
		input string
		want  string
	}{
		{"Text 原样保留空白", dsv.Column{Header: "v", Key: "v"}, " x ", " x "},
		{"Int 去前导零", dsv.Column{Header: "v", Key: "v", Type: dsv.Int}, "007", "7"},
		{"Int 接受正号", dsv.Column{Header: "v", Key: "v", Type: dsv.Int}, "+5", "5"},
		{"Int 负数", dsv.Column{Header: "v", Key: "v", Type: dsv.Int}, "-0042", "-42"},
		{"Float 去多余小数位", dsv.Column{Header: "v", Key: "v", Type: dsv.Float}, "1.500", "1.5"},
		{"Float 科学计数法转普通形式", dsv.Column{Header: "v", Key: "v", Type: dsv.Float}, "1e3", "1000"},
		{"Bool 大写 TRUE", dsv.Column{Header: "v", Key: "v", Type: dsv.Bool}, "TRUE", "true"},
		{"Bool 数字 1", dsv.Column{Header: "v", Key: "v", Type: dsv.Bool}, "1", "true"},
		{"Bool 小写 false", dsv.Column{Header: "v", Key: "v", Type: dsv.Bool}, "false", "false"},
		{"Bool 数字 0", dsv.Column{Header: "v", Key: "v", Type: dsv.Bool}, "0", "false"},
		{"Date 按布局解析并输出", dsv.Column{Header: "v", Key: "v", Type: dsv.Date, Layout: "2006-1-2"}, "2026-01-09", "2026-1-9"},
		{"DateTime 按布局解析并输出", dsv.Column{Header: "v", Key: "v", Type: dsv.DateTime, Layout: "2006-01-02 15:04:05"}, "2026-01-09 08:05:00", "2026-01-09 08:05:00"},
		{"Time 按布局解析并输出", dsv.Column{Header: "v", Key: "v", Type: dsv.Time, Layout: "15:04"}, "08:05", "08:05"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := dsv.ReadOptions{Delimiter: "!^", Columns: []dsv.Column{tt.col}}
			rows, err := dsv.Read(strings.NewReader("v\n"+tt.input+"\n"), opts)
			if err != nil {
				t.Fatalf("读取失败: %v", err)
			}
			t.Logf("输入 %q -> 输出 %q", tt.input, rows[0]["v"])
			if rows[0]["v"] != tt.want {
				t.Fatalf("输入 %q 期望 %q，得到 %q", tt.input, tt.want, rows[0]["v"])
			}
		})
	}
}

// 各类型的非法输入：错误是 *RowError，Column 为列的 Key。
func TestReadValueTypeErrors(t *testing.T) {
	tests := []struct {
		name string
		col  dsv.Column
		in   string
	}{
		{"Int 非数字", dsv.Column{Header: "v", Key: "v", Type: dsv.Int}, "abc"},
		{"Float 非数字", dsv.Column{Header: "v", Key: "v", Type: dsv.Float}, "x1"},
		{"Bool 非布尔", dsv.Column{Header: "v", Key: "v", Type: dsv.Bool}, "yes"},
		{"Date 格式不符", dsv.Column{Header: "v", Key: "v", Type: dsv.Date, Layout: "2006-01-02"}, "2026/01/09"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			opts := dsv.ReadOptions{Delimiter: "!^", Columns: []dsv.Column{tt.col}}
			_, err := dsv.Read(strings.NewReader("v\n"+tt.in+"\n"), opts)
			t.Logf("错误: %v", err)
			var rowErr *dsv.RowError
			if !errors.As(err, &rowErr) {
				t.Fatalf("期望 *RowError，得到 %T: %v", err, err)
			}
			if rowErr.Column != "v" || rowErr.Line != 2 {
				t.Fatalf("错误位置不符: %+v", rowErr)
			}
			if !strings.Contains(rowErr.Error(), "无法解析") {
				t.Fatalf("错误信息应说明无法解析: %v", rowErr)
			}
		})
	}
}

// Required 列为空：*RowError；非必填列为空返回 ""。
func TestReadRequired(t *testing.T) {
	opts := dsv.ReadOptions{Delimiter: "!^", Columns: []dsv.Column{
		{Header: "姓名", Key: "name", Required: true},
		{Header: "年龄", Key: "age"},
	}}
	_, err := dsv.Read(strings.NewReader("姓名!^年龄\n!^30\n"), opts)
	t.Logf("错误: %v", err)
	var rowErr *dsv.RowError
	if !errors.As(err, &rowErr) || rowErr.Column != "name" || rowErr.Line != 2 {
		t.Fatalf("必填为空应报第 2 行 name 列，得到 %v", err)
	}
	if !strings.Contains(rowErr.Error(), "必填") {
		t.Fatalf("错误信息应说明必填: %v", rowErr)
	}

	rows, err := dsv.Read(strings.NewReader("姓名!^年龄\n张三!^\n"), opts)
	if err != nil {
		t.Fatalf("非必填为空不应报错: %v", err)
	}
	if rows[0]["age"] != "" {
		t.Fatalf("非必填空值应为空字符串，得到 %q", rows[0]["age"])
	}
}

// Validate 收到的是规范化之后的行；返回错误包成 *RowError；
// 返回 ErrStop 时读取成功结束且不返回错误。
func TestReadValidate(t *testing.T) {
	opts := dsv.ReadOptions{Delimiter: "!^", Columns: []dsv.Column{
		{Header: "姓名", Key: "name"},
		{Header: "年龄", Key: "age", Type: dsv.Int},
	}}
	var seen string
	opts.Validate = func(_ int, row map[string]string) error {
		seen = row["age"]
		return nil
	}
	if _, err := dsv.Read(strings.NewReader("姓名!^年龄\n张三!^007\n"), opts); err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	t.Logf("Validate 看到的值: %q", seen)
	if seen != "7" {
		t.Fatalf("Validate 应看到规范化后的值 7，得到 %q", seen)
	}

	boom := errors.New("业务规则不通过")
	opts.Validate = func(_ int, _ map[string]string) error { return boom }
	_, err := dsv.Read(strings.NewReader("姓名!^年龄\n张三!^30\n"), opts)
	t.Logf("校验错误: %v", err)
	var rowErr *dsv.RowError
	if !errors.As(err, &rowErr) || rowErr.Line != 2 || !errors.Is(err, boom) {
		t.Fatalf("期望包装 boom 的第 2 行 *RowError，得到 %v", err)
	}

	opts.Validate = func(line int, _ map[string]string) error {
		if line == 3 {
			return dsv.ErrStop
		}
		return nil
	}
	rows, err := dsv.Read(strings.NewReader("姓名!^年龄\n张三!^30\n李四!^40\n"), opts)
	if err != nil {
		t.Fatalf("ErrStop 不应作为错误返回: %v", err)
	}
	t.Logf("ErrStop 前读到的行: %#v", rows)
	if len(rows) != 1 {
		t.Fatalf("ErrStop 前应交付 1 行，得到 %d 行", len(rows))
	}
}

// Validate 返回 *RowError：原样透传，不被当前行二次包装。
func TestReadValidateRowErrorPassthrough(t *testing.T) {
	opts := dsv.ReadOptions{Delimiter: "!^", Columns: []dsv.Column{
		{Header: "姓名", Key: "name"},
		{Header: "年龄", Key: "age"},
	}}
	opts.Validate = func(_ int, _ map[string]string) error {
		return &dsv.RowError{Line: 99, Column: "自定义列", Err: errors.New("跨行校验失败")}
	}
	_, err := dsv.Read(strings.NewReader("姓名!^年龄\n张三!^30\n"), opts)
	t.Logf("错误: %v", err)
	var rowErr *dsv.RowError
	if !errors.As(err, &rowErr) {
		t.Fatalf("期望 *RowError，得到 %T", err)
	}
	if rowErr.Line != 99 || rowErr.Column != "自定义列" {
		t.Fatalf("业务指定的位置被覆盖: %+v", rowErr)
	}
}
