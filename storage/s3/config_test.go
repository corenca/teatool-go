package s3_test

import (
	"errors"
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
