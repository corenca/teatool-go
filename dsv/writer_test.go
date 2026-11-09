package dsv_test

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/dsv"
)

// writeOptions 返回最常用的两列写出声明。
func writeOptions() dsv.WriteOptions {
	return dsv.WriteOptions{
		Delimiter: "!^",
		Columns: []dsv.Column{
			{Header: "姓名", Key: "name"},
			{Header: "年龄", Key: "age"},
		},
	}
}

// twoRows 返回两条可用于写出的数据。
func twoRows() []map[string]string {
	return []map[string]string{
		{"name": "张三", "age": "30"},
		{"name": "李四", "age": "40"},
	}
}

// 表头 + 两行：分隔符、顺序、行尾换行符合预期。
func TestWriteFileBasic(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := dsv.WriteFile(path, twoRows(), writeOptions()); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
	got := readFile(t, path)
	t.Logf("写出内容:\n%s", got)
	want := "姓名!^年龄\n张三!^30\n李四!^40\n"
	if got != want {
		t.Fatalf("内容不符\n得到: %q\n期望: %q", got, want)
	}
}

// NoHeader：不写表头。
func TestWriteNoHeader(t *testing.T) {
	opts := writeOptions()
	opts.NoHeader = true
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := dsv.WriteFile(path, twoRows(), opts); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
	got := readFile(t, path)
	t.Logf("写出内容:\n%s", got)
	if got != "张三!^30\n李四!^40\n" {
		t.Fatalf("无表头内容不符: %q", got)
	}
}

// TrailingDelimiter：表头与每行都以分隔符结尾。
func TestWriteTrailingDelimiter(t *testing.T) {
	opts := writeOptions()
	opts.TrailingDelimiter = true
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := dsv.WriteFile(path, twoRows()[:1], opts); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
	got := readFile(t, path)
	t.Logf("写出内容:\n%s", got)
	if got != "姓名!^年龄!^\n张三!^30!^\n" {
		t.Fatalf("末尾分隔符内容不符: %q", got)
	}
}

// LineEnding="\r\n"：每行以 CRLF 结尾。
func TestWriteCRLF(t *testing.T) {
	opts := writeOptions()
	opts.LineEnding = "\r\n"
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := dsv.WriteFile(path, twoRows()[:1], opts); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
	got := readFile(t, path)
	t.Logf("写出内容: %q", got)
	if got != "姓名!^年龄\r\n张三!^30\r\n" {
		t.Fatalf("CRLF 内容不符: %q", got)
	}
}

// Sanitize 把换行、分隔符替换成安全字符，写出后能被 Read 原样读回。
func TestWriteSanitize(t *testing.T) {
	opts := writeOptions()
	opts.Sanitize = strings.NewReplacer("\r\n", " ", "\n", " ", "!^", ",").Replace
	path := filepath.Join(t.TempDir(), "out.txt")
	rows := []map[string]string{{"name": "张!^三\n备注", "age": "30"}}
	if err := dsv.WriteFile(path, rows, opts); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
	got := readFile(t, path)
	t.Logf("写出内容: %q", got)
	if got != "姓名!^年龄\n张,三 备注!^30\n" {
		t.Fatalf("Sanitize 结果不符: %q", got)
	}

	back, err := dsv.Read(strings.NewReader(got), dsv.ReadOptions{Delimiter: "!^", Columns: writeOptions().Columns})
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	if back[0]["name"] != "张,三 备注" {
		t.Fatalf("读回内容不符: %#v", back)
	}
}

// Sanitize 之后仍含换行：*RowError，errors.Is ErrIllegalField。
func TestWriteIllegalField(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	rows := []map[string]string{{"name": "张\n三", "age": "30"}}
	err := dsv.WriteFile(path, rows, writeOptions())
	t.Logf("错误: %v", err)
	if !errors.Is(err, dsv.ErrIllegalField) {
		t.Fatalf("期望 ErrIllegalField，得到 %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("失败时不应产生目标文件，stat 错误: %v", statErr)
	}
}

// 缺 Key：*RowError，errors.Is ErrMissingKey；文件不落盘。
func TestWriteMissingKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	rows := []map[string]string{{"name": "张三"}}
	err := dsv.WriteFile(path, rows, writeOptions())
	t.Logf("错误: %v", err)
	if !errors.Is(err, dsv.ErrMissingKey) {
		t.Fatalf("期望 ErrMissingKey，得到 %v", err)
	}
	var rowErr *dsv.RowError
	if !errors.As(err, &rowErr) || rowErr.Column != "age" || rowErr.Line != 2 {
		t.Fatalf("期望第 2 行 age 列的 *RowError，得到 %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, fs.ErrNotExist) {
		t.Fatalf("失败时不应产生目标文件，stat 错误: %v", statErr)
	}
}

// Abort：不产生目标文件；已存在的目标文件内容不变。
func TestWriteAbort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := os.WriteFile(path, []byte("old"), 0o644); err != nil {
		t.Fatal(err)
	}
	w, err := dsv.NewFileWriter(path, writeOptions())
	if err != nil {
		t.Fatalf("NewFileWriter 失败: %v", err)
	}
	if err := w.Write(twoRows()[0]); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := w.Abort(); err != nil {
		t.Fatalf("Abort 失败: %v", err)
	}
	if got := readFile(t, path); got != "old" {
		t.Fatalf("Abort 后目标文件应保持不变，得到 %q", got)
	}
}

// 失败态：写失败后再次 Write 返回同一错误，Close 也返回它。
func TestWriterStickyFailure(t *testing.T) {
	var buf bytes.Buffer
	w, err := dsv.NewWriter(&buf, writeOptions())
	if err != nil {
		t.Fatalf("NewWriter 失败: %v", err)
	}
	first := w.Write(map[string]string{"name": "张三"})
	second := w.Write(twoRows()[0])
	t.Logf("第一次: %v；第二次: %v", first, second)
	if !errors.Is(first, dsv.ErrMissingKey) || !errors.Is(second, dsv.ErrMissingKey) {
		t.Fatalf("失败态应返回同一类错误: %v / %v", first, second)
	}
	if err := w.Close(); !errors.Is(err, dsv.ErrMissingKey) {
		t.Fatalf("Close 应返回首次错误，得到 %v", err)
	}
}

// Close 后 Write 返回 ErrClosed；重复 Close 返回 nil。
func TestWriterClosed(t *testing.T) {
	var buf bytes.Buffer
	w, err := dsv.NewWriter(&buf, writeOptions())
	if err != nil {
		t.Fatalf("NewWriter 失败: %v", err)
	}
	if err := w.Write(twoRows()[0]); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if err := w.Write(twoRows()[1]); !errors.Is(err, dsv.ErrClosed) {
		t.Fatalf("Close 后 Write 应返回 ErrClosed，得到 %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("重复 Close 应返回 nil，得到 %v", err)
	}
}

// 目标文件已存在且本次写入失败：原文件内容保持不变（原子落盘）。
func TestWriteAtomicKeepsOldFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	if err := os.WriteFile(path, []byte("old-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	err := dsv.WriteFile(path, []map[string]string{{"name": "张三"}}, writeOptions())
	t.Logf("错误: %v", err)
	if !errors.Is(err, dsv.ErrMissingKey) {
		t.Fatalf("期望 ErrMissingKey，得到 %v", err)
	}
	if got := readFile(t, path); got != "old-content" {
		t.Fatalf("失败后目标文件应保持不变，得到 %q", got)
	}
}

// 非法选项：Columns 为空、分隔符为空、LineEnding 非法、表头重复。
func TestWriteInvalidOptions(t *testing.T) {
	tests := []struct {
		name string
		opts dsv.WriteOptions
		want error
	}{
		{"Columns 为空", dsv.WriteOptions{Delimiter: "!^"}, dsv.ErrInvalidColumn},
		{"分隔符为空", dsv.WriteOptions{Columns: []dsv.Column{{Key: "a"}}}, dsv.ErrInvalidOption},
		{"LineEnding 非法", dsv.WriteOptions{Delimiter: "!^", LineEnding: "x", Columns: []dsv.Column{{Key: "a"}}}, dsv.ErrInvalidOption},
		{"写出表头重复", dsv.WriteOptions{Delimiter: "!^", Columns: []dsv.Column{
			{Header: "同名", Key: "a"}, {Header: "同名", Key: "b"},
		}}, dsv.ErrDuplicateColumn},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			_, err := dsv.NewWriter(&buf, tt.opts)
			t.Logf("错误: %v", err)
			if !errors.Is(err, tt.want) {
				t.Fatalf("期望 %v，得到 %v", tt.want, err)
			}
		})
	}
}

// breakTemp 把当前写出器的临时文件替换成非空目录，让后续 Remove 必然失败。
func breakTemp(t *testing.T, dir string) {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".dsv-*"))
	if err != nil || len(matches) != 1 {
		t.Fatalf("找不到唯一的临时文件: %v %v", matches, err)
	}
	tmp := matches[0]
	if err := os.Remove(tmp); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(tmp, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(tmp, "keep"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(tmp) })
}

// Abort 删除临时文件失败时应返回错误，而不是静默成功。
func TestAbortReportsCleanupFailure(t *testing.T) {
	dir := t.TempDir()
	w, err := dsv.NewFileWriter(filepath.Join(dir, "out.txt"), writeOptions())
	if err != nil {
		t.Fatalf("NewFileWriter 失败: %v", err)
	}
	breakTemp(t, dir)

	err = w.Abort()
	t.Logf("清理错误: %v", err)
	if err == nil {
		t.Fatal("清理失败应返回错误")
	}
	if _, ok := errors.AsType[*fs.PathError](err); !ok {
		t.Fatalf("期望 *fs.PathError，得到 %T: %v", err, err)
	}
}

// Close 在失败路径上应把清理错误并入返回值，原业务错误仍可用 errors.Is 取到。
func TestCloseJoinsCleanupFailure(t *testing.T) {
	dir := t.TempDir()
	w, err := dsv.NewFileWriter(filepath.Join(dir, "out.txt"), writeOptions())
	if err != nil {
		t.Fatalf("NewFileWriter 失败: %v", err)
	}
	if err := w.Write(map[string]string{"name": "张三"}); !errors.Is(err, dsv.ErrMissingKey) {
		t.Fatalf("准备失败态时出错: %v", err)
	}
	breakTemp(t, dir)

	err = w.Close()
	t.Logf("关闭错误: %v", err)
	if !errors.Is(err, dsv.ErrMissingKey) {
		t.Fatalf("应保留原业务错误，得到 %v", err)
	}
	if _, ok := errors.AsType[*fs.PathError](err); !ok {
		t.Fatalf("清理错误未并入 Close 返回值: %v", err)
	}
}

// Sanitize 也作用于表头：含分隔符的表头被替换后可正常写出。
func TestWriteSanitizesHeader(t *testing.T) {
	opts := dsv.WriteOptions{
		Delimiter: "!^",
		Columns:   []dsv.Column{{Header: "姓名!^别名", Key: "name"}},
		Sanitize:  strings.NewReplacer("!^", ",").Replace,
	}
	var buf bytes.Buffer
	w, err := dsv.NewWriter(&buf, opts)
	if err != nil {
		t.Fatalf("NewWriter 失败: %v", err)
	}
	if err := w.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	t.Logf("写出内容: %q", buf.String())
	if buf.String() != "姓名,别名\n" {
		t.Fatalf("表头 Sanitize 结果不符: %q", buf.String())
	}

	unsafe := opts
	unsafe.Sanitize = nil
	if _, err := dsv.NewWriter(&bytes.Buffer{}, unsafe); !errors.Is(err, dsv.ErrIllegalField) {
		t.Fatalf("未替换的表头应报 ErrIllegalField，得到 %v", err)
	}
}

// 失败态返回的是第一次的错误实例本身。
func TestWriterStickyFailureSameInstance(t *testing.T) {
	var buf bytes.Buffer
	w, err := dsv.NewWriter(&buf, writeOptions())
	if err != nil {
		t.Fatalf("NewWriter 失败: %v", err)
	}
	first := w.Write(map[string]string{"name": "张三"})
	second := w.Write(twoRows()[0])
	t.Logf("第一次: %v；第二次: %v", first, second)
	if first == nil || second != first {
		t.Fatalf("失败态应返回同一错误实例: %v / %v", first, second)
	}
}

// TrailingDelimiter + NoHeader：写出后能被同样选项读回。
func TestTrailingDelimiterNoHeaderRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "out.txt")
	opts := dsv.WriteOptions{
		Delimiter:         "!^",
		NoHeader:          true,
		TrailingDelimiter: true,
		Columns: []dsv.Column{
			{Header: "姓名", Key: "name"},
			{Header: "年龄", Key: "age"},
		},
	}
	if err := dsv.WriteFile(path, twoRows(), opts); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
	t.Logf("写出内容: %q", readFile(t, path))

	rows, err := dsv.ReadFile(path, dsv.ReadOptions{
		Delimiter:         "!^",
		NoHeader:          true,
		TrailingDelimiter: true,
		Columns:           opts.Columns,
	})
	if err != nil {
		t.Fatalf("读回失败: %v", err)
	}
	t.Logf("读回内容: %#v", rows)
	if len(rows) != 2 || rows[0]["name"] != "张三" || rows[1]["age"] != "40" {
		t.Fatalf("往返结果不符: %#v", rows)
	}
}
