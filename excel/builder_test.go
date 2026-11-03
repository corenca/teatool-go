package excel_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/excel"
	"github.com/xuri/excelize/v2"
)

// builderCols 是两个 sheet 共用的简单列声明。
func builderCols() []excel.Column {
	return []excel.Column{
		{Header: "姓名", Key: "name"},
		{Header: "年龄", Key: "age"},
	}
}

// TestBuilderTwoSheets 场景：Builder 顺序添加两个 sheet，读回名字、顺序与数据。
func TestBuilderTwoSheets(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	sw1, err := b.AddSheet("用户", excel.WriteOptions{Columns: builderCols(), HeaderBold: true})
	if err != nil {
		t.Fatal(err)
	}
	if err := sw1.Write(map[string]string{"name": "张三", "age": "18"}); err != nil {
		t.Fatal(err)
	}
	sw2, err := b.AddSheet("统计", excel.WriteOptions{Columns: builderCols()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sw2.Write(map[string]string{"name": "总计", "age": "2"}); err != nil {
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
	infos, err := r.Sheets()
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("sheets=%+v", infos)
	if len(infos) != 2 || infos[0].Name != "用户" || infos[1].Name != "统计" {
		t.Fatalf("sheets=%+v", infos)
	}
	s, err := r.Sheet("用户")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Read(excel.ReadOptions{Columns: builderCols()})
	t.Logf("用户 rows=%v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0]["name"] != "张三" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

// TestBuilderNameRules 场景：非法 sheet 名返回 ErrInvalidSheetName，且不污染 Builder。
func TestBuilderNameRules(t *testing.T) {
	cases := []struct {
		name string
		bad  string
	}{
		{"空名", ""},
		{"32 个 ASCII", strings.Repeat("a", 32)},
		{"16 个 emoji", strings.Repeat("😀", 16)},
		{"首单引号", "'tmp"},
		{"尾单引号", "tmp'"},
		{"方括号", "A[B]"},
		{"冒号", "A:B"},
	}
	opts := excel.WriteOptions{Columns: builderCols()}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b, err := excel.NewBuilder(io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := b.AddSheet(c.bad, opts); !errors.Is(err, excel.ErrInvalidSheetName) {
				t.Fatalf("err=%v, want ErrInvalidSheetName", err)
			}
			if _, err := b.AddSheet("ok", opts); err != nil {
				t.Fatalf("after invalid name: %v", err)
			}
			if err := b.Close(); err != nil {
				t.Fatal(err)
			}
		})
	}

	b, err := excel.NewBuilder(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddSheet("Foo", opts); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddSheet("foo", opts); !errors.Is(err, excel.ErrSheetExists) {
		t.Fatalf("duplicate err=%v, want ErrSheetExists", err)
	}
	b2, err := excel.NewBuilder(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b2.AddSheet("Σ", opts); err != nil {
		t.Fatal(err)
	}
	if _, err := b2.AddSheet("ς", opts); !errors.Is(err, excel.ErrSheetExists) {
		t.Fatalf("unicode duplicate err=%v, want ErrSheetExists", err)
	}
	if err := b2.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddSheet("ok", opts); err != nil {
		t.Fatalf("after duplicate: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestNewBuilderInvalidTarget 场景：nil writer 或空路径在构造期报 ErrInvalidColumn。
func TestNewBuilderInvalidTarget(t *testing.T) {
	if _, err := excel.NewBuilder(nil); !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("nil writer err=%v", err)
	}
	if _, err := excel.NewFileBuilder(""); !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("empty path err=%v", err)
	}
}

// TestBuilderAutoClose 场景：AddSheet 自动 flush 上一个 sheet，旧句柄再写返回 ErrClosed。
func TestBuilderAutoClose(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	sw1, err := b.AddSheet("A", excel.WriteOptions{Columns: builderCols()})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddSheet("B", excel.WriteOptions{Columns: builderCols()}); err != nil {
		t.Fatal(err)
	}
	if err := sw1.Write(map[string]string{"name": "x", "age": "1"}); !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("sw1.Write err=%v, want ErrClosed", err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestBuilderFailedCloseNoOutput 场景：缺 Key 写失败后 Close 返回原错误且不产出文件。
func TestBuilderFailedCloseNoOutput(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	sw, err := b.AddSheet("A", excel.WriteOptions{Columns: builderCols()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sw.Write(map[string]string{"name": "x"}); !errors.Is(err, excel.ErrMissingKey) {
		t.Fatalf("Write err=%v, want ErrMissingKey", err)
	}
	err = b.Close()
	t.Logf("Close err=%v buf=%d", err, buf.Len())
	if !errors.Is(err, excel.ErrMissingKey) {
		t.Fatalf("Close err=%v, want ErrMissingKey", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("buf len=%d, want 0", buf.Len())
	}
	if err := b.Abort(); err != nil {
		t.Fatalf("Abort: %v", err)
	}
}

// TestBuilderCloseEmptyRetry 场景：无 sheet 时 Close 返回 ErrEmptyWorkbook 但不关闭，可重试。
func TestBuilderCloseEmptyRetry(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); !errors.Is(err, excel.ErrEmptyWorkbook) {
		t.Fatalf("Close err=%v, want ErrEmptyWorkbook", err)
	}
	sw, err := b.AddSheet("A", excel.WriteOptions{Columns: builderCols()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sw.Write(map[string]string{"name": "x", "age": "1"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatalf("retry Close: %v", err)
	}
	if buf.Len() == 0 {
		t.Fatal("buffer empty")
	}
}

// TestBuilderAbort 场景：Abort 不产出文件，之后 Close 返回 ErrClosed。
func TestBuilderAbort(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	sw, err := b.AddSheet("A", excel.WriteOptions{Columns: builderCols()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sw.Write(map[string]string{"name": "x", "age": "1"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Abort(); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 0 {
		t.Fatalf("buf len=%d, want 0", buf.Len())
	}
	if err := b.Close(); !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("Close err=%v, want ErrClosed", err)
	}
}

// TestBuilderFilePermissions 场景：NewFileBuilder 产物权限为 0644。
func TestBuilderFilePermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 不支持 POSIX 权限")
	}
	path := filepath.Join(t.TempDir(), "out.xlsx")
	b, err := excel.NewFileBuilder(path)
	if err != nil {
		t.Fatal(err)
	}
	sw, err := b.AddSheet("A", excel.WriteOptions{Columns: builderCols()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sw.Write(map[string]string{"name": "x", "age": "1"}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("perm=%04o", info.Mode().Perm())
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("perm=%04o, want 0644", info.Mode().Perm())
	}
}

// builderUser 是 Builder.WriteFrom 测试用的结构体。
type builderUser struct {
	Name string `excel:"姓名"`
	Age  int    `excel:"年龄"`
}

// TestBuilderAddSheetRows 场景：map 批量写两个 sheet，读回数据。
func TestBuilderAddSheetRows(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
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

	r, err := excel.Open(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer r.Close()
	s, err := r.Sheet("统计")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Read(excel.ReadOptions{Columns: builderCols()})
	t.Logf("统计 rows=%v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0]["name"] != "总计" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

// TestBuilderAddSheetRowsMissingKeyNoOutput 场景：批量写缺 Key 失败后不产出文件。
func TestBuilderAddSheetRowsMissingKeyNoOutput(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	err = b.AddSheetRows("用户", []map[string]string{{"name": "张三"}}, excel.WriteOptions{Columns: builderCols()})
	if !errors.Is(err, excel.ErrMissingKey) {
		t.Fatalf("AddSheetRows err=%v, want ErrMissingKey", err)
	}
	if err := b.Close(); !errors.Is(err, excel.ErrMissingKey) {
		t.Fatalf("Close err=%v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("buf len=%d, want 0", buf.Len())
	}
}

// TestBuilderWriteFrom 场景：结构体批量写两个 sheet，字段顺序与 tag 正确。
func TestBuilderWriteFrom(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.WriteFrom("用户", []builderUser{{Name: "张三", Age: 18}}, excel.WriteOptions{}); err != nil {
		t.Fatal(err)
	}
	if err := b.WriteFrom("统计", []builderUser{{Name: "总计", Age: 2}}, excel.WriteOptions{}); err != nil {
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
	s, err := r.Sheet("用户")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.ReadInto[builderUser](excel.ReadOptions{})
	t.Logf("用户 rows=%+v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0] != (builderUser{Name: "张三", Age: 18}) {
		t.Fatalf("rows=%+v err=%v", rows, err)
	}
}

// TestBuilderWriteFromColumnsRejected 场景：结构体写拒绝 opts.Columns。
func TestBuilderWriteFromColumnsRejected(t *testing.T) {
	b, err := excel.NewBuilder(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	err = b.WriteFrom("用户", []builderUser{{Name: "x"}}, excel.WriteOptions{Columns: builderCols()})
	if !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("err=%v, want ErrInvalidColumn", err)
	}
	if err := b.Abort(); err != nil {
		t.Fatal(err)
	}
}

// TestBuilderHeaderStyleCached 场景：两个 sheet 都开 HeaderBold，表头都加粗。
func TestBuilderHeaderStyleCached(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	opts := excel.WriteOptions{Columns: builderCols(), HeaderBold: true}
	if err := b.AddSheetRows("用户", []map[string]string{{"name": "张三", "age": "18"}}, opts); err != nil {
		t.Fatal(err)
	}
	if err := b.AddSheetRows("统计", []map[string]string{{"name": "总计", "age": "2"}}, opts); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}

	f, err := excelize.OpenReader(bytes.NewReader(buf.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	for _, sheet := range []string{"用户", "统计"} {
		styleID, err := f.GetCellStyle(sheet, "A1")
		if err != nil {
			t.Fatal(err)
		}
		style, err := f.GetStyle(styleID)
		if err != nil {
			t.Fatal(err)
		}
		if style.Font == nil || !style.Font.Bold {
			t.Fatalf("%s A1 style=%+v, want bold", sheet, style)
		}
		t.Logf("%s A1 bold=true styleID=%d", sheet, styleID)
	}
}

// TestBuilderFlushOnce 场景：SheetWriter.Close 两次不重复 Flush，最终文件仍可读。
func TestBuilderFlushOnce(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	sw, err := b.AddSheet("A", excel.WriteOptions{Columns: builderCols()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sw.Write(map[string]string{"name": "x", "age": "1"}); err != nil {
		t.Fatal(err)
	}
	if err := sw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := sw.Close(); err != nil {
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
	s, err := r.Sheet("A")
	if err != nil {
		t.Fatal(err)
	}
	rows, err := s.Read(excel.ReadOptions{Columns: builderCols()})
	t.Logf("rows=%v err=%v", rows, err)
	if err != nil || len(rows) != 1 || rows[0]["name"] != "x" {
		t.Fatalf("rows=%v err=%v", rows, err)
	}
}

// TestBuilderCloseTwice 场景：Builder.Close 第二次返回 ErrClosed。
func TestBuilderCloseTwice(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddSheetRows("A", []map[string]string{{"name": "x", "age": "1"}}, excel.WriteOptions{Columns: builderCols()}); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
	if err := b.Close(); !errors.Is(err, excel.ErrClosed) {
		t.Fatalf("second Close err=%v, want ErrClosed", err)
	}
}

// TestBuilderSheetCloseAfterFailure 场景：失败后 SheetWriter.Close 返回原错误，再调用幂等。
func TestBuilderSheetCloseAfterFailure(t *testing.T) {
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	sw, err := b.AddSheet("A", excel.WriteOptions{Columns: builderCols()})
	if err != nil {
		t.Fatal(err)
	}
	if err := sw.Write(map[string]string{"name": "x"}); !errors.Is(err, excel.ErrMissingKey) {
		t.Fatalf("Write err=%v", err)
	}
	if err := sw.Close(); !errors.Is(err, excel.ErrMissingKey) {
		t.Fatalf("first Close err=%v, want ErrMissingKey", err)
	}
	if err := sw.Close(); err != nil {
		t.Fatalf("second Close err=%v", err)
	}
	if err := b.Close(); !errors.Is(err, excel.ErrMissingKey) {
		t.Fatalf("Builder.Close err=%v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("buf len=%d, want 0", buf.Len())
	}
}

// TestNewFileBuilderAbortNoFile 场景：NewFileBuilder 被 Abort 后不产出文件。
func TestNewFileBuilderAbortNoFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "abort.xlsx")
	b, err := excel.NewFileBuilder(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.AddSheetRows("A", []map[string]string{{"name": "x", "age": "1"}}, excel.WriteOptions{Columns: builderCols()}); err != nil {
		t.Fatal(err)
	}
	if err := b.Abort(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("file exists or stat err=%v", err)
	}
}

// TestBuilderAddSheetInvalidColumnsNotPoison 场景：列声明错误后 Builder 仍可添加正常 sheet。
func TestBuilderAddSheetInvalidColumnsNotPoison(t *testing.T) {
	b, err := excel.NewBuilder(io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := b.AddSheet("A", excel.WriteOptions{}); !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("invalid columns err=%v", err)
	}
	if _, err := b.AddSheet("ok", excel.WriteOptions{Columns: builderCols()}); err != nil {
		t.Fatalf("after invalid columns: %v", err)
	}
	if err := b.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestBuilderWriteFromAssemblyErrorNoOutput 场景：结构体装配错误时不产出文件。
func TestBuilderWriteFromAssemblyErrorNoOutput(t *testing.T) {
	type bad struct {
		X []string `excel:"x"`
	}
	var buf bytes.Buffer
	b, err := excel.NewBuilder(&buf)
	if err != nil {
		t.Fatal(err)
	}
	err = b.WriteFrom("A", []bad{{}}, excel.WriteOptions{})
	if !errors.Is(err, excel.ErrInvalidColumn) {
		t.Fatalf("WriteFrom err=%v", err)
	}
	if err := b.Close(); !errors.Is(err, excel.ErrEmptyWorkbook) {
		t.Fatalf("Close err=%v", err)
	}
	if buf.Len() != 0 {
		t.Fatalf("buf len=%d, want 0", buf.Len())
	}
}
