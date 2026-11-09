package dsv_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/corenca/teatool-go/dsv"
)

// outRow 是结构体写出的主用例类型。
type outRow struct {
	Name  string    `dsv:"姓名"`
	Age   int       `dsv:"年龄"`
	Score *float64  `dsv:"分数"`
	Birth time.Time `dsv:"生日,layout:2006-01-02"`
	Nick  string    `dsv:"-"`
}

// wrapped 是 encoding.TextMarshaler 自定义类型。
type wrapped string

// MarshalText 给文本加尖括号，验证结构体层会调用文本接口。
func (w wrapped) MarshalText() ([]byte, error) {
	return []byte("<" + string(w) + ">"), nil
}

// wrappedRow 使用自定义文本类型。
type wrappedRow struct {
	V wrapped `dsv:"v"`
}

// outRows 返回两条结构体数据，覆盖 nil 指针与零值指针。
func outRows() []outRow {
	zero := 0.0
	return []outRow{
		{Name: "张三", Age: 30, Birth: time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)},
		{Name: "李四", Age: 40, Score: &zero, Birth: time.Date(2001, 1, 2, 0, 0, 0, 0, time.UTC)},
	}
}

// WriteFileFrom：表头来自 tag，指针 nil 写空、&0 写 "0"，time 按 layout。
func TestWriteFileFrom(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := dsv.WriteFileFrom(path, outRows(), dsv.WriteOptions{Delimiter: "!^"}); err != nil {
		t.Fatalf("WriteFileFrom 失败: %v", err)
	}
	got := readFile(t, path)
	t.Logf("写出内容:\n%s", got)
	want := "姓名!^年龄!^分数!^生日\n张三!^30!^!^2000-01-02\n李四!^40!^0!^2001-01-02\n"
	if got != want {
		t.Fatalf("内容不符\n得到: %q\n期望: %q", got, want)
	}
}

// StructWriter 逐条流式写：写入顺序与行序一致。
func TestStructWriterStreaming(t *testing.T) {
	var buf bytes.Buffer
	w, err := dsv.NewStructWriter[outRow](&buf, dsv.WriteOptions{Delimiter: "!^"})
	if err != nil {
		t.Fatalf("NewStructWriter 失败: %v", err)
	}
	for _, row := range outRows() {
		if err := w.Write(row); err != nil {
			t.Fatalf("Write 失败: %v", err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	got := buf.String()
	t.Logf("写出内容:\n%s", got)
	if !strings.HasPrefix(got, "姓名!^年龄!^分数!^生日\n张三!^30") {
		t.Fatalf("流式写出内容不符: %q", got)
	}
}

// TextMarshaler 自定义类型可写。
func TestWriteFromTextMarshaler(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := dsv.WriteFileFrom(path, []wrappedRow{{V: "abc"}}, dsv.WriteOptions{Delimiter: "!^"}); err != nil {
		t.Fatalf("WriteFileFrom 失败: %v", err)
	}
	got := readFile(t, path)
	t.Logf("写出内容: %q", got)
	if got != "v\n<abc>\n" {
		t.Fatalf("自定义类型写出不符: %q", got)
	}
}

// opts.Columns 非空：ErrInvalidColumn。
func TestWriteFromRejectsColumns(t *testing.T) {
	var buf bytes.Buffer
	opts := dsv.WriteOptions{Delimiter: "!^", Columns: []dsv.Column{{Header: "x"}}}
	_, err := dsv.NewStructWriter[outRow](&buf, opts)
	t.Logf("错误: %v", err)
	if !errors.Is(err, dsv.ErrInvalidColumn) {
		t.Fatalf("期望 ErrInvalidColumn，得到 %v", err)
	}
}

// Sanitize 处理后写出的文件能被 ReadInto 读回。
func TestWriteFromRoundTrip(t *testing.T) {
	opts := dsv.WriteOptions{
		Delimiter: "!^",
		Sanitize:  strings.NewReplacer("\n", " ", "!^", ",").Replace,
	}
	path := filepath.Join(t.TempDir(), "out.txt")
	rows := []outRow{{Name: "张!^三\n备注", Age: 30, Birth: time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)}}
	if err := dsv.WriteFileFrom(path, rows, opts); err != nil {
		t.Fatalf("WriteFileFrom 失败: %v", err)
	}
	back, err := dsv.ReadFileInto[outRow](path, dsv.ReadOptions{Delimiter: "!^"})
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	t.Logf("读回内容: %+v", back)
	if len(back) != 1 || back[0].Name != "张,三 备注" || back[0].Age != 30 {
		t.Fatalf("往返结果不符: %+v", back)
	}
}

// 空 rows：生成只有表头的文件。
func TestWriteFromEmptyRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := dsv.WriteFileFrom[outRow](path, nil, dsv.WriteOptions{Delimiter: "!^"}); err != nil {
		t.Fatalf("WriteFileFrom 失败: %v", err)
	}
	got := readFile(t, path)
	t.Logf("写出内容: %q", got)
	if got != "姓名!^年龄!^分数!^生日\n" {
		t.Fatalf("空数据应只有表头，得到 %q", got)
	}
}

// 非法字段（Sanitize 后仍含分隔符）：ErrIllegalField，目标文件不产生。
func TestWriteFromIllegalField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	rows := []outRow{{Name: "张!^三", Age: 30, Birth: time.Date(2000, 1, 2, 0, 0, 0, 0, time.UTC)}}
	err := dsv.WriteFileFrom(path, rows, dsv.WriteOptions{Delimiter: "!^"})
	t.Logf("错误: %v", err)
	if !errors.Is(err, dsv.ErrIllegalField) {
		t.Fatalf("期望 ErrIllegalField，得到 %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("失败时不应产生目标文件，stat 错误: %v", statErr)
	}
}
