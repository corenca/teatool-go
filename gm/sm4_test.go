package gm_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/gm"
)

func TestGenerateSM4Key(t *testing.T) {
	for _, size := range []int{16, 32} {
		key, err := gm.GenerateSM4Key(size)
		if err != nil {
			t.Fatalf("GenerateSM4Key(%d): %v", size, err)
		}
		t.Logf("GenerateSM4Key(%d) 长度 = %d", size, len(key))
		if len(key) != size {
			t.Fatalf("长度 = %d，期望 %d", len(key), size)
		}
	}
	for _, size := range []int{0, 15, 17, 64} {
		if _, err := gm.GenerateSM4Key(size); !errors.Is(err, gm.ErrInvalidKey) {
			t.Fatalf("GenerateSM4Key(%d) 错误 = %v，期望 ErrInvalidKey", size, err)
		}
	}
}

func TestParseSM4KeyHex(t *testing.T) {
	cases := []struct {
		name string
		in   string
		size int
	}{
		{"16 字节小写", "000102030405060708090a0b0c0d0e0f", gm.KeySizeSM4GCM},
		{"16 字节大写", "000102030405060708090A0B0C0D0E0F", gm.KeySizeSM4GCM},
		{"32 字节", "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f", gm.KeySizeSM4Packet},
		{"首尾空白", "\n  000102030405060708090a0b0c0d0e0f\t", gm.KeySizeSM4GCM},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			key, err := gm.ParseSM4KeyHex(tc.in, tc.size)
			if err != nil {
				t.Fatalf("ParseSM4KeyHex: %v", err)
			}
			t.Logf("输入 %d 个字符 -> 密钥 %d 字节", len(tc.in), len(key))
			if len(key) != tc.size {
				t.Fatalf("密钥长度 = %d，期望 %d", len(key), tc.size)
			}
			want, _ := hex.DecodeString(strings.TrimSpace(tc.in))
			if !bytes.Equal(key, want) {
				t.Fatal("解析结果与 hex 解码不一致")
			}
		})
	}
}

func TestParseSM4KeyHexRejectsInvalid(t *testing.T) {
	cases := map[string]struct {
		in   string
		size int
	}{
		"空串":      {"", gm.KeySizeSM4GCM},
		"只有空白":    {"  \n\t", gm.KeySizeSM4GCM},
		"奇数长度":    {"abc", gm.KeySizeSM4GCM},
		"非 hex":   {"zz" + strings.Repeat("00", 15), gm.KeySizeSM4GCM},
		"带 0x 前缀": {"0x" + strings.Repeat("00", 15), gm.KeySizeSM4GCM},
		"长度不足":    {strings.Repeat("00", 15), gm.KeySizeSM4GCM},
		"长度超出":    {strings.Repeat("00", 17), gm.KeySizeSM4GCM},
		"尺寸不支持":   {strings.Repeat("00", 16), 24},
	}
	for name, tc := range cases {
		if _, err := gm.ParseSM4KeyHex(tc.in, tc.size); !errors.Is(err, gm.ErrInvalidKey) {
			t.Fatalf("%s: 错误 = %v，期望 ErrInvalidKey", name, err)
		}
	}
}

func TestParseSM4KeyHexRoundTripWithGenerate(t *testing.T) {
	for _, size := range []int{gm.KeySizeSM4GCM, gm.KeySizeSM4Packet} {
		generated, err := gm.GenerateSM4Key(size)
		if err != nil {
			t.Fatalf("GenerateSM4Key(%d): %v", size, err)
		}
		parsed, err := gm.ParseSM4KeyHex(hex.EncodeToString(generated), size)
		if err != nil {
			t.Fatalf("ParseSM4KeyHex: %v", err)
		}
		if !bytes.Equal(parsed, generated) {
			t.Fatalf("%d 字节密钥往返后不一致", size)
		}
		t.Logf("%d 字节密钥 GenerateSM4Key -> hex -> ParseSM4KeyHex 往返一致", size)
	}
}

func TestEncryptSM4GCMRoundTrip(t *testing.T) {
	key, _ := gm.GenerateSM4Key(16)
	cases := []struct {
		name      string
		plaintext string
		aad       string
	}{
		{"普通文本", "hello world", ""},
		{"空明文", "", ""},
		{"中文与 emoji", "密码：p@ssw0rd  🔐", "field:db.password"},
		{"带 AAD", `{"ak":"demo"}`, "tenant=42|path=db.password"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sealed, err := gm.EncryptSM4GCM(key, []byte(tc.plaintext), []byte(tc.aad), gm.EncryptOptions{KID: "2026-01"})
			if err != nil {
				t.Fatalf("EncryptSM4GCM: %v", err)
			}
			t.Logf("明文 %d 字节 -> 密文 %q", len(tc.plaintext), sealed)

			got, err := gm.DecryptSM4GCM(key, sealed, []byte(tc.aad))
			if err != nil {
				t.Fatalf("DecryptSM4GCM: %v", err)
			}
			if string(got) != tc.plaintext {
				t.Fatalf("明文 = %q，期望 %q", got, tc.plaintext)
			}
		})
	}
}

func TestEncryptSM4GCMRejectsTampering(t *testing.T) {
	key, _ := gm.GenerateSM4Key(16)
	sealed, err := gm.EncryptSM4GCM(key, []byte("敏感配置"), nil, gm.EncryptOptions{})
	if err != nil {
		t.Fatalf("EncryptSM4GCM: %v", err)
	}
	payload := payloadOf(t, sealed)

	for _, at := range []int{0, len(payload) / 2, len(payload) - 1} {
		tampered := rebuild(sealed, flip(payload, at))
		if _, err := gm.DecryptSM4GCM(key, tampered, nil); !errors.Is(err, gm.ErrDecrypt) {
			t.Fatalf("篡改第 %d 字节后错误 = %v，期望 ErrDecrypt", at, err)
		}
	}
	t.Logf("篡改 nonce/密文/tag 均被拒绝")

	if _, err := gm.DecryptSM4GCM(key, sealed, []byte("other-aad")); !errors.Is(err, gm.ErrDecrypt) {
		t.Fatalf("错误 AAD 错误 = %v，期望 ErrDecrypt", err)
	}
	otherKey, _ := gm.GenerateSM4Key(16)
	if _, err := gm.DecryptSM4GCM(otherKey, sealed, nil); !errors.Is(err, gm.ErrDecrypt) {
		t.Fatalf("错误密钥错误 = %v，期望 ErrDecrypt", err)
	}
}

func TestEncryptSM4GCMRejectsBadKeyAndFormat(t *testing.T) {
	key, _ := gm.GenerateSM4Key(16)
	if _, err := gm.EncryptSM4GCM(key[:15], []byte("x"), nil, gm.EncryptOptions{}); !errors.Is(err, gm.ErrInvalidKey) {
		t.Fatalf("短密钥错误 = %v，期望 ErrInvalidKey", err)
	}
	sealed, _ := gm.EncryptSM4GCM(key, []byte("x"), nil, gm.EncryptOptions{})

	if _, err := gm.DecryptSM4GCM(key, "v1.sm4gcm..", nil); !errors.Is(err, gm.ErrInvalidFormat) {
		t.Fatalf("空 payload 错误 = %v，期望 ErrInvalidFormat", err)
	}
	short := rebuild(sealed, payloadOf(t, sealed)[:20])
	if _, err := gm.DecryptSM4GCM(key, short, nil); !errors.Is(err, gm.ErrInvalidFormat) {
		t.Fatalf("过短 payload 错误 = %v，期望 ErrInvalidFormat", err)
	}
	unknown := "v1.sm4cbc.." + sealed[len("v1.sm4cbc.."):]
	if _, err := gm.DecryptSM4GCM(key, unknown, nil); !errors.Is(err, gm.ErrUnsupported) {
		t.Fatalf("算法不匹配错误 = %v，期望 ErrUnsupported", err)
	}
}

func TestEncryptSM4GCMDoesNotReuseNonce(t *testing.T) {
	key, _ := gm.GenerateSM4Key(16)
	first, _ := gm.EncryptSM4GCM(key, []byte("same"), nil, gm.EncryptOptions{})
	second, _ := gm.EncryptSM4GCM(key, []byte("same"), nil, gm.EncryptOptions{})
	if bytes.Equal(payloadOf(t, first)[:12], payloadOf(t, second)[:12]) {
		t.Fatal("两次加密的 nonce 相同")
	}
	t.Logf("两次加密 nonce 不同，密文不同")
}

func TestEncryptSM4PacketRoundTrip(t *testing.T) {
	key, _ := gm.GenerateSM4Key(32)
	cases := []struct {
		name      string
		plaintext string
		aad       string
	}{
		{"空明文", "", "POST|/api/login|1730000000"},
		{"15 字节", "123456789012345", ""},
		{"16 字节", "1234567890123456", ""},
		{"17 字节", "12345678901234567", ""},
		{"中文报文", `{"password":"密码🔐","name":"张三"}`, "POST|/api/user|1730000000"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			sealed, err := gm.EncryptSM4Packet(key, []byte(tc.plaintext), []byte(tc.aad), gm.EncryptOptions{KID: "2026-01"})
			if err != nil {
				t.Fatalf("EncryptSM4Packet: %v", err)
			}
			t.Logf("明文 %d 字节 -> 密文 %s", len(tc.plaintext), sealed)

			got, err := gm.DecryptSM4Packet(key, sealed, []byte(tc.aad))
			if err != nil {
				t.Fatalf("DecryptSM4Packet: %v", err)
			}
			if string(got) != tc.plaintext {
				t.Fatalf("明文 = %q，期望 %q", got, tc.plaintext)
			}
		})
	}
}

func TestEncryptSM4PacketRejectsTampering(t *testing.T) {
	key, _ := gm.GenerateSM4Key(32)
	sealed, err := gm.EncryptSM4Packet(key, []byte(`{"amount":100}`), []byte("POST|/pay"), gm.EncryptOptions{})
	if err != nil {
		t.Fatalf("EncryptSM4Packet: %v", err)
	}
	payload := payloadOf(t, sealed)
	// payload = iv(16) || ciphertext || mac(32)
	points := map[string]int{
		"iv":         0,
		"ciphertext": 16,
		"mac":        len(payload) - 1,
	}
	for name, at := range points {
		tampered := rebuild(sealed, flip(payload, at))
		if _, err := gm.DecryptSM4Packet(key, tampered, []byte("POST|/pay")); !errors.Is(err, gm.ErrDecrypt) {
			t.Fatalf("篡改 %s 后错误 = %v，期望 ErrDecrypt", name, err)
		}
	}
	t.Logf("篡改 iv/ciphertext/mac 均被拒绝")

	if _, err := gm.DecryptSM4Packet(key, sealed, []byte("GET|/pay")); !errors.Is(err, gm.ErrDecrypt) {
		t.Fatalf("错误 AAD 错误 = %v，期望 ErrDecrypt", err)
	}
	otherKey, _ := gm.GenerateSM4Key(32)
	if _, err := gm.DecryptSM4Packet(otherKey, sealed, []byte("POST|/pay")); !errors.Is(err, gm.ErrDecrypt) {
		t.Fatalf("错误密钥错误 = %v，期望 ErrDecrypt", err)
	}
}

func TestEncryptSM4PacketRejectsBadKeyAndFormat(t *testing.T) {
	key, _ := gm.GenerateSM4Key(32)
	for _, size := range []int{0, 16, 31, 33} {
		badKey := make([]byte, size)
		copy(badKey, key)
		if _, err := gm.EncryptSM4Packet(badKey, []byte("x"), nil, gm.EncryptOptions{}); !errors.Is(err, gm.ErrInvalidKey) {
			t.Fatalf("密钥 %d 字节错误 = %v，期望 ErrInvalidKey", size, err)
		}
	}

	sealed, _ := gm.EncryptSM4Packet(key, []byte("x"), nil, gm.EncryptOptions{})
	payload := payloadOf(t, sealed)
	// 破坏 CBC 块对齐：删掉密文里的一个字节。
	broken := append(bytes.Clone(payload[:16]), payload[17:]...)
	if _, err := gm.DecryptSM4Packet(key, rebuild(sealed, broken), nil); !errors.Is(err, gm.ErrInvalidFormat) {
		t.Fatalf("非块对齐错误 = %v，期望 ErrInvalidFormat", err)
	}
	if _, err := gm.DecryptSM4Packet(key, rebuild(sealed, payload[:40]), nil); !errors.Is(err, gm.ErrInvalidFormat) {
		t.Fatalf("长度不足错误 = %v，期望 ErrInvalidFormat", err)
	}
}

func TestEncryptSM4PacketUsesDistinctIV(t *testing.T) {
	key, _ := gm.GenerateSM4Key(32)
	first, _ := gm.EncryptSM4Packet(key, []byte("same"), nil, gm.EncryptOptions{})
	second, _ := gm.EncryptSM4Packet(key, []byte("same"), nil, gm.EncryptOptions{})
	if bytes.Equal(payloadOf(t, first)[:16], payloadOf(t, second)[:16]) {
		t.Fatal("两次加密的 IV 相同")
	}
	t.Logf("两次加密 IV 不同")
}
