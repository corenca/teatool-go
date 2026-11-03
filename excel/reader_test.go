package excel_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/corenca/teatool-go/excel"
	"github.com/xuri/excelize/v2"
)

// TestReaderSheets 场景：两个工作表（第二个隐藏），断言 Sheets 的顺序、Index 与 Visible。
func TestReaderSheets(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		if err := f.SetSheetName("Sheet1", "用户"); err != nil {
			t.Fatal(err)
		}
		if _, err := f.NewSheet("统计"); err != nil {
			t.Fatal(err)
		}
		if err := f.SetSheetVisible("统计", false); err != nil {
			t.Fatal(err)
		}
	})

	r, err := excel.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	infos, err := r.Sheets()
	t.Logf("sheets=%+v err=%v", infos, err)
	if err != nil {
		t.Fatal(err)
	}
	if len(infos) != 2 {
		t.Fatalf("len=%d, want 2", len(infos))
	}
	if infos[0].Name != "用户" || infos[0].Index != 0 || !infos[0].Visible {
		t.Fatalf("first=%+v", infos[0])
	}
	if infos[1].Name != "统计" || infos[1].Index != 1 || infos[1].Visible {
		t.Fatalf("second=%+v", infos[1])
	}
}

// TestReaderSheetMissing 场景：请求不存在的工作表，返回 ErrSheetNotFound。
func TestReaderSheetMissing(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"h"})
	})
	r, err := excel.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	_, err = r.Sheet("不存在")
	t.Logf("err=%v", err)
	if !errors.Is(err, excel.ErrSheetNotFound) {
		t.Fatalf("err=%v, want ErrSheetNotFound", err)
	}
}

// TestSheetReaderRead 场景：两个 sheet 用不同列声明读取；名字匹配大小写不敏感且 Info 用规范名。
func TestSheetReaderRead(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		if err := f.SetSheetName("Sheet1", "Data"); err != nil {
			t.Fatal(err)
		}
		f.SetSheetRow("Data", "A1", &[]any{"姓名", "年龄"})
		f.SetSheetRow("Data", "A2", &[]any{"张三", "18"})
		if _, err := f.NewSheet("统计"); err != nil {
			t.Fatal(err)
		}
		f.SetSheetRow("统计", "A1", &[]any{"项目", "值"})
		f.SetSheetRow("统计", "A2", &[]any{"人数", "1"})
	})

	r, err := excel.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	s, err := r.Sheet("data")
	if err != nil {
		t.Fatal(err)
	}
	if s.Info().Name != "Data" || s.Info().Index != 0 {
		t.Fatalf("info=%+v", s.Info())
	}
	rows, err := s.Read(excel.ReadOptions{Columns: []excel.Column{
		{Header: "姓名", Key: "name"},
		{Header: "年龄", Key: "age", Type: excel.Int},
	}})
	t.Logf("Data rows=%v err=%v", rows, err)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["name"] != "张三" || rows[0]["age"] != "18" {
		t.Fatalf("rows=%v", rows)
	}

	stat, err := r.Sheet("统计")
	if err != nil {
		t.Fatal(err)
	}
	rows2, err := stat.Read(excel.ReadOptions{Columns: []excel.Column{
		{Header: "项目", Key: "item"},
		{Header: "值", Key: "value"},
	}})
	t.Logf("统计 rows=%v err=%v", rows2, err)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows2) != 1 || rows2[0]["item"] != "人数" || rows2[0]["value"] != "1" {
		t.Fatalf("rows2=%v", rows2)
	}
}

// TestSheetReaderEachStop 场景：Each 在第一行数据返回 ErrStop，只回调一次且返回 nil。
func TestSheetReaderEachStop(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"h"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"a"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"b"})
	})
	r, err := excel.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s, err := r.Sheet("")
	if err != nil {
		t.Fatal(err)
	}

	var got []string
	err = s.Each(excel.ReadOptions{Columns: []excel.Column{{Header: "h"}}}, func(rowNum int, row map[string]string) error {
		got = append(got, row["h"])
		return excel.ErrStop
	})
	t.Logf("got=%v err=%v", got, err)
	if err != nil {
		t.Fatalf("err=%v", err)
	}
	if len(got) != 1 || got[0] != "a" {
		t.Fatalf("got=%v", got)
	}
}

// TestReaderClose 场景：Close 幂等；关闭后 Sheets/Sheet/Read 返回 ErrClosed，Info 仍可用。
func TestReaderClose(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"h"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"a"})
	})
	r, err := excel.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	s, err := r.Sheet("")
	if err != nil {
		t.Fatal(err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("first close: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("second close: %v", err)
	}
	if _, err := r.Sheets(); !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("Sheets err=%v", err)
	}
	if _, err := r.Sheet(""); !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("Sheet err=%v", err)
	}
	if _, err := s.Read(excel.ReadOptions{Columns: []excel.Column{{Header: "h"}}}); !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("Read err=%v", err)
	}
	if s.Info().Name != "Sheet1" {
		t.Fatalf("Info after close=%+v", s.Info())
	}
}

// TestReadTrimSpaceWhitespaceRow 场景：TrimSpace 下纯空格行被跳过，Required 不误报。
func TestReadTrimSpaceWhitespaceRow(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"h"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"   "})
		f.SetSheetRow("Sheet1", "A3", &[]any{"a"})
	})
	r, err := excel.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s, err := r.Sheet("")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Read(excel.ReadOptions{
		TrimSpace: true,
		Columns:   []excel.Column{{Header: "h", Required: true}},
	})
	t.Logf("rows=%v err=%v", rows, err)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["h"] != "a" {
		t.Fatalf("rows=%v", rows)
	}
}

// TestReadHeaderWhitespaceDeclaration 场景：声明表头带空格/BOM 时清洗后匹配文件表头。
func TestReadHeaderWhitespaceDeclaration(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"姓名"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"张三"})
	})
	r, err := excel.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s, err := r.Sheet("")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Read(excel.ReadOptions{Columns: []excel.Column{{Header: " 姓名 "}}})
	t.Logf("rows=%v err=%v", rows, err)
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0]["姓名"] != "张三" {
		t.Fatalf("rows=%v", rows)
	}
}

// TestReaderReadAfterError 场景：A 表缺列报错后，同一个 Reader 仍能读 B 表。
func TestReaderReadAfterError(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		if err := f.SetSheetName("Sheet1", "A"); err != nil {
			t.Fatal(err)
		}
		f.SetSheetRow("A", "A1", &[]any{"a"})
		f.SetSheetRow("A", "A2", &[]any{"1"})
		if _, err := f.NewSheet("B"); err != nil {
			t.Fatal(err)
		}
		f.SetSheetRow("B", "A1", &[]any{"a", "b"})
		f.SetSheetRow("B", "A2", &[]any{"1", "2"})
	})
	r, err := excel.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()

	sA, err := r.Sheet("A")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := sA.Read(excel.ReadOptions{Columns: []excel.Column{{Header: "a"}, {Header: "b"}}}); !errors.Is(err, excel.ErrMissingColumn) {
		t.Fatalf("A err=%v, want ErrMissingColumn", err)
	}

	sB, err := r.Sheet("B")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := sB.Read(excel.ReadOptions{Columns: []excel.Column{{Header: "a"}, {Header: "b"}}})
	t.Logf("B rows=%v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0]["a"] != "1" || rows[0]["b"] != "2" {
		t.Fatalf("B rows=%v err=%v", rows, err)
	}
}

// TestReadHeaderBOMDeclaration 场景：声明表头带 UTF-8 BOM 时清洗后匹配文件表头。
func TestReadHeaderBOMDeclaration(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"姓名"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"张三"})
	})
	r, err := excel.Open(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s, err := r.Sheet("")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Read(excel.ReadOptions{Columns: []excel.Column{{Header: "\ufeff姓名"}}})
	t.Logf("rows=%v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0]["姓名"] != "张三" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}
