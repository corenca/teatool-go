package s3

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/corenca/teatool-go/storage"
)

// TestMultipartCopyWithSmallParts 用缩小的分片验证大对象复制的分片路径：
// 16MiB 源对象按 5MiB 分片复制，内容与内容头都要一致。
func TestMultipartCopyWithSmallParts(t *testing.T) {
	raw, err := os.ReadFile("s3_test.json")
	if err != nil {
		t.Skipf("缺少 s3_test.json，跳过真实服务测试")
	}
	var config Config
	if err := json.Unmarshal(raw, &config); err != nil {
		t.Fatalf("解析 s3_test.json 失败: %v", err)
	}
	client, err := New(config)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	ctx := t.Context()
	if _, err := client.Exists(ctx, config.Bucket, ".teatool-connectivity-probe"); err != nil {
		t.Skipf("无法连接 %s: %v", config.Endpoint, err)
	}

	prefix := fmt.Sprintf("teatool-test/multipart-copy/%d/", time.Now().UnixNano())
	t.Cleanup(func() {
		_ = client.DeletePrefix(context.Background(), config.Bucket, prefix)
	})
	src := prefix + "src.bin"
	dst := prefix + "dst.bin"
	payload := bytes.Repeat([]byte("0123456789abcdef"), 1<<20)

	if err := client.PutBytes(ctx, config.Bucket, src, payload, storage.WriterOptions{
		ContentType:  "application/octet-stream",
		CacheControl: "max-age=30",
	}); err != nil {
		t.Fatalf("写入源对象失败: %v", err)
	}
	info, err := client.Stat(ctx, config.Bucket, src)
	if err != nil {
		t.Fatalf("Stat 失败: %v", err)
	}

	if err := client.multipartCopy(ctx, info, config.Bucket, src, config.Bucket, dst, 5<<20); err != nil {
		t.Fatalf("multipartCopy 失败: %v", err)
	}
	got, err := client.GetBytes(ctx, config.Bucket, dst, storage.ReaderOptions{})
	if err != nil {
		t.Fatalf("读取目标对象失败: %v", err)
	}
	dstInfo, err := client.Stat(ctx, config.Bucket, dst)
	if err != nil {
		t.Fatalf("Stat(目标) 失败: %v", err)
	}
	t.Logf("源大小=%d 目标=%s", info.Size, dstInfo)

	if !bytes.Equal(got, payload) {
		t.Fatalf("复制后内容不一致：目标 %d 字节", len(got))
	}
	if dstInfo.ContentType != "application/octet-stream" {
		t.Fatalf("ContentType = %q", dstInfo.ContentType)
	}
	if dstInfo.CacheControl != "max-age=30" {
		t.Fatalf("CacheControl = %q", dstInfo.CacheControl)
	}
}
