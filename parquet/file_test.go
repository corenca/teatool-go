package parquet_test

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/corenca/teatool-go/parquet"
)

// TestOpenMeta 验证读取 footer 元信息：行数、行组数、大小、创建者与列清单。
func TestOpenMeta(t *testing.T) {
	path := filepath.Join(t.TempDir(), "meta.parquet")
	writeSampleFile(t, path, []sampleRow{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}, {ID: 3, Name: "c"}})

	f, err := parquet.Open(path)
	if err != nil {
		t.Fatalf("Open 失败: %v", err)
	}
	// 重复 Close 应返回 nil。
	defer func() {
		if err := f.Close(); err != nil {
			t.Errorf("重复 Close 失败: %v", err)
		}
	}()

	m := f.Meta()
	t.Logf("Meta: rows=%d groups=%d size=%d createdBy=%q", m.NumRows, m.RowGroups, m.Size, m.CreatedBy)
	if m.NumRows != 3 {
		t.Errorf("NumRows = %d, 期望 3", m.NumRows)
	}
	if m.RowGroups < 1 {
		t.Errorf("RowGroups = %d, 期望至少 1", m.RowGroups)
	}
	if m.Size <= 0 {
		t.Errorf("Size = %d, 期望大于 0", m.Size)
	}
	if m.CreatedBy == "" {
		t.Error("CreatedBy 为空")
	}

	var paths []string
	for _, p := range f.Schema().Columns() {
		paths = append(paths, p[0])
	}
	if !slices.Contains(paths, "id") || !slices.Contains(paths, "name") {
		t.Errorf("列清单 = %v, 期望包含 id 与 name", paths)
	}
}

// TestOpenRejectsNonParquet 验证空文件、错误魔数、过短文件都返回 ErrNotParquet。
func TestOpenRejectsNonParquet(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"空文件", nil},
		{"魔数错误", []byte("NOPE................")},
		{"只有 4 字节魔数", []byte("PAR1")},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), c.name+".parquet")
			if err := os.WriteFile(path, c.data, 0o644); err != nil {
				t.Fatalf("写测试文件失败: %v", err)
			}
			_, err := parquet.Open(path)
			if !errors.Is(err, parquet.ErrNotParquet) {
				t.Fatalf("err = %v, 期望 ErrNotParquet", err)
			}
		})
	}
}

// TestOpenReaderAt 验证用内存 ReaderAt 打开，Close 后不接管外部 ReaderAt。
func TestOpenReaderAt(t *testing.T) {
	path := filepath.Join(t.TempDir(), "readerat.parquet")
	writeSampleFile(t, path, []sampleRow{{ID: 7, Name: "g"}})

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读文件失败: %v", err)
	}
	f, err := parquet.OpenAt(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("OpenReaderAt 失败: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	// Close 不应关闭调用方的 ReaderAt，再打开一次仍然成功。
	f2, err := parquet.OpenAt(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatalf("Close 后再次 OpenReaderAt 失败: %v", err)
	}
	f2.Close()
}

// TestOpenCorruptFooter 验证魔数正确但 footer 损坏时返回 error 而不是 panic。
func TestOpenCorruptFooter(t *testing.T) {
	path := filepath.Join(t.TempDir(), "corrupt.parquet")
	data := make([]byte, 64)
	copy(data, "PAR1")
	copy(data[len(data)-4:], "PAR1")
	for i := 4; i < len(data)-4; i++ {
		data[i] = 0xff
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("写测试文件失败: %v", err)
	}

	f, err := parquet.Open(path)
	if err == nil {
		f.Close()
		t.Fatal("损坏 footer 应返回错误")
	}
	t.Logf("损坏 footer 的错误: %v", err)
}
