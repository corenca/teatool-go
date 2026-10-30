package gm

import (
	"crypto/ecdsa"
	"crypto/rand"
	"encoding/hex"
	"encoding/pem"
	"fmt"

	"github.com/emmansun/gmsm/sm2"
	"github.com/emmansun/gmsm/smx509"
)

// SM2CiphertextOrder 是 SM2 密文的拼接顺序。
//
// 两种顺序只是 C1、C2、C3 三段的位置不同，并不影响算法安全性；跨系统互通时
// 必须与对端一致，否则解密会得到乱码或直接失败（C3 校验不过）。
type SM2CiphertextOrder int

const (
	// C1C3C2 是 GB/T 32918.4 与多数实现的默认顺序。
	//
	// 零值，不显式传 SM2Options 时就是它。新系统互通优先选这个。
	C1C3C2 SM2CiphertextOrder = iota
	// C1C2C3 是部分存量系统使用的顺序。
	//
	// 只用于对接改造困难的旧系统；新代码不要主动选它。
	C1C2C3
)

// SM2Options 控制 SM2 密文的编码方式。零值等价于 C1C3C2 裸编码（未压缩点，带 04 前缀）。
//
// ASN1 为 true 时输出/接受 ASN.1 DER 结构，此时 Order 不参与编码；需要与
// Java BouncyCastle 默认输出等老实现对接时再用。
type SM2Options struct {
	// Order 是 C1、C2、C3 的拼接顺序，零值为 C1C3C2；ASN1 为 true 时忽略。
	Order SM2CiphertextOrder
	// ASN1 为 true 时使用 ASN.1 DER 编码，为 false 时使用裸字节拼接。
	ASN1 bool
}

// GenerateSM2Key 生成一对 SM2 密钥。
//
// 返回的私钥内嵌 ecdsa.PrivateKey，公钥通过 &priv.PublicKey 取用。私钥只应
// 留在服务端；公钥可用 SM2PublicKeyHex 下发给前端。
func GenerateSM2Key() (*sm2.PrivateKey, error) {
	priv, err := sm2.GenerateKey(rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("gm: 生成 SM2 密钥失败: %w", err)
	}
	return priv, nil
}

// MarshalSM2PrivateKeyPEM 把 SM2 私钥编码为 PKCS#8 PEM 文本。
//
// priv 为 nil 或编码失败时返回 ErrInvalidKey。PEM 适合写入密钥文件或密钥
// 管理系统，不要写进日志、配置模板或代码仓库。
func MarshalSM2PrivateKeyPEM(priv *sm2.PrivateKey) ([]byte, error) {
	if priv == nil {
		return nil, fmt.Errorf("%w: 私钥为 nil", ErrInvalidKey)
	}
	der, err := smx509.MarshalPKCS8PrivateKey(priv)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: der}), nil
}

// ParseSM2PrivateKeyPEM 解析 PKCS#8 PEM 私钥。
//
// data 必须是完整的 PEM 文本（可包含首尾空白）。PEM 结构错误、私钥不是 PKCS#8
// 或不是 SM2 密钥时返回 ErrInvalidKey。
func ParseSM2PrivateKeyPEM(data []byte) (*sm2.PrivateKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%w: PEM 解码失败", ErrInvalidKey)
	}
	key, err := smx509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: PKCS#8 解析失败", ErrInvalidKey)
	}
	priv, ok := key.(*sm2.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("%w: 不是 SM2 私钥", ErrInvalidKey)
	}
	return priv, nil
}

// MarshalSM2PublicKeyPEM 把 SM2 公钥编码为 PKIX PEM 文本。
//
// pub 为 nil 或编码失败时返回 ErrInvalidKey。前端对接通常用 SM2PublicKeyHex，
// PEM 适合写入证书或公钥文件。
func MarshalSM2PublicKeyPEM(pub *ecdsa.PublicKey) ([]byte, error) {
	if pub == nil {
		return nil, fmt.Errorf("%w: 公钥为 nil", ErrInvalidKey)
	}
	der, err := smx509.MarshalPKIXPublicKey(pub)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}), nil
}

// ParseSM2PublicKeyPEM 解析 PKIX PEM 公钥。
//
// data 必须是完整的 PEM 文本。解析失败或不是 SM2 公钥时返回 ErrInvalidKey。
func ParseSM2PublicKeyPEM(data []byte) (*ecdsa.PublicKey, error) {
	block, _ := pem.Decode(data)
	if block == nil {
		return nil, fmt.Errorf("%w: PEM 解码失败", ErrInvalidKey)
	}
	key, err := smx509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("%w: PKIX 解析失败", ErrInvalidKey)
	}
	pub, ok := key.(*ecdsa.PublicKey)
	if !ok {
		return nil, fmt.Errorf("%w: 不是 SM2 公钥", ErrInvalidKey)
	}
	return pub, nil
}

// SM2PrivateKeyHex 返回私钥的 32 字节 hex（64 个字符），可直接放进环境变量。
//
// priv 为 nil 时返回空串；数值较小时左侧补零，长度始终为 64 个字符。
func SM2PrivateKeyHex(priv *sm2.PrivateKey) string {
	if priv == nil || priv.D == nil {
		return ""
	}
	return hex.EncodeToString(priv.D.FillBytes(make([]byte, 32)))
}

// ParseSM2PrivateKeyHex 解析 32 字节 hex 私钥。
//
// s 必须是 64 个 hex 字符；空串、非 hex、长度不符、私钥为 0 或超出曲线阶时
// 返回 ErrInvalidKey。
func ParseSM2PrivateKeyHex(s string) (*sm2.PrivateKey, error) {
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != 32 {
		return nil, fmt.Errorf("%w: 私钥 hex 必须是 32 字节", ErrInvalidKey)
	}
	priv, err := sm2.NewPrivateKey(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return priv, nil
}

// SM2PublicKeyHex 返回未压缩公钥的 hex 编码（04||X||Y，共 130 个字符）。
//
// pub 为 nil 时返回空串。前端 sm-crypto 直接使用该字符串作为公钥。
func SM2PublicKeyHex(pub *ecdsa.PublicKey) string {
	if pub == nil || pub.X == nil || pub.Y == nil {
		return ""
	}
	raw := make([]byte, 65)
	raw[0] = 0x04
	pub.X.FillBytes(raw[1:33])
	pub.Y.FillBytes(raw[33:])
	return hex.EncodeToString(raw)
}

// ParseSM2PublicKeyHex 解析未压缩公钥 hex（04||X||Y）。
//
// s 必须是 130 个 hex 字符且以 04 开头；非 hex、长度不符或点不在曲线上时
// 返回 ErrInvalidKey。
func ParseSM2PublicKeyHex(s string) (*ecdsa.PublicKey, error) {
	raw, err := hex.DecodeString(s)
	if err != nil || len(raw) != 65 || raw[0] != 0x04 {
		return nil, fmt.Errorf("%w: 公钥 hex 必须是 04||X||Y", ErrInvalidKey)
	}
	pub, err := sm2.NewPublicKey(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return pub, nil
}

// EncryptSM2 用 SM2 公钥加密短数据，返回裸密文字节。
//
// pub 必须是 SM2 曲线公钥；plaintext 建议不超过几百字节，大报文用 EncryptSM4Packet；
// opts 控制密文顺序与编码（见 SM2Options）。默认输出 C1C3C2、未压缩点、带 04。
//
// 公钥为 nil 或加密失败返回 ErrInvalidKey；选项不合法返回 ErrUnsupported。
// 返回值是裸字节，传输或存配置前需自行 base64/hex 编码。
func EncryptSM2(pub *ecdsa.PublicKey, plaintext []byte, opts SM2Options) ([]byte, error) {
	if pub == nil {
		return nil, fmt.Errorf("%w: 公钥为 nil", ErrInvalidKey)
	}
	encrypterOpts, err := opts.encrypterOpts()
	if err != nil {
		return nil, err
	}
	ciphertext, err := sm2.Encrypt(rand.Reader, pub, plaintext, encrypterOpts)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return ciphertext, nil
}

// DecryptSM2 用 SM2 私钥解密密文。
//
// priv 为 nil 返回 ErrInvalidKey；opts 必须与加密时一致（C1C3C2 / C1C2C3 /
// ASN.1），否则解密失败。密文损坏、私钥不匹配或编码不符统一返回 ErrDecrypt，
// 不返回部分明文。
func DecryptSM2(priv *sm2.PrivateKey, ciphertext []byte, opts SM2Options) ([]byte, error) {
	if priv == nil {
		return nil, fmt.Errorf("%w: 私钥为 nil", ErrInvalidKey)
	}
	decrypterOpts, err := opts.decrypterOpts()
	if err != nil {
		return nil, err
	}
	plaintext, err := priv.Decrypt(nil, ciphertext, decrypterOpts)
	if err != nil {
		return nil, fmt.Errorf("%w: SM2 解密失败", ErrDecrypt)
	}
	return plaintext, nil
}

// encrypterOpts 把公开选项翻译成 gmsm 的加密选项。
func (o SM2Options) encrypterOpts() (*sm2.EncrypterOpts, error) {
	switch {
	case o.ASN1:
		return sm2.ASN1EncrypterOpts, nil
	case o.Order == C1C2C3:
		return sm2.NewPlainEncrypterOpts(sm2.MarshalUncompressed, sm2.C1C2C3), nil
	case o.Order == C1C3C2:
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: SM2 密文顺序 %d", ErrUnsupported, o.Order)
	}
}

// decrypterOpts 把公开选项翻译成 gmsm 的解密选项。
func (o SM2Options) decrypterOpts() (*sm2.DecrypterOpts, error) {
	switch {
	case o.ASN1:
		return sm2.ASN1DecrypterOpts, nil
	case o.Order == C1C2C3:
		return sm2.NewPlainDecrypterOpts(sm2.C1C2C3), nil
	case o.Order == C1C3C2:
		return nil, nil
	default:
		return nil, fmt.Errorf("%w: SM2 密文顺序 %d", ErrUnsupported, o.Order)
	}
}
