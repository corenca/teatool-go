package gm

import (
	"crypto/hmac"

	"github.com/emmansun/gmsm/sm3"
)

// HashSM3 计算 SM3 摘要，返回 32 字节定长结果。
//
// data 为任意长度，允许空切片。需要带密钥的完整性校验时用 HMACSM3。
func HashSM3(data []byte) []byte {
	sum := sm3.Sum(data)
	return sum[:]
}

// HMACSM3 用给定密钥计算 HMAC-SM3，返回 32 字节结果。
//
// key 按 HMAC 规范接受任意长度，data 为消息内容、允许为空。本函数不校验密钥
// 长度；报文加密场景使用打包密钥的后 16 字节，不要与 SM4 加密密钥复用。
func HMACSM3(key, data []byte) []byte {
	mac := hmac.New(sm3.New, key)
	mac.Write(data)
	return mac.Sum(nil)
}
