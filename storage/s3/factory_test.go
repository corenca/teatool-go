package s3_test

import (
	"encoding/json"
	"testing"

	"github.com/corenca/teatool-go/storage"
	"github.com/corenca/teatool-go/storage/s3"
)

// TestRegisterBackend 校验 s3.New 能直接注册进 storage 工厂，且构造出的客户端
// 与 Service 使用配置里的桶名。全程离线，不发出请求。
func TestRegisterBackend(t *testing.T) {
	decode := func(raw map[string]any, target any) error {
		encoded, err := json.Marshal(raw)
		if err != nil {
			return err
		}
		return json.Unmarshal(encoded, target)
	}
	factory, err := storage.NewFactory(decode)
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	if err := storage.RegisterBackend(factory, "s3", s3.New); err != nil {
		t.Fatalf("注册 s3 后端失败: %v", err)
	}

	raw := map[string]any{
		"endpoint":        "http://127.0.0.1:1",
		"accessKeyId":     "offline-ak",
		"secretAccessKey": "offline-sk",
		"usePathStyle":    true,
		"bucket":          "demo",
	}
	t.Logf("注册 s3 后端，配置=%v", raw)

	client, err := factory.NewClient("s3", raw)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	bucketer, ok := client.(storage.Bucketer)
	if !ok {
		t.Fatalf("客户端类型 %T 未实现 storage.Bucketer", client)
	}
	t.Logf("NewClient() -> %T，Bucket() = %q", client, bucketer.Bucket())
	if got := bucketer.Bucket(); got != "demo" {
		t.Fatalf("Bucket() = %q，期望 demo", got)
	}

	svc, err := factory.NewService("s3", raw)
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Logf("NewService() -> Bucket() = %q", svc.Bucket())
	if got := svc.Bucket(); got != "demo" {
		t.Fatalf("Service.Bucket() = %q，期望 demo", got)
	}
}

// 编译期断言：S3 客户端向工厂提供配置里的桶名。
var _ storage.Bucketer = (*s3.Client)(nil)
