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

// TestConfigTags 固定配置结构体的解码契约：每个字段都带 json/yaml/toml/
// mapstructure 四种 tag，键名一致；HTTPClient、Credentials 这类只能代码注入的
// 字段统一标 "-"。
func TestConfigTags(t *testing.T) {
	typ := reflect.TypeFor[s3.Config]()
	for field := range typ.Fields() {
		want, ok := field.Tag.Lookup("mapstructure")
		if !ok || want == "" {
			t.Errorf("字段 %s 缺少 mapstructure tag", field.Name)
			continue
		}
		for _, name := range []string{"json", "yaml", "toml"} {
			got, ok := field.Tag.Lookup(name)
			if !ok {
				t.Errorf("字段 %s 缺少 %s tag", field.Name, name)
				continue
			}
			if got != want {
				t.Errorf("字段 %s 的 %s tag = %q，期望与 mapstructure 一致 %q", field.Name, name, got, want)
			}
		}
		jsonTag, _ := field.Tag.Lookup("json")
		yamlTag, _ := field.Tag.Lookup("yaml")
		tomlTag, _ := field.Tag.Lookup("toml")
		t.Logf("%s -> json:%q yaml:%q toml:%q mapstructure:%q",
			field.Name, jsonTag, yamlTag, tomlTag, want)
	}
}
