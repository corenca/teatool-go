package excel_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/corenca/teatool-go/excel"
)

// TestReadFileOneShot 场景：文件一行入口按名字/第一个 sheet 读取。
func TestReadFileOneShot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.xlsx")
	b, err := excel.NewFileBuilder(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddSheetRows("用户", []map[string]string{{"name": "张三", "age": "18"}}, excel.WriteOptions{Columns: builderCols()}); err != nil {
		t.Fatal(err)
	}
	if err := b.AddSheetRows("统计", []map[string]string{{"name": "总计", "age": "2"}}, excel.WriteOptions{Columns: builderCols()}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	rows, err := excel.ReadFile(path, "", excel.ReadOptions{Columns: builderCols()})
	t.Logf("first rows=%v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0]["name"] != "张三" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
	rows, err = excel.ReadFile(path, "统计", excel.ReadOptions{Columns: builderCols()})
	if err != nil || len(rows) != 1 || rows[0]["name"] != "总计" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

// TestReadFileIntoOneShot 场景：文件一行入口按 sheet 读结构体。
func TestReadFileIntoOneShot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.xlsx")
	b, err := excel.NewFileBuilder(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.WriteFrom("用户", []builderUser{{Name: "张三", Age: 18}}, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	rows, err := excel.ReadFileInto[builderUser](path, "用户", excel.ReadOptions{})
	t.Logf("rows=%+v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0] != (builderUser{Name: "张三", Age: 18}) {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
	if _, err := excel.ReadFileInto[builderUser](path, "用户", excel.ReadOptions{Columns: builderCols()}); !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("Columns err=%v", err)
	}
}

// TestReadFileMissingSheet 场景：文件一行入口请求不存在的 sheet 返回 ErrSheetNotFound。
func TestReadFileMissingSheet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "in.xlsx")
	b, err := excel.NewFileBuilder(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddSheetRows("A", []map[string]string{{"name": "x", "age": "1"}}, excel.WriteOptions{Columns: builderCols()}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	_, err = excel.ReadFile(path, "不存在", excel.ReadOptions{Columns: builderCols()})
	if !errors.Is(err, excel.ErrSheetNotFound) {
		t.Fatalf("err=%v, want ErrSheetNotFound", err)
	}
}

// TestWriteFileOneShot 场景：文件一行入口写单 sheet 并可读回。
func TestWriteFileOneShot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.xlsx")
	if err := excel.WriteFile(path, "统计", []map[string]string{{"name": "总计", "age": "2"}}, excel.WriteOptions{Columns: builderCols()}); err != nil {
		t.Fatal(err)
	}
	rows, err := excel.ReadFile(path, "统计", excel.ReadOptions{Columns: builderCols()})
	t.Logf("rows=%v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0]["name"] != "总计" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

// TestWriteFileEmptySheet 场景：写侧空 sheet 名返回 ErrInvalidSheetName 且不创建文件。
func TestWriteFileEmptySheet(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.xlsx")
	err := excel.WriteFile(path, "", []map[string]string{{"name": "x", "age": "1"}}, excel.WriteOptions{Columns: builderCols()})
	if !errors.Is(err, excel.ErrInvalidSheetName) {
		t.Fatalf("err=%v, want ErrInvalidSheetName", err)
	}
	if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
		t.Fatalf("file exists or stat err=%v", statErr)
	}
}

// TestWriteFileFromOneShot 场景：文件一行入口写结构体并可读回。
func TestWriteFileFromOneShot(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.xlsx")
	if err := excel.WriteFileFrom(path, "用户", []builderUser{{Name: "张三", Age: 18}}, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	rows, err := excel.ReadFileInto[builderUser](path, "用户", excel.ReadOptions{})
	t.Logf("rows=%+v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0] != (builderUser{Name: "张三", Age: 18}) {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

// TestWriteFileAtomicOnError 场景：写失败时目标文件保持原内容。
func TestWriteFileAtomicOnError(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.xlsx")
	if err := excel.WriteFile(path, "Sheet1", []map[string]string{{"name": "old", "age": "1"}}, excel.WriteOptions{Columns: builderCols()}); err != nil {
		t.Fatal(err)
	}
	err := excel.WriteFile(path, "Sheet1", []map[string]string{{"name": "new"}}, excel.WriteOptions{Columns: builderCols()})
	if !errors.Is(err, excel.ErrMissingKey) {
		t.Fatalf("err=%v, want ErrMissingKey", err)
	}
	rows, err := excel.ReadFile(path, "Sheet1", excel.ReadOptions{Columns: builderCols()})
	t.Logf("after error rows=%v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0]["name"] != "old" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}
