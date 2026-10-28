package s3_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/corenca/teatool-go/storage"
)

// TestPutSmall 校验小对象单次写入后内容、类型与 metadata 都可回读。
func TestPutSmall(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "small.txt"

	writer, err := client.Put(ctx, bucket, key, storage.WriterOptions{
		ContentType: "text/plain",
		Metadata:    map[string]string{"Foo": "Bar"},
	})
	if err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	if _, err := writer.Write([]byte("hello")); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	got, err := client.GetBytes(ctx, bucket, key, storage.ReaderOptions{})
	if err != nil {
		t.Fatalf("GetBytes 失败: %v", err)
	}
	info, err := client.Stat(ctx, bucket, key)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	t.Logf("内容 = %q metadata = %v", got, info.Metadata)

	if string(got) != "hello" {
		t.Fatalf("内容 = %q, want hello", got)
	}
	if info.Metadata["foo"] != "Bar" {
		t.Fatalf("metadata foo = %q, want Bar", info.Metadata["foo"])
	}
}

// TestPutMultipart 校验超过 PartSize 的数据完整落盘。
func TestPutMultipart(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "large.bin"
	payload := bytes.Repeat([]byte("m"), 6<<20)

	writer, err := client.Put(ctx, bucket, key, storage.WriterOptions{PartSize: 5 << 20})
	if err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	if _, err := writer.Write(payload); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	info, err := client.Stat(ctx, bucket, key)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}
	got, err := client.GetBytes(ctx, bucket, key, storage.ReaderOptions{})
	if err != nil {
		t.Fatalf("GetBytes 失败: %v", err)
	}
	t.Logf("分片对象大小 = %d ETag = %s", info.Size, info.ETag)

	if info.Size != int64(len(payload)) {
		t.Fatalf("Size = %d, want %d", info.Size, len(payload))
	}
	if !bytes.Equal(got, payload) {
		t.Fatal("分片上传内容与原数据不一致")
	}
}

// TestWriterRejectsWriteAfterClose 校验 Close 之后写入返回 ErrInvalidArgument。
func TestWriterRejectsWriteAfterClose(t *testing.T) {
	client, bucket, prefix := newTestContext(t)

	writer, err := client.Put(t.Context(), bucket, prefix+"closed.txt", storage.WriterOptions{})
	if err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	_, err = writer.Write([]byte("late"))
	t.Logf("Close 后 Write error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}

// TestWriterCanceledLeavesNoObject 校验写一半取消后对象不可见。
func TestWriterCanceledLeavesNoObject(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	key := prefix + "canceled.bin"

	ctx, cancel := context.WithCancel(t.Context())
	writer, err := client.Put(ctx, bucket, key, storage.WriterOptions{PartSize: 5 << 20})
	if err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	// 写超一片，触发分片上传后再取消。
	if _, err := writer.Write(bytes.Repeat([]byte("c"), 5<<20+1)); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	cancel()

	err = writer.Close()
	t.Logf("取消后 Close error = %v", err)
	if err == nil {
		t.Fatal("取消后 Close 应返回错误")
	}

	exists, statErr := client.Exists(t.Context(), bucket, key)
	t.Logf("取消后 Exists = %v, %v", exists, statErr)
	if statErr != nil {
		t.Fatalf("Exists 失败: %v", statErr)
	}
	if exists {
		t.Fatal("未完成的分片上传不应产生可见对象")
	}
}

// TestPutRejectsInvalidPartSize 校验分片大小在发请求前就被拒绝。
func TestPutRejectsInvalidPartSize(t *testing.T) {
	client := newOfflineClient(t)

	cases := map[string]int{
		"小于5MiB": storage.MinPartSize - 1,
		"超过5GiB": storage.MaxPartSize + 1,
	}
	for name, partSize := range cases {
		_, err := client.Put(t.Context(), "bucket", "k", storage.WriterOptions{PartSize: partSize})
		t.Logf("PartSize=%s(%d) error = %v", name, partSize, err)
		if !errors.Is(err, storage.ErrInvalidArgument) {
			t.Fatalf("%s: 期望 ErrInvalidArgument，实际 %v", name, err)
		}
	}
}

// TestPutRejectsNegativeConcurrency 校验并发数不能为负。
func TestPutRejectsNegativeConcurrency(t *testing.T) {
	client := newOfflineClient(t)

	_, err := client.Put(t.Context(), "bucket", "k", storage.WriterOptions{Concurrency: -1})
	t.Logf("Concurrency=-1 error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}

// TestWriterAbortLeavesNoObject 校验 Abort 放弃已开始的分片上传。
func TestWriterAbortLeavesNoObject(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "aborted.bin"

	writer, err := client.Put(ctx, bucket, key, storage.WriterOptions{PartSize: 5 << 20})
	if err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	// 写超一片，确保分片上传已经发起。
	if _, err := writer.Write(bytes.Repeat([]byte("a"), 5<<20+1)); err != nil {
		t.Fatalf("Write 失败: %v", err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatalf("Abort 失败: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Abort 后 Close 应返回 nil，实际 %v", err)
	}

	exists, err := client.Exists(t.Context(), bucket, key)
	if err != nil {
		t.Fatalf("Exists 失败: %v", err)
	}
	t.Logf("Abort 后 Exists = %v", exists)
	if exists {
		t.Fatal("Abort 后不应产生可见对象")
	}
}

// TestWriterAbortIsIdempotent 校验重复 Abort 不报错且之后 Write 被拒绝。
func TestWriterAbortIsIdempotent(t *testing.T) {
	client, bucket, prefix := newTestContext(t)

	writer, err := client.Put(t.Context(), bucket, prefix+"idempotent.bin", storage.WriterOptions{})
	if err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatalf("首次 Abort 失败: %v", err)
	}
	if err := writer.Abort(); err != nil {
		t.Fatalf("重复 Abort 失败: %v", err)
	}
	_, err = writer.Write([]byte("late"))
	t.Logf("Abort 后 Write error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}

// TestPutMultipartConcurrent 校验并发分片上传按分片序提交且内容完整。
func TestPutMultipartConcurrent(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "concurrent.bin"
	// 3 个整片 + 零头，覆盖"并发派发 + 尾部小分片"。
	payload := bytes.Repeat([]byte("0123456789abcdef"), (16<<20+1234)/16)

	writer, err := client.Put(ctx, bucket, key, storage.WriterOptions{
		PartSize:    5 << 20,
		Concurrency: 4,
	})
	if err != nil {
		t.Fatalf("Put 失败: %v", err)
	}
	// 分多次写入，制造缓冲区拼接与派发交错。
	for chunk := 0; chunk < len(payload); chunk += 1 << 20 {
		end := min(chunk+1<<20, len(payload))
		if _, err := writer.Write(payload[chunk:end]); err != nil {
			t.Fatalf("Write 失败: %v", err)
		}
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	got, err := client.GetBytes(ctx, bucket, key, storage.ReaderOptions{})
	if err != nil {
		t.Fatalf("GetBytes 失败: %v", err)
	}
	t.Logf("并发分片上传：写入 %d 字节，读回 %d 字节", len(payload), len(got))
	if !bytes.Equal(got, payload) {
		t.Fatal("并发分片上传内容与原数据不一致")
	}
}
