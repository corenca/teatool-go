package gm_test

import (
	"bytes"
	"encoding/base64"
	"errors"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/gm"
)

func TestKID(t *testing.T) {
	key, _ := gm.GenerateSM4Key(16)
	for _, kid := range []string{"", "2026-01", "prod-v1"} {
		sealed, err := gm.EncryptSM4GCM(key, []byte("hello"), nil, gm.EncryptOptions{KID: kid})
		if err != nil {
			t.Fatalf("EncryptSM4GCM(kid=%q): %v", kid, err)
		}
		got, err := gm.KID(sealed)
		if err != nil {
			t.Fatalf("KID(%q): %v", sealed, err)
		}
		t.Logf("kid %q -> 密文 %s...", kid, sealed[:min(len(sealed), 24)])
		if got != kid {
			t.Fatalf("KID = %q，期望 %q", got, kid)
		}
	}
}

func TestKIDRejectsInvalid(t *testing.T) {
	cases := []string{
		"v1.sm4gcm.kid",              // 缺 payload
		"v2.sm4gcm.kid.payload",      // 版本不支持
		"v1.sm4cbc.bad*char.payload", // kid 含非法字符
		"v1.sm4gcm.kid.%%%",          // base64 非法
		"",
	}
	for _, sealed := range cases {
		if _, err := gm.KID(sealed); !errors.Is(err, gm.ErrInvalidFormat) && !errors.Is(err, gm.ErrUnsupported) {
			t.Fatalf("KID(%q) 错误 = %v，期望格式/版本错误", sealed, err)
		}
	}
}

// 辅助：取出密文信封中的 payload 字节。
func payloadOf(t *testing.T, sealed string) []byte {
	t.Helper()
	parts := strings.Split(sealed, ".")
	if len(parts) != 4 {
		t.Fatalf("密文段数 = %d，期望 4：%q", len(parts), sealed)
	}
	raw, err := base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		t.Fatalf("base64 解码 %q: %v", parts[3], err)
	}
	return raw
}

// 辅助：用新的 payload 重建密文信封。
func rebuild(sealed string, payload []byte) string {
	parts := strings.Split(sealed, ".")
	parts[3] = base64.RawURLEncoding.EncodeToString(payload)
	return strings.Join(parts, ".")
}

// 辅助：翻转 payload 中某个字节。
func flip(payload []byte, at int) []byte {
	out := bytes.Clone(payload)
	out[at] ^= 0xff
	return out
}
