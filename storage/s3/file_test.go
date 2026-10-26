package s3_test

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/corenca/teatool-go/storage"
)

// TestUploadSmallFile 校验本地文件内容原样上传。
func TestUploadSmallFile(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	content := bytes.Repeat([]byte("upload-"), 512)
	local := filepath.Join(t.TempDir(), "source.bin")
	if err := os.WriteFile(local, content, 0o644); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}

	key := prefix + "uploaded.bin"
	if err := client.Upload(ctx, bucket, key, local, storage.WriterOptions{}); err != nil {
		t.Fatalf("Upload 失败: %v", err)
	}
	got, err := client.GetBytes(ctx, bucket, key, storage.ReaderOptions{})
	if err != nil {
		t.Fatalf("GetBytes 失败: %v", err)
	}
	t.Logf("上传字节数 = %d", len(got))
	if !bytes.Equal(got, content) {
		t.Fatalf("上传内容不一致：got %d 字节", len(got))
	}
}

// TestUploadLargeFile 校验大文件上传完整落盘。
func TestUploadLargeFile(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	content := bytes.Repeat([]byte("y"), 6<<20)
	local := filepath.Join(t.TempDir(), "large.bin")
	if err := os.WriteFile(local, content, 0o644); err != nil {
		t.Fatalf("写临时文件失败: %v", err)
	}

	key := prefix + "large-file.bin"
	if err := client.Upload(ctx, bucket, key, local, storage.WriterOptions{PartSize: 5 << 20}); err != nil {
		t.Fatalf("Upload 失败: %v", err)
	}
	info, err := client.Stat(ctx, bucket, key)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	t.Logf("大文件大小 = %d", info.Size)
	if info.Size != int64(len(content)) {
		t.Fatalf("Size = %d, want %d", info.Size, len(content))
	}
}

// TestDownloadCreatesParentAndOverwrites 校验父目录自动创建且覆盖旧内容。
func TestDownloadCreatesParentAndOverwrites(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "download.txt"

	if err := client.PutBytes(ctx, bucket, key, []byte("downloaded body"), storage.WriterOptions{}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	path := filepath.Join(t.TempDir(), "a", "b", "c.txt")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte("stale"), 0o644); err != nil {
		t.Fatalf("写旧文件失败: %v", err)
	}

	if err := client.Download(ctx, bucket, key, path, storage.ReaderOptions{}); err != nil {
		t.Fatalf("Download 失败: %v", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读下载文件失败: %v", err)
	}
	t.Logf("下载内容 = %q", got)
	if string(got) != "downloaded body" {
		t.Fatalf("内容 = %q", got)
	}
}

// TestDownloadCreatesMissingParent 校验父目录不存在时自动创建。
func TestDownloadCreatesMissingParent(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "nested.txt"

	if err := client.PutBytes(ctx, bucket, key, []byte("ok"), storage.WriterOptions{}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	path := filepath.Join(t.TempDir(), "x", "y", "z.txt")
	if err := client.Download(ctx, bucket, key, path, storage.ReaderOptions{}); err != nil {
		t.Fatalf("Download 失败: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("下载文件不存在: %v", err)
	}
	t.Logf("下载成功: %s", path)
}
