package gm_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/gm"
)

func TestSM2KeyPEMAndHexRoundTrip(t *testing.T) {
	priv, err := gm.GenerateSM2Key()
	if err != nil {
		t.Fatalf("GenerateSM2Key: %v", err)
	}
	t.Logf("公钥 hex 长度 = %d，开头 = %s", len(gm.SM2PublicKeyHex(&priv.PublicKey)), gm.SM2PublicKeyHex(&priv.PublicKey)[:10])

	privPEM, err := gm.MarshalSM2PrivateKeyPEM(priv)
	if err != nil {
		t.Fatalf("MarshalSM2PrivateKeyPEM: %v", err)
	}
	parsedPriv, err := gm.ParseSM2PrivateKeyPEM(privPEM)
	if err != nil {
		t.Fatalf("ParseSM2PrivateKeyPEM: %v", err)
	}
	if parsedPriv.D.Cmp(priv.D) != 0 {
		t.Fatal("私钥 PEM 往返后不一致")
	}

	pubPEM, err := gm.MarshalSM2PublicKeyPEM(&priv.PublicKey)
	if err != nil {
		t.Fatalf("MarshalSM2PublicKeyPEM: %v", err)
	}
	parsedPub, err := gm.ParseSM2PublicKeyPEM(pubPEM)
	if err != nil {
		t.Fatalf("ParseSM2PublicKeyPEM: %v", err)
	}
	if parsedPub.X.Cmp(priv.X) != 0 || parsedPub.Y.Cmp(priv.Y) != 0 {
		t.Fatal("公钥 PEM 往返后不一致")
	}

	hexPub := gm.SM2PublicKeyHex(&priv.PublicKey)
	fromHex, err := gm.ParseSM2PublicKeyHex(hexPub)
	if err != nil {
		t.Fatalf("ParseSM2PublicKeyHex: %v", err)
	}
	if fromHex.X.Cmp(priv.X) != 0 || fromHex.Y.Cmp(priv.Y) != 0 {
		t.Fatal("公钥 hex 往返后不一致")
	}
	t.Logf("私钥 PKCS#8、公钥 PKIX、公钥 hex 三种编码往返一致")
}

func TestSM2EncryptDecrypt(t *testing.T) {
	priv, _ := gm.GenerateSM2Key()
	cases := []struct {
		name string
		opts gm.SM2Options
	}{
		{"默认 C1C3C2", gm.SM2Options{}},
		{"C1C2C3", gm.SM2Options{Order: gm.C1C2C3}},
		{"ASN.1", gm.SM2Options{ASN1: true}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			plaintext := []byte("登录密码：p@ss🔐")
			ciphertext, err := gm.EncryptSM2(&priv.PublicKey, plaintext, tc.opts)
			if err != nil {
				t.Fatalf("EncryptSM2: %v", err)
			}
			t.Logf("明文 %d 字节 -> 密文 %d 字节", len(plaintext), len(ciphertext))

			got, err := gm.DecryptSM2(priv, ciphertext, tc.opts)
			if err != nil {
				t.Fatalf("DecryptSM2: %v", err)
			}
			if !bytes.Equal(got, plaintext) {
				t.Fatalf("明文 = %q，期望 %q", got, plaintext)
			}
		})
	}
}

func TestSM2DecryptRejectsWrongKey(t *testing.T) {
	priv, _ := gm.GenerateSM2Key()
	other, _ := gm.GenerateSM2Key()
	ciphertext, err := gm.EncryptSM2(&priv.PublicKey, []byte("secret"), gm.SM2Options{})
	if err != nil {
		t.Fatalf("EncryptSM2: %v", err)
	}
	if _, err := gm.DecryptSM2(other, ciphertext, gm.SM2Options{}); !errors.Is(err, gm.ErrDecrypt) {
		t.Fatalf("错误私钥错误 = %v，期望 ErrDecrypt", err)
	}
	if _, err := gm.DecryptSM2(priv, ciphertext[:len(ciphertext)-1], gm.SM2Options{}); !errors.Is(err, gm.ErrDecrypt) {
		t.Fatalf("截断密文错误 = %v，期望 ErrDecrypt", err)
	}
}

func TestSM2RejectsBadPublicKey(t *testing.T) {
	cases := []string{
		"",
		"04",
		"04" + "00" + "00",
		"zz" + strings.Repeat("00", 64),
	}
	for _, s := range cases {
		if _, err := gm.ParseSM2PublicKeyHex(s); !errors.Is(err, gm.ErrInvalidKey) {
			t.Fatalf("ParseSM2PublicKeyHex(%q) 错误 = %v，期望 ErrInvalidKey", s, err)
		}
	}
}

// 保证外层使用的公钥类型就是 gmsm 的 sm2.PublicKey，避免调用方再引一层别名。

func TestSM2PrivateKeyHexRoundTrip(t *testing.T) {
	priv, err := gm.GenerateSM2Key()
	if err != nil {
		t.Fatalf("GenerateSM2Key: %v", err)
	}

	privHex := gm.SM2PrivateKeyHex(priv)
	t.Logf("私钥 hex 长度 = %d", len(privHex))
	if len(privHex) != 64 {
		t.Fatalf("私钥 hex 长度 = %d，期望 64", len(privHex))
	}

	parsed, err := gm.ParseSM2PrivateKeyHex(privHex)
	if err != nil {
		t.Fatalf("ParseSM2PrivateKeyHex: %v", err)
	}
	if parsed.D.Cmp(priv.D) != 0 {
		t.Fatal("私钥 hex 往返后 D 不一致")
	}
	if parsed.X.Cmp(priv.X) != 0 || parsed.Y.Cmp(priv.Y) != 0 {
		t.Fatal("私钥 hex 往返后公钥不一致")
	}
}

func TestSM2PrivateKeyHexRejectsBad(t *testing.T) {
	cases := map[string]string{
		"空串":      "",
		"过短":      "00",
		"零值":      strings.Repeat("00", 32),
		"超出曲线阶":   strings.Repeat("ff", 32),
		"非 hex":   "zz" + strings.Repeat("00", 31),
		"带 0x 前缀": "0x" + strings.Repeat("01", 31),
	}
	for name, s := range cases {
		if _, err := gm.ParseSM2PrivateKeyHex(s); !errors.Is(err, gm.ErrInvalidKey) {
			t.Fatalf("%s: 错误 = %v，期望 ErrInvalidKey", name, err)
		}
	}
}
