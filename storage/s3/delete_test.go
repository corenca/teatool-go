package s3_test

import (
	"errors"
	"testing"

	"github.com/corenca/teatool-go/storage"
)

// TestDeleteIsIdempotent 校验删除不存在的对象不报错。
func TestDeleteIsIdempotent(t *testing.T) {
	client, bucket, prefix := newTestContext(t)

	err := client.Delete(t.Context(), bucket, prefix+"missing.txt")
	t.Logf("Delete(missing) error = %v", err)
	if err != nil {
		t.Fatalf("期望 nil，实际 %v", err)
	}
}

// TestDeleteMany 校验批量删除后对象不可见。
func TestDeleteMany(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	keys := []string{prefix + "a.txt", prefix + "b.txt"}
	for _, key := range keys {
		if err := client.PutBytes(ctx, bucket, key, []byte("x"), storage.WriterOptions{}); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	if err := client.DeleteMany(ctx, bucket, keys); err != nil {
		t.Fatalf("DeleteMany 失败: %v", err)
	}
	for _, key := range keys {
		exists, err := client.Exists(ctx, bucket, key)
		if err != nil {
			t.Fatalf("Exists 失败: %v", err)
		}
		t.Logf("删除后 Exists(%s) = %v", key, exists)
		if exists {
			t.Fatalf("对象 %s 仍存在", key)
		}
	}
}

// TestDeletePrefixRejectsEmpty 校验空前缀被拒绝且不发请求。
func TestDeletePrefixRejectsEmpty(t *testing.T) {
	client := newOfflineClient(t)

	err := client.DeletePrefix(t.Context(), "bucket", "")
	t.Logf("DeletePrefix(\"\") error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}

// TestDeletePrefix 校验按前缀删除后整批不可见。
func TestDeletePrefix(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	deletePrefix := prefix + "purge/"
	keys := []string{deletePrefix + "a.txt", deletePrefix + "sub/b.txt"}
	for _, key := range keys {
		if err := client.PutBytes(ctx, bucket, key, []byte("x"), storage.WriterOptions{}); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	if err := client.DeletePrefix(ctx, bucket, deletePrefix); err != nil {
		t.Fatalf("DeletePrefix 失败: %v", err)
	}
	remaining, err := client.ListAll(ctx, bucket, deletePrefix)
	if err != nil {
		t.Fatalf("ListAll 失败: %v", err)
	}
	t.Logf("删除后剩余对象数 = %d", len(remaining))
	if len(remaining) != 0 {
		t.Fatalf("仍有 %d 个对象未删除", len(remaining))
	}
}
