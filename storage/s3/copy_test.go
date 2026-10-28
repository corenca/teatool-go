package s3_test

import (
	"errors"
	"testing"

	"github.com/corenca/teatool-go/storage"
)

// TestCopyEscapedKey 校验含空格等特殊字符的源 key 能正确复制。
func TestCopyEscapedKey(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	src := prefix + "a b/c.txt"
	dst := prefix + "copied.txt"
	payload := []byte("copy payload")

	if err := client.PutBytes(ctx, bucket, src, payload, storage.WriterOptions{}); err != nil {
		t.Fatalf("写入源对象失败: %v", err)
	}
	if err := client.Copy(ctx, bucket, src, bucket, dst); err != nil {
		t.Fatalf("Copy 失败: %v", err)
	}
	got, err := client.GetBytes(ctx, bucket, dst, storage.ReaderOptions{})
	if err != nil {
		t.Fatalf("读取目标对象失败: %v", err)
	}
	t.Logf("复制后内容 = %q", got)
	if string(got) != string(payload) {
		t.Fatalf("内容 = %q, want %q", got, payload)
	}
}

// TestMove 校验移动后源对象消失、目标对象存在。
func TestMove(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	src := prefix + "old.txt"
	dst := prefix + "new.txt"
	payload := []byte("move payload")

	if err := client.PutBytes(ctx, bucket, src, payload, storage.WriterOptions{}); err != nil {
		t.Fatalf("写入源对象失败: %v", err)
	}
	if err := client.Move(ctx, bucket, src, bucket, dst); err != nil {
		t.Fatalf("Move 失败: %v", err)
	}

	srcExists, err := client.Exists(ctx, bucket, src)
	if err != nil {
		t.Fatalf("Exists(源) 失败: %v", err)
	}
	dstExists, err := client.Exists(ctx, bucket, dst)
	if err != nil {
		t.Fatalf("Exists(目标) 失败: %v", err)
	}
	t.Logf("移动后 源存在=%v 目标存在=%v", srcExists, dstExists)
	if srcExists {
		t.Fatal("源对象仍然存在")
	}
	if !dstExists {
		t.Fatal("目标对象不存在")
	}

	got, err := client.GetBytes(ctx, bucket, dst, storage.ReaderOptions{})
	if err != nil {
		t.Fatalf("读取目标对象失败: %v", err)
	}
	if string(got) != string(payload) {
		t.Fatalf("内容 = %q, want %q", got, payload)
	}
}

// TestCopyPreservesContentType 校验小对象复制走 CopyObject 时保留内容头。
func TestCopyPreservesContentType(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	src := prefix + "typed.txt"
	dst := prefix + "typed-copy.txt"

	err := client.PutBytes(ctx, bucket, src, []byte("x"), storage.WriterOptions{
		ContentType:  "text/plain; charset=utf-8",
		CacheControl: "max-age=120",
	})
	if err != nil {
		t.Fatalf("写入源对象失败: %v", err)
	}
	if err := client.Copy(ctx, bucket, src, bucket, dst); err != nil {
		t.Fatalf("Copy 失败: %v", err)
	}
	info, err := client.Stat(ctx, bucket, dst)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	t.Logf("复制后 = %s", info)
	if info.ContentType != "text/plain; charset=utf-8" {
		t.Fatalf("ContentType = %q", info.ContentType)
	}
	if info.CacheControl != "max-age=120" {
		t.Fatalf("CacheControl = %q", info.CacheControl)
	}
}

// TestCopyMissingSource 校验源对象不存在时返回 ErrObjectNotFound。
func TestCopyMissingSource(t *testing.T) {
	client, bucket, prefix := newTestContext(t)

	err := client.Copy(t.Context(), bucket, prefix+"absent.bin", bucket, prefix+"dst.bin")
	t.Logf("Copy(缺失源) error = %v", err)
	if !errors.Is(err, storage.ErrObjectNotFound) {
		t.Fatalf("期望 ErrObjectNotFound，实际 %v", err)
	}
}
