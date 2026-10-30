// Package gm 提供国密 SM2 / SM3 / SM4 的加解密工具。
//
// 对称密文共用一层文本信封，便于存进配置、JSON 与 URL：
//
//	v1.<alg>.<kid>.<base64url(payload)>
//
// 两种算法各有适用场景：
//
//   - SM4-GCM 用于后端闭环的配置项加密，密钥 16 字节，认证加密；
//   - SM4-CBC + PKCS#7 + HMAC-SM3 用于前后端互通的报文加密，密钥 32 字节：
//     前 16 字节为 SM4 密钥，后 16 字节为 HMAC 密钥，采用 Encrypt-then-MAC。
//
// SM2 用于短数据加密（密码、令牌、密钥），默认遵循 GB/T 32918.4 的 C1C3C2
// 与未压缩点编码，可以通过 SM2Options 显式切换 C1C2C3 或 ASN.1。
//
// 解密失败一律返回 ErrDecrypt，不返回部分明文，也不暴露底层失败原因。
// 包内函数无状态，可并发调用。
package gm

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
)

// 对称密钥长度。传给 GenerateSM4Key、ParseSM4KeyHex 以及两个 SM4 加密入口。
const (
	// KeySizeSM4GCM 是 SM4-GCM 的密钥长度：16 字节，对应 32 个 hex 字符。
	KeySizeSM4GCM = 16
	// KeySizeSM4Packet 是报文加密的密钥长度：32 字节，对应 64 个 hex 字符。
	//
	// 前 16 字节为 SM4 密钥，后 16 字节为 HMAC-SM3 密钥，两段必须相互独立。
	KeySizeSM4Packet = 32
)

// 信封固定字段。
const (
	envelopeVersion = "v1"
	algorithmGCM    = "sm4gcm"
	algorithmPacket = "sm4cbc"

	// kidMaxLen 限制密钥版本标识的长度，避免信封被滥用为数据通道。
	kidMaxLen = 64
)

// 错误哨兵。全部用 errors.Is 判断，不要比较错误字符串。
var (
	// ErrInvalidKey 表示密钥、公钥或私钥不合法。
	//
	// 触发场景：密钥长度不是 16/32 字节、hex 解码失败、SM2 PEM 结构错误或
	// 算法不匹配。错误信息里只含长度与格式，不会包含密钥本身。
	ErrInvalidKey = errors.New("gm: 密钥无效")
	// ErrInvalidFormat 表示密文信封、base64 或长度不合法。
	//
	// 触发场景：信封段数不是 4 段、payload 不是合法 base64url、kid 超长或
	// 含非法字符、密文长度不足。遇到它说明数据被截断或根本不是本包的密文。
	ErrInvalidFormat = errors.New("gm: 密文格式无效")
	// ErrUnsupported 表示版本、算法或选项不受支持。
	//
	// 触发场景：信封版本不是 v1、算法标识不是 sm4gcm/sm4cbc、SM2 选项组合
	// 不合法。通常意味着两端库版本不一致。
	ErrUnsupported = errors.New("gm: 不支持的版本或算法")
	// ErrDecrypt 是解密失败的统一出口，不携带底层细节。
	//
	// 密钥错误、密文被篡改、GCM 认证失败、HMAC 校验不一致都归到它，避免把
	// "密钥不对"与"数据被改过"的差异泄露给调用方。错误不包底层原因，调用方
	// 也不要尝试区分。
	ErrDecrypt = errors.New("gm: 解密失败")
)

// EncryptOptions 控制对称加密写入信封的元数据。
//
// 零值可用：不加版本标识，得到 v1.sm4gcm..<payload> 形式的信封。
type EncryptOptions struct {
	// KID 是密钥版本标识，可空；非空时只允许字母、数字、_ 与 -，最长 64 字节。
	//
	// 它写入信封第三段，解密前可用 KID 读出，按版本从密钥环里选对应密钥，
	// 从而支持密钥轮换。标识本身不是密钥、不是认证信息，不要放敏感数据。
	KID string
}

// KID 读取对称密文信封里的密钥版本标识，用于按版本选择密钥。
//
// sealed 必须是 EncryptSM4GCM 或 EncryptSM4Packet 生成的完整密文；kid 为空时返回空串。
// 信封段数、版本、算法、kid 字符或 base64 不合法时返回 ErrInvalidFormat
// 或 ErrUnsupported。
func KID(sealed string) (string, error) {
	_, kid, _, err := parseEnvelope(sealed)
	if err != nil {
		return "", err
	}
	return kid, nil
}

// buildEnvelope 校验 kid 并拼出文本信封。
func buildEnvelope(algorithm, kid string, payload []byte) (string, error) {
	if err := validateKID(kid); err != nil {
		return "", err
	}
	var b strings.Builder
	b.Grow(len(envelopeVersion) + len(algorithm) + len(kid) + 3 + base64.RawURLEncoding.EncodedLen(len(payload)))
	b.WriteString(envelopeVersion)
	b.WriteByte('.')
	b.WriteString(algorithm)
	b.WriteByte('.')
	b.WriteString(kid)
	b.WriteByte('.')
	b.WriteString(base64.RawURLEncoding.EncodeToString(payload))
	return b.String(), nil
}

// parseEnvelope 拆出算法、kid 与 payload。
func parseEnvelope(sealed string) (algorithm, kid string, payload []byte, err error) {
	parts := strings.SplitN(sealed, ".", 4)
	if len(parts) != 4 {
		return "", "", nil, fmt.Errorf("%w: 信封应为 4 段", ErrInvalidFormat)
	}
	if parts[0] != envelopeVersion {
		return "", "", nil, fmt.Errorf("%w: 版本 %q", ErrUnsupported, parts[0])
	}
	if parts[1] != algorithmGCM && parts[1] != algorithmPacket {
		return "", "", nil, fmt.Errorf("%w: 算法 %q", ErrUnsupported, parts[1])
	}
	if err := validateKID(parts[2]); err != nil {
		return "", "", nil, err
	}
	payload, err = base64.RawURLEncoding.DecodeString(parts[3])
	if err != nil {
		return "", "", nil, fmt.Errorf("%w: base64 解码失败", ErrInvalidFormat)
	}
	return parts[1], parts[2], payload, nil
}

// validateKID 限制 kid 的字符集与长度。
func validateKID(kid string) error {
	if len(kid) > kidMaxLen {
		return fmt.Errorf("%w: kid 超过 %d 字节", ErrInvalidFormat, kidMaxLen)
	}
	for _, r := range kid {
		switch {
		case r >= 'a' && r <= 'z':
		case r >= 'A' && r <= 'Z':
		case r >= '0' && r <= '9':
		case r == '_' || r == '-':
		default:
			return fmt.Errorf("%w: kid 含非法字符 %q", ErrInvalidFormat, r)
		}
	}
	return nil
}
