package s3

import (
	"context"
	"errors"
	"testing"

	"github.com/aws/aws-sdk-go-v2/credentials"

	"github.com/corenca/teatool-go/storage"
)

// TestNewUsesSessionToken 校验 STS 临时凭据的会话令牌被传进静态提供者。
func TestNewUsesSessionToken(t *testing.T) {
	client, err := New(Config{
		Endpoint:        "http://127.0.0.1:1",
		AccessKeyID:     "ak",
		SecretAccessKey: "sk",
		SessionToken:    "token-123",
	})
	if err != nil {
		t.Fatalf("New 失败: %v", err)
	}
	creds, err := client.sdk.Options().Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve 失败: %v", err)
	}
	t.Logf("凭据 = ak:%s token:%s", creds.AccessKeyID, creds.SessionToken)
	if creds.SessionToken != "token-123" {
		t.Fatalf("SessionToken = %q, want token-123", creds.SessionToken)
	}
}

// TestNewAcceptsCredentialsProvider 校验注入的凭据链优先于静态密钥。
func TestNewAcceptsCredentialsProvider(t *testing.T) {
	provided := credentials.NewStaticCredentialsProvider("provider-ak", "provider-sk", "provider-token")
	client, err := New(Config{
		Endpoint:    "http://127.0.0.1:1",
		Credentials: provided,
	})
	if err != nil {
		t.Fatalf("仅注入 Credentials 时 New 失败: %v", err)
	}
	creds, err := client.sdk.Options().Credentials.Retrieve(context.Background())
	if err != nil {
		t.Fatalf("Retrieve 失败: %v", err)
	}
	t.Logf("凭据来源 = %s", creds.Source)
	if creds.AccessKeyID != "provider-ak" {
		t.Fatalf("AccessKeyID = %q, want provider-ak", creds.AccessKeyID)
	}
}

// TestNewRejectsMissingCredentialsStill 校验没有注入凭据链时仍要求静态密钥。
func TestNewRejectsMissingCredentialsStill(t *testing.T) {
	_, err := New(Config{Endpoint: "http://127.0.0.1:1"})
	t.Logf("无凭据 New error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}
