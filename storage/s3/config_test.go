package s3_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/corenca/teatool-go/storage"
	"github.com/corenca/teatool-go/storage/s3"
)

// TestNewRejectsMissingCredentials 校验凭据缺失时返回 ErrInvalidArgument。
func TestNewRejectsMissingCredentials(t *testing.T) {
	cases := []struct {
		name   string
		config s3.Config
	}{
		{"空配置", s3.Config{}},
		{"缺 secret", s3.Config{AccessKeyID: "ak"}},
		{"缺 access", s3.Config{SecretAccessKey: "sk"}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := s3.New(tc.config)
			t.Logf("New(%s) error = %v", tc.name, err)
			if !errors.Is(err, storage.ErrInvalidArgument) {
				t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
			}
		})
	}
}

// TestConfigMapstructureTags 固定配置结构体的解码契约：每个字段都必须有
// mapstructure tag，且不再保留没有解码路径的 toml tag。
func TestConfigMapstructureTags(t *testing.T) {
	typ := reflect.TypeFor[s3.Config]()
	for field := range typ.Fields() {
		tag, ok := field.Tag.Lookup("mapstructure")
		if !ok || tag == "" {
			t.Errorf("字段 %s 缺少 mapstructure tag", field.Name)
			continue
		}
		if _, hasToml := field.Tag.Lookup("toml"); hasToml {
			t.Errorf("字段 %s 仍带 toml tag（仓库没有 TOML 解码路径）", field.Name)
		}
		t.Logf("%s -> mapstructure:%q", field.Name, tag)
	}
}

// TestClientBucket 校验客户端原样保留配置里的桶名，且不因桶为空而拒绝构造。
func TestClientBucket(t *testing.T) {
	cases := []struct {
		name   string
		bucket string
	}{
		{"有桶", "demo"},
		{"空桶", ""},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			client, err := s3.New(s3.Config{
				AccessKeyID:     "ak",
				SecretAccessKey: "sk",
				Bucket:          tc.bucket,
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			t.Logf("配置 bucket=%q，Client.Bucket() = %q", tc.bucket, client.Bucket())
			if got := client.Bucket(); got != tc.bucket {
				t.Fatalf("Bucket() = %q，期望 %q", got, tc.bucket)
			}
		})
	}
}
