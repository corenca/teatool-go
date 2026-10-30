package excel_test

import (
	"errors"
	"testing"

	"github.com/corenca/teatool-go/excel"
)

// TestConvertValue 覆盖各类型的校验与规范化。
func TestConvertValue(t *testing.T) {
	cases := []struct {
		typ     excel.ValueType
		layout  string
		in      string
		want    string
		wantErr bool
	}{
		{excel.Int, "", " 42 ", "42", false},
		{excel.Int, "", "4.2", "", true},
		{excel.Float, "", "1234.50", "1234.5", false},
		{excel.Bool, "", "TRUE", "true", false},
		{excel.Bool, "", "false", "false", false},
		{excel.Bool, "", "1", "true", false},
		{excel.Bool, "", "是", "", true},
		{excel.Date, "2006-01-02", "45292", "2024-01-01", false},
		{excel.Date, "2006-01-02", "abc", "", true},
		{excel.DateTime, "2006-01-02 15:04:05", "45292.5", "2024-01-01 12:00:00", false},
		{excel.Time, "15:04:05", "0.5", "12:00:00", false},
	}
	for _, c := range cases {
		got, err := excel.ConvertValueForTest(excel.Column{Key: "x", Type: c.typ, Layout: c.layout}, c.in, false)
		t.Logf("type=%s in=%q got=%q err=%v", c.typ, c.in, got, err)
		if (err != nil) != c.wantErr {
			t.Fatalf("type=%s err=%v", c.typ, err)
		}
		if err == nil && got != c.want {
			t.Fatalf("type=%s got=%q want=%q", c.typ, got, c.want)
		}
	}
}

// TestConvertValueEmpty 确认空值不进类型解析。
func TestConvertValueEmpty(t *testing.T) {
	for _, typ := range []excel.ValueType{excel.Int, excel.Float, excel.Bool, excel.Date} {
		got, err := excel.ConvertValueForTest(excel.Column{Key: "x", Type: typ, Layout: "2006-01-02"}, "", false)
		t.Logf("type=%s got=%q err=%v", typ, got, err)
		if err != nil || got != "" {
			t.Fatalf("type=%s got=%q err=%v", typ, got, err)
		}
	}
}

// TestConvertValue1904 确认 1904 日期系统偏移。
func TestConvertValue1904(t *testing.T) {
	got, err := excel.ConvertValueForTest(excel.Column{Key: "x", Type: excel.Date, Layout: "2006-01-02"}, "45292", true)
	t.Logf("1904 got=%q err=%v", got, err)
	if err != nil || got != "2028-01-02" {
		t.Fatalf("got=%q err=%v", got, err)
	}
	if !errors.Is(nil, nil) {
		t.Fatal("unreachable")
	}
}
