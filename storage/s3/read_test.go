package s3_test

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/storage"
)

// TestStat 校验 head 语义：大小、ETag、类型与 metadata 小写。
func TestStat(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "stat.txt"

	if err := client.PutBytes(ctx, bucket, key, []byte("hello storage"), storage.WriterOptions{
		ContentType: "text/plain",
		Metadata:    map[string]string{"Probe": "v1"},
	}); err != nil {
		t.Fatalf("写入探针对象失败: %v", err)
	}

	info, err := client.Stat(ctx, bucket, key)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	t.Logf("Stat = %+v", info)

	if info.Key != key || info.Size != int64(len("hello storage")) {
		t.Fatalf("Key/Size 不符: %+v", info)
	}
	if info.ETag == "" {
		t.Fatal("ETag 为空")
	}
	if !strings.HasPrefix(info.ContentType, "text/plain") {
		t.Fatalf("ContentType = %q", info.ContentType)
	}
	if got := info.Metadata["probe"]; got != "v1" {
		t.Fatalf("metadata probe = %q, want v1", got)
	}
}

// TestStatNotFound 校验缺失对象映射为 ErrObjectNotFound。
func TestStatNotFound(t *testing.T) {
	client, bucket, prefix := newTestContext(t)

	_, err := client.Stat(t.Context(), bucket, prefix+"missing.txt")
	t.Logf("Stat(missing) error = %v", err)
	if !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("期望 ErrObjectNotFound，实际 %v", err)
	}
}

// TestExists 校验存在与不存在两种结果。
func TestExists(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "exists.txt"

	if err := client.PutBytes(ctx, bucket, key, []byte("x"), storage.WriterOptions{}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	yes, err := client.Exists(ctx, bucket, key)
	t.Logf("Exists(存在) = %v, %v", yes, err)
	if !yes || err != nil {
		t.Fatalf("期望 true,nil，实际 %v,%v", yes, err)
	}

	no, err := client.Exists(ctx, bucket, prefix+"absent.txt")
	t.Logf("Exists(不存在) = %v, %v", no, err)
	if no || err != nil {
		t.Fatalf("期望 false,nil，实际 %v,%v", no, err)
	}
}

// TestGetRange 校验前闭后开区间与对象总大小。
func TestGetRange(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "range.txt"
	payload := "0123456789abcdefghij"

	if err := client.PutBytes(ctx, bucket, key, []byte(payload), storage.WriterOptions{}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	reader, err := client.Get(ctx, bucket, key, storage.ReaderOptions{
		Range: &storage.ByteRange{Start: 10, End: 20},
	})
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	defer reader.Close()

	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	t.Logf("区间读取 = %q 对象总大小 = %d", got, reader.Info.Size)

	if string(got) != "abcdefghij" {
		t.Fatalf("区间内容 = %q, want abcdefghij", got)
	}
	if reader.Info.Size != int64(len(payload)) {
		t.Fatalf("Info.Size = %d, want %d", reader.Info.Size, len(payload))
	}
}

// TestGetBytes 校验整对象读取。
func TestGetBytes(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "whole.txt"

	if err := client.PutBytes(ctx, bucket, key, []byte("hello"), storage.WriterOptions{}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	got, err := client.GetBytes(ctx, bucket, key, storage.ReaderOptions{})
	if err != nil {
		t.Fatalf("GetBytes 失败: %v", err)
	}
	t.Logf("GetBytes = %q", got)
	if string(got) != "hello" {
		t.Fatalf("GetBytes = %q, want hello", got)
	}
}

// TestGetRejectsInvalidRange 校验非法区间在发请求前就被拒绝。
func TestGetRejectsInvalidRange(t *testing.T) {
	client := newOfflineClient(t)

	_, err := client.Get(t.Context(), "bucket", "a.txt", storage.ReaderOptions{
		Range: &storage.ByteRange{Start: 10, End: 10},
	})
	t.Logf("Get(非法区间) error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}

// TestStatContentHeaders 校验写入的内容头能通过 Stat/Get 完整回读。
func TestStatContentHeaders(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "headers.txt"

	err := client.PutBytes(ctx, bucket, key, []byte("headers"), storage.WriterOptions{
		ContentType:        "text/plain; charset=utf-8",
		ContentDisposition: `attachment; filename="headers.txt"`,
		CacheControl:       "max-age=60",
	})
	if err != nil {
		t.Fatalf("PutBytes 失败: %v", err)
	}

	info, err := client.Stat(ctx, bucket, key)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	t.Logf("Stat = %s", info)
	if info.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("ContentType = %q", info.ContentType)
	}
	if info.ContentDisposition != `attachment; filename="headers.txt"` {
		t.Fatalf("ContentDisposition = %q", info.ContentDisposition)
	}
	if info.CacheControl != "max-age=60" {
		t.Fatalf("CacheControl = %q", info.CacheControl)
	}

	reader, err := client.Get(ctx, bucket, key, storage.ReaderOptions{})
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	defer reader.Close()
	t.Logf("Get.Info = %s", reader.Info)
	if reader.Info.CacheControl != "max-age=60" {
		t.Fatalf("Reader.Info.CacheControl = %q", reader.Info.CacheControl)
	}
}

// TestGetSuffixRange 校验 Start 为负数时读取对象末尾 N 字节。
func TestGetSuffixRange(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "suffix.txt"
	payload := "0123456789abcdefghij"

	if err := client.PutBytes(ctx, bucket, key, []byte(payload), storage.WriterOptions{}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	reader, err := client.Get(ctx, bucket, key, storage.ReaderOptions{
		Range: &storage.ByteRange{Start: -4},
	})
	if err != nil {
		t.Fatalf("Get 失败: %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	t.Logf("末尾 4 字节 = %q 总大小 = %d", got, reader.Info.Size)
	if string(got) != "ghij" {
		t.Fatalf("尾部内容 = %q, want ghij", got)
	}
	if reader.Info.Size != int64(len(payload)) {
		t.Fatalf("Info.Size = %d, want %d", reader.Info.Size, len(payload))
	}
}

// TestGetOutOfBoundsRange 校验起点越界时返回空流而不是错误。
func TestGetOutOfBoundsRange(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "oob.txt"
	if err := client.PutBytes(ctx, bucket, key, []byte("short"), storage.WriterOptions{}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}

	reader, err := client.Get(ctx, bucket, key, storage.ReaderOptions{
		Range: &storage.ByteRange{Start: 100, End: 200},
	})
	if err != nil {
		t.Fatalf("Get 越界应返回空流，实际 %v", err)
	}
	defer reader.Close()
	got, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	t.Logf("越界读取 = %d 字节，总大小 = %d", len(got), reader.Info.Size)
	if len(got) != 0 {
		t.Fatalf("越界读取返回 %d 字节，want 0", len(got))
	}
}
