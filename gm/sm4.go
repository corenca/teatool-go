package gm

import (
	"bytes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/emmansun/gmsm/padding"
	"github.com/emmansun/gmsm/sm3"
	"github.com/emmansun/gmsm/sm4"
)

// GenerateSM4Key 生成密码学安全的随机对称密钥。
//
// size 只接受 KeySizeSM4GCM（16 字节，给 EncryptSM4GCM）或 KeySizeSM4Packet（32 字节，给
// EncryptSM4Packet），其他值返回 ErrInvalidKey。需要文本形式时用 hex.EncodeToString
// 编码后写入环境变量或密钥管理系统，读取时用 ParseSM4KeyHex。
func GenerateSM4Key(size int) ([]byte, error) {
	if size != KeySizeSM4GCM && size != KeySizeSM4Packet {
		return nil, fmt.Errorf("%w: 长度 %d，只支持 %d 或 %d 字节", ErrInvalidKey, size, KeySizeSM4GCM, KeySizeSM4Packet)
	}
	key := make([]byte, size)
	if _, err := rand.Read(key); err != nil {
		return nil, fmt.Errorf("gm: 生成密钥失败: %w", err)
	}
	return key, nil
}

// ParseSM4KeyHex 把 hex 字符串解析为对称密钥，并校验长度。
//
// s 适合直接传环境变量、配置中心或 KMS 返回的 hex 字符串：首尾空白会被忽略，
// 大写与小写均可；size 只接受 KeySizeSM4GCM（16 字节）或 KeySizeSM4Packet（32 字节）。
//
// 返回解码后的密钥字节。s 为空、不是合法 hex、长度与 size 不符，或 size 不是
// 16/32 时，统一返回 ErrInvalidKey。
func ParseSM4KeyHex(s string, size int) ([]byte, error) {
	if size != KeySizeSM4GCM && size != KeySizeSM4Packet {
		return nil, fmt.Errorf("%w: 长度 %d，只支持 %d 或 %d 字节", ErrInvalidKey, size, KeySizeSM4GCM, KeySizeSM4Packet)
	}
	raw := strings.TrimSpace(s)
	if raw == "" {
		return nil, fmt.Errorf("%w: 密钥为空", ErrInvalidKey)
	}
	key, err := hex.DecodeString(raw)
	if err != nil {
		return nil, fmt.Errorf("%w: 不是合法 hex", ErrInvalidKey)
	}
	if len(key) != size {
		return nil, fmt.Errorf("%w: 需要 %d 字节，实际 %d 字节", ErrInvalidKey, size, len(key))
	}
	return key, nil
}

// EncryptSM4GCM 用 SM4-GCM 加密配置项等后端闭环数据，返回可直接写入 YAML/JSON/env
// 的文本密文（v1.sm4gcm.<kid>.<payload>）。
//
// key 必须是 16 字节；plaintext 为任意长度明文，允许空；aad 为附加认证数据，
// 可为 nil，建议绑定配置项路径（如 "db.password"），解密时必须提供完全相同的
// 值；opts.KID 是可选密钥版本标识，写入密文头。
//
// 密钥长度不合法返回 ErrInvalidKey；kid 含非法字符或过长返回 ErrInvalidFormat；
// 随机源失败返回底层错误。
func EncryptSM4GCM(key, plaintext, aad []byte, opts EncryptOptions) (string, error) {
	aead, err := newGCM(key)
	if err != nil {
		return "", err
	}

	nonce := make([]byte, aead.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", fmt.Errorf("gm: 生成 nonce 失败: %w", err)
	}

	sealed := aead.Seal(nil, nonce, plaintext, aad)
	payload := make([]byte, 0, len(nonce)+len(sealed))
	payload = append(payload, nonce...)
	payload = append(payload, sealed...)
	return buildEnvelope(algorithmGCM, opts.KID, payload)
}

// DecryptSM4GCM 解密 EncryptSM4GCM 生成的文本密文。
//
// key 与 aad 必须与加密时完全一致；sealed 是完整信封。成功返回明文。算法不是
// sm4gcm 时返回 ErrUnsupported；载荷长度不足或 base64 不合法返回 ErrInvalidFormat；
// 密钥、AAD 或密文不匹配返回 ErrDecrypt，不返回部分明文。
func DecryptSM4GCM(key []byte, sealed string, aad []byte) ([]byte, error) {
	algorithm, _, payload, err := parseEnvelope(sealed)
	if err != nil {
		return nil, err
	}
	if algorithm != algorithmGCM {
		return nil, fmt.Errorf("%w: 算法 %q 不是 %s", ErrUnsupported, algorithm, algorithmGCM)
	}

	aead, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	nonceSize := aead.NonceSize()
	if len(payload) < nonceSize+aead.Overhead() {
		return nil, fmt.Errorf("%w: GCM 载荷过短", ErrInvalidFormat)
	}

	plaintext, err := aead.Open(nil, payload[:nonceSize], payload[nonceSize:], aad)
	if err != nil {
		return nil, fmt.Errorf("%w: GCM 认证失败", ErrDecrypt)
	}
	return plaintext, nil
}

// newGCM 构造 SM4-GCM，并校验密钥长度。
func newGCM(key []byte) (cipher.AEAD, error) {
	if len(key) != KeySizeSM4GCM {
		return nil, fmt.Errorf("%w: SM4-GCM 需要 %d 字节", ErrInvalidKey, KeySizeSM4GCM)
	}
	block, err := sm4.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, fmt.Errorf("gm: 构造 GCM 失败: %w", err)
	}
	return aead, nil
}

const (
	packetIVSize    = 16
	packetMACSize   = 32
	packetBlockSize = 16
)

// EncryptSM4Packet 用 SM4-CBC + PKCS#7 + HMAC-SM3 加密接口报文，返回文本密文
// （v1.sm4cbc.<kid>.<payload>），前后端按 README 的字节契约互通。
//
// key 必须是 32 字节：前 16 字节为 SM4 密钥，后 16 字节为 HMAC 密钥；plaintext
// 为任意长度明文，允许空，内部自动做 PKCS#7 填充；aad 参与 MAC 计算，可为 nil，
// 建议绑定 method、path 与时间戳；opts.KID 写入密文头并参与 MAC 计算。
//
// 密钥长度不合法返回 ErrInvalidKey；kid 非法返回 ErrInvalidFormat；随机源失败
// 返回底层错误。
func EncryptSM4Packet(key, plaintext, aad []byte, opts EncryptOptions) (string, error) {
	block, macKey, err := newPacketCipher(key)
	if err != nil {
		return "", err
	}
	if err := validateKID(opts.KID); err != nil {
		return "", err
	}

	iv := make([]byte, packetIVSize)
	if _, err := rand.Read(iv); err != nil {
		return "", fmt.Errorf("gm: 生成 IV 失败: %w", err)
	}
	return encryptSM4PacketWithIV(block, macKey, plaintext, aad, opts.KID, iv)
}

// encryptSM4PacketWithIV 用指定 IV 完成加密，供内部测试与固定向量对齐。
func encryptSM4PacketWithIV(block cipher.Block, macKey, plaintext, aad []byte, kid string, iv []byte) (string, error) {
	if len(iv) != packetIVSize {
		return "", fmt.Errorf("%w: IV 需要 %d 字节", ErrInvalidKey, packetIVSize)
	}

	padded := padding.NewPKCS7Padding(packetBlockSize).Pad(bytes.Clone(plaintext))
	ciphertext := make([]byte, len(padded))
	cipher.NewCBCEncrypter(block, iv).CryptBlocks(ciphertext, padded)

	mac := packetMAC(macKey, kid, aad, iv, ciphertext)
	payload := make([]byte, 0, len(iv)+len(ciphertext)+len(mac))
	payload = append(payload, iv...)
	payload = append(payload, ciphertext...)
	payload = append(payload, mac...)

	return buildEnvelope(algorithmPacket, kid, payload)
}

// DecryptSM4Packet 解密 EncryptSM4Packet 生成的文本密文。
//
// 先以常量时间比较 MAC，通过后才解密与去填充；key 与 aad 必须与加密时一致。
// 算法不是 sm4cbc 返回 ErrUnsupported；载荷过短、块未对齐或 base64 不合法返回
// ErrInvalidFormat；密钥、AAD、MAC 或填充不匹配统一返回 ErrDecrypt，不返回
// 部分明文。
func DecryptSM4Packet(key []byte, sealed string, aad []byte) ([]byte, error) {
	algorithm, kid, payload, err := parseEnvelope(sealed)
	if err != nil {
		return nil, err
	}
	if algorithm != algorithmPacket {
		return nil, fmt.Errorf("%w: 算法 %q 不是 %s", ErrUnsupported, algorithm, algorithmPacket)
	}

	block, macKey, err := newPacketCipher(key)
	if err != nil {
		return nil, err
	}
	if len(payload) < packetIVSize+packetBlockSize+packetMACSize {
		return nil, fmt.Errorf("%w: 报文载荷过短", ErrInvalidFormat)
	}

	iv := payload[:packetIVSize]
	mac := payload[len(payload)-packetMACSize:]
	ciphertext := payload[packetIVSize : len(payload)-packetMACSize]
	if len(ciphertext)%packetBlockSize != 0 {
		return nil, fmt.Errorf("%w: 密文未按 %d 字节对齐", ErrInvalidFormat, packetBlockSize)
	}

	want := packetMAC(macKey, kid, aad, iv, ciphertext)
	if !hmac.Equal(want, mac) {
		return nil, fmt.Errorf("%w: MAC 校验失败", ErrDecrypt)
	}

	plaintext := make([]byte, len(ciphertext))
	cipher.NewCBCDecrypter(block, iv).CryptBlocks(plaintext, ciphertext)
	plaintext, err = padding.NewPKCS7Padding(packetBlockSize).Unpad(plaintext)
	if err != nil {
		return nil, fmt.Errorf("%w: 填充无效", ErrDecrypt)
	}
	return plaintext, nil
}

// newPacketCipher 切分报文密钥并构造 SM4。
func newPacketCipher(key []byte) (cipher.Block, []byte, error) {
	if len(key) != KeySizeSM4Packet {
		return nil, nil, fmt.Errorf("%w: 报文加密需要 %d 字节", ErrInvalidKey, KeySizeSM4Packet)
	}
	block, err := sm4.NewCipher(key[:KeySizeSM4GCM])
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %v", ErrInvalidKey, err)
	}
	return block, key[KeySizeSM4GCM:], nil
}

// packetMAC 计算 MAC-SM3，输入为版本、算法、kid、AAD、IV 与密文。
func packetMAC(macKey []byte, kid string, aad, iv, ciphertext []byte) []byte {
	header := envelopeVersion + "." + algorithmPacket
	input := make([]byte, 0, len(header)+1+len(kid)+1+len(aad)+len(iv)+len(ciphertext))
	input = append(input, header...)
	input = append(input, 0)
	input = append(input, kid...)
	input = append(input, 0)
	input = append(input, aad...)
	input = append(input, iv...)
	input = append(input, ciphertext...)

	mac := hmac.New(sm3.New, macKey)
	mac.Write(input)
	return mac.Sum(nil)
}
