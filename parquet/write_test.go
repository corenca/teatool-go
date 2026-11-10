package parquet_test

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	pq "github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"

	"github.com/corenca/teatool-go/parquet"
)

// errWriter 在第 n 次写入后开始返回错误，用于验证失败态。
type errWriter struct {
	remaining int
}

// Write 实现 io.Writer。
func (w *errWriter) Write(p []byte) (int, error) {
	if w.remaining <= 0 {
		return 0, errWriteFailed
	}
	w.remaining--
	return len(p), nil
}

// errWriteFailed 是注入的写入错误。
var errWriteFailed = errors.New("模拟磁盘写入失败")

// TestWriteFileRoundTrip 验证结构体写出后能读回相同的值。
func TestWriteFileRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "roundtrip.parquet")
	note := "备注"
	rows := []allKindsRow{
		{ID: 1, Name: "甲", Score: 1.5, Active: true, At: sampleTime, Tags: []string{"a"}, Note: &note, Addr: address{City: "北京"}},
		{ID: 2, Name: "乙", Score: 2.5, Active: false, At: sampleTime, Tags: nil, Note: nil, Addr: address{City: "上海"}},
	}
	if err := parquet.WriteFile(path, rows, parquet.WriteOptions{}); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}

	got, err := parquet.ReadFile[allKindsRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if len(got) != 2 || got[0].ID != 1 || got[1].Name != "乙" {
		t.Fatalf("读回结果不对: %+v", got)
	}
	if !got[0].At.Equal(sampleTime) || got[0].Addr.City != "北京" {
		t.Errorf("时间/嵌套字段不对: %+v", got[0])
	}
}

// TestWriteFileAtomic 验证写出只留目标文件、权限 0644、没有临时文件残留。
func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "atomic.parquet")
	if err := parquet.WriteFile(path, []sampleRow{{ID: 1, Name: "a"}}, parquet.WriteOptions{}); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "atomic.parquet" {
		t.Fatalf("目录内容 = %v, 期望只有 atomic.parquet", names(entries))
	}
	info, err := entries[0].Info()
	if err != nil {
		t.Fatalf("读取权限失败: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o644 {
		t.Errorf("权限 = %o, 期望 644", perm)
	}
}

// names 提取目录项名称，便于断言失败时输出。
func names(entries []os.DirEntry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Name()
	}
	return out
}

// TestWriteAbort 验证 Abort 删除临时文件且不产生目标文件。
func TestWriteAbort(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "abort.parquet")

	w, err := parquet.NewFileWriter[sampleRow](path, parquet.WriteOptions{})
	if err != nil {
		t.Fatalf("NewFileWriter 失败: %v", err)
	}
	if err := w.Write([]sampleRow{{ID: 1, Name: "a"}}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := w.Abort(); err != nil {
		t.Fatalf("Abort 失败: %v", err)
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("Abort 后目标文件仍存在: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("Abort 后有残留文件: %v", names(entries))
	}
}

// TestWriteIOFailure 验证写入端的 IO 错误由 Close 返回。
//
// 上游把页缓冲在内存里，flush 发生在 Close（或行组提交）时，所以磁盘错误
// 通常不会从 Write 返回；失败后重复 Close/Abort 不应再报错。
func TestWriteIOFailure(t *testing.T) {
	bad := &errWriter{}
	nw, err := parquet.NewWriter[sampleRow](bad, parquet.WriteOptions{})
	if err != nil {
		t.Fatalf("NewWriter 失败: %v", err)
	}
	if err := nw.Write([]sampleRow{{ID: 1, Name: "a"}}); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := nw.Close(); !errors.Is(err, errWriteFailed) {
		t.Fatalf("Close err = %v, 期望写入错误", err)
	}
	if err := nw.Close(); err != nil {
		t.Errorf("重复 Close err = %v, 期望 nil", err)
	}
	if err := nw.Abort(); err != nil {
		t.Errorf("Abort err = %v, 期望 nil", err)
	}
}

// TestWriteCompression 验证每种压缩名都真实生效并能读回。
func TestWriteCompression(t *testing.T) {
	cases := []struct {
		name  string
		codec format.CompressionCodec
	}{
		{"", format.Snappy},
		{"snappy", format.Snappy},
		{"zstd", format.Zstd},
		{"gzip", format.Gzip},
		{"lz4", format.Lz4Raw},
		{"none", format.Uncompressed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "codec.parquet")
			if err := parquet.WriteFile(path, []sampleRow{{ID: 1, Name: "a"}}, parquet.WriteOptions{Compression: c.name}); err != nil {
				t.Fatalf("WriteFile 失败: %v", err)
			}
			if got := firstColumnCodec(t, path); got != c.codec {
				t.Errorf("列压缩 = %v, 期望 %v", got, c.codec)
			}
			rows, err := parquet.ReadFile[sampleRow](path, parquet.ReadOptions{})
			if err != nil || len(rows) != 1 {
				t.Fatalf("压缩文件读回失败: rows=%v err=%v", rows, err)
			}
		})
	}

	path := filepath.Join(t.TempDir(), "bad.parquet")
	if err := parquet.WriteFile(path, []sampleRow{{ID: 1}}, parquet.WriteOptions{Compression: "bogus"}); !errors.Is(err, parquet.ErrInvalidOption) {
		t.Fatalf("未知压缩名的 err = %v, 期望 ErrInvalidOption", err)
	}
}

// firstColumnCodec 读取文件第一列第一行组的压缩编码。
func firstColumnCodec(t *testing.T, path string) format.CompressionCodec {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("打开文件失败: %v", err)
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	pf, err := pq.OpenFile(f, info.Size())
	if err != nil {
		t.Fatalf("上游打开失败: %v", err)
	}
	return pf.Metadata().RowGroups[0].Columns[0].MetaData.Codec
}

// TestWriteRowGroupLimit 验证 MaxRowsPerRowGroup 生效。
func TestWriteRowGroupLimit(t *testing.T) {
	path := filepath.Join(t.TempDir(), "groups.parquet")
	rows := make([]sampleRow, 5)
	for i := range rows {
		rows[i] = sampleRow{ID: int64(i), Name: "x"}
	}
	if err := parquet.WriteFile(path, rows, parquet.WriteOptions{MaxRowsPerRowGroup: 2}); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
	f, err := parquet.Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer f.Close()
	if got := f.Meta().RowGroups; got != 3 {
		t.Errorf("行组数 = %d, 期望 3", got)
	}
}

// TestWriteKeyValueMetadata 验证文件级元信息写入且能读回。
func TestWriteKeyValueMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.parquet")
	if err := parquet.WriteFile(path, []sampleRow{{ID: 1}}, parquet.WriteOptions{
		KeyValueMetadata: map[string]string{"source": "test"},
	}); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}
	f, err := parquet.Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	defer f.Close()
	if got := f.Meta().KeyValue["source"]; got != "test" {
		t.Errorf("元信息 source = %q, 期望 test", got)
	}
}

// TestWriteInvalidOptions 验证结构体模式传 Schema、负数参数被拒绝。
func TestWriteInvalidOptions(t *testing.T) {
	var buf writerBuffer
	if _, err := parquet.NewWriter[sampleRow](&buf, parquet.WriteOptions{Schema: pq.SchemaOf(new(sampleRow))}); !errors.Is(err, parquet.ErrInvalidOption) {
		t.Errorf("结构体 + Schema 的 err = %v, 期望 ErrInvalidOption", err)
	}
	if _, err := parquet.NewWriter[sampleRow](&buf, parquet.WriteOptions{MaxRowsPerRowGroup: -1}); !errors.Is(err, parquet.ErrInvalidOption) {
		t.Errorf("负数行组的 err = %v, 期望 ErrInvalidOption", err)
	}
}

// writerBuffer 是最小的 io.Writer，用于只验证构造期的用例。
type writerBuffer struct{ n int }

// Write 实现 io.Writer。
func (w *writerBuffer) Write(p []byte) (int, error) { w.n += len(p); return len(p), nil }

var _ io.Writer = (*writerBuffer)(nil)
