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

// TestUploadReadErrorAborts 校验本地读取失败时不会提交截断对象。
func TestUploadReadErrorAborts(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	// 打开目录成功、读取失败，正好模拟"开始上传后本地读错误"。
	key := prefix + "from-dir.bin"

	err := client.Upload(ctx, bucket, key, t.TempDir(), storage.WriterOptions{})
	t.Logf("Upload(目录) error = %v", err)
	if err == nil {
		t.Fatal("读取目录应返回错误")
	}

	exists, statErr := client.Exists(ctx, bucket, key)
	if statErr != nil {
		t.Fatalf("Exists 失败: %v", statErr)
	}
	t.Logf("读取失败后 Exists = %v", exists)
	if exists {
		t.Fatal("本地读失败不应提交对象")
	}
}

// TestDownloadIsAtomic 校验下载不残留临时文件，失败时保留原文件。
func TestDownloadIsAtomic(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "atomic.txt"
	if err := client.PutBytes(ctx, bucket, key, []byte("fresh"), storage.WriterOptions{}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "out.txt")
	if err := client.Download(ctx, bucket, key, path, storage.ReaderOptions{}); err != nil {
		t.Fatalf("Download 失败: %v", err)
	}
	assertOnlyFile(t, dir, "out.txt")

	// 目标不存在时下载失败：不得留下半个文件或临时文件。
	missing := filepath.Join(dir, "missing.txt")
	err := client.Download(ctx, bucket, prefix+"absent.bin", missing, storage.ReaderOptions{})
	t.Logf("Download(缺失对象) error = %v", err)
	if err == nil {
		t.Fatal("下载缺失对象应返回错误")
	}
	assertOnlyFile(t, dir, "out.txt")

	// 目标已存在时下载失败：原内容必须保留。
	if err := os.WriteFile(path, []byte("keep"), 0o644); err != nil {
		t.Fatalf("写入旧文件失败: %v", err)
	}
	if err := client.Download(ctx, bucket, prefix+"absent.bin", path, storage.ReaderOptions{}); err == nil {
		t.Fatal("下载缺失对象应返回错误")
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取旧文件失败: %v", err)
	}
	t.Logf("失败后目标文件内容 = %q", got)
	if string(got) != "keep" {
		t.Fatalf("失败下载破坏了已有文件: %q", got)
	}
	assertOnlyFile(t, dir, "out.txt")
}

// assertOnlyFile 断言目录里只有指定的文件（不残留临时文件）。
func assertOnlyFile(t *testing.T, dir, name string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("读取目录失败: %v", err)
	}
	var names []string
	for _, entry := range entries {
		names = append(names, entry.Name())
	}
	t.Logf("目录 %s 内容 = %v", dir, names)
	if len(names) != 1 || names[0] != name {
		t.Fatalf("目录内容 = %v, want [%s]", names, name)
	}
}
