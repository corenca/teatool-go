package excel_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/excel"
)

// TestNormalizeColumns 覆盖列声明的装配期校验。
func TestNormalizeColumns(t *testing.T) {
	cases := []struct {
		name    string
		cols    []excel.Column
		wantErr error
	}{
		{"空声明", nil, excel.ErrInvalidColumn},
		{"Key 默认取 Header", []excel.Column{{Header: "订单号"}}, nil},
		{"Key 与 Header 都为空", []excel.Column{{}}, excel.ErrInvalidColumn},
		{"重复 Key", []excel.Column{{Header: "A", Key: "k"}, {Header: "B", Key: "k"}}, excel.ErrInvalidColumn},
		{"未知类型", []excel.Column{{Header: "A", Type: excel.ValueType("json")}}, excel.ErrInvalidColumn},
		{"日期缺 Layout", []excel.Column{{Header: "A", Type: excel.Date}}, excel.ErrInvalidColumn},
		{"负列宽", []excel.Column{{Header: "A", Width: -1}}, excel.ErrInvalidColumn},
		{"列宽超上限", []excel.Column{{Header: "A", Width: 256}}, excel.ErrInvalidColumn},
	}
	for _, c := range cases {
		got, err := excel.NormalizeColumnsForTest(c.cols)
		t.Logf("%s: err=%v", c.name, err)
		if !errors.Is(err, c.wantErr) {
			t.Fatalf("%s: err=%v, want %v", c.name, err, c.wantErr)
		}
		if err == nil && got[0].Key == "" {
			t.Fatalf("%s: Key 未填充", c.name)
		}
	}
}

// TestReadOptionsValidate 覆盖读取装配期的额外约束。
func TestReadOptionsValidate(t *testing.T) {
	dateCol := excel.Column{Header: "日期", Type: excel.Date, Layout: "2006-01-02"}

	cases := []struct {
		name string
		opts excel.ReadOptions
		want error
	}{
		{"日期列必须开 RawValues", excel.ReadOptions{Columns: []excel.Column{dateCol}}, excel.ErrInvalidColumn},
		{"日期列开 RawValues", excel.ReadOptions{Columns: []excel.Column{dateCol}, RawValues: true}, nil},
		{"SkipRows 为负", excel.ReadOptions{SkipRows: -1, Columns: []excel.Column{{Header: "A"}}}, excel.ErrInvalidColumn},
		{"表头模式 Header 为空", excel.ReadOptions{Columns: []excel.Column{{Key: "a"}}}, excel.ErrInvalidColumn},
		{"表头模式声明重复 Header", excel.ReadOptions{Columns: []excel.Column{{Header: "A", Key: "a"}, {Header: "A", Key: "b"}}}, excel.ErrInvalidColumn},
		{"无表头模式只要求 Key", excel.ReadOptions{NoHeader: true, Columns: []excel.Column{{Key: "a"}}}, nil},
	}
	for _, c := range cases {
		err := excel.ValidateReadOptionsForTest(c.opts)
		t.Logf("%s: err=%v", c.name, err)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: err=%v, want %v", c.name, err, c.want)
		}
	}
}

// TestWriteOptionsValidate 覆盖写出装配期约束。
func TestWriteOptionsValidate(t *testing.T) {
	cases := []struct {
		name string
		opts excel.WriteOptions
		want error
	}{
		{"空声明", excel.WriteOptions{}, excel.ErrInvalidColumn},
		{"重复表头", excel.WriteOptions{Columns: []excel.Column{{Header: "A", Key: "a"}, {Header: "A", Key: "b"}}}, excel.ErrInvalidColumn},
		{"正常", excel.WriteOptions{Columns: []excel.Column{{Header: "A", Key: "a"}}}, nil},
	}
	for _, c := range cases {
		err := excel.ValidateWriteOptionsForTest(c.opts)
		t.Logf("%s: err=%v", c.name, err)
		if !errors.Is(err, c.want) {
			t.Fatalf("%s: err=%v, want %v", c.name, err, c.want)
		}
	}
}

// TestRowError 确认行级错误同时暴露行号、列名与原始错误。
func TestRowError(t *testing.T) {
	cause := errors.New("解析失败")
	err := &excel.RowError{Row: 7, Column: "金额", Err: cause}
	if !strings.Contains(err.Error(), "7") || !strings.Contains(err.Error(), "金额") {
		t.Fatalf("Error()=%q", err.Error())
	}
	if !errors.Is(err, cause) {
		t.Fatalf("Unwrap 未生效: %v", err)
	}
}
