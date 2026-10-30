package gm

import (
	"encoding/hex"
	"testing"
)

// 固定向量由 sm-crypto 0.5.8 生成（key/IV/kid/AAD 与 interop_test.go 相同）。
const (
	interopKey32  = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	interopIV     = "a0a1a2a3a4a5a6a7a8a9aaabacadaeaf"
	interopKID    = "2026-01"
	interopAAD    = "POST|/api/login|1730000000"
	interopPlain  = "接口报文 hello 🔐"
	interopSealed = "v1.sm4cbc.2026-01.oKGio6SlpqeoqaqrrK2urz3cr85SuxIXtCpL33ZRa6Hh17OdeUMXtE61b4VcjPe4oJiDsw8sm3i_V3B74FnpUONFWPJBZCAyEdZk83_OUYE"
	interopCTHex  = "3ddcafce52bb1217b42a4bdf76516ba1e1d7b39d794317b44eb56f855c8cf7b8"
	interopMACHex = "a09883b30f2c9b78bf57707be059e950e34558f24164203211d664f37fce5181"
)

func mustHex(t *testing.T, s string) []byte {
	t.Helper()
	raw, err := hex.DecodeString(s)
	if err != nil {
		t.Fatalf("hex 解码 %q: %v", s, err)
	}
	return raw
}

// 验证 Go 用固定 IV 加密的结果与 sm-crypto 逐字节一致。
func TestEncryptSM4PacketFixedIVMatchesFrontend(t *testing.T) {
	key := mustHex(t, interopKey32)
	block, macKey, err := newPacketCipher(key)
	if err != nil {
		t.Fatalf("newPacketCipher: %v", err)
	}

	got, err := encryptSM4PacketWithIV(block, macKey, []byte(interopPlain), []byte(interopAAD), interopKID, mustHex(t, interopIV))
	if err != nil {
		t.Fatalf("encryptSM4PacketWithIV: %v", err)
	}
	t.Logf("Go 密文   = %s", got)
	t.Logf("前端密文 = %s", interopSealed)
	if got != interopSealed {
		t.Fatalf("Go 与 sm-crypto 密文不一致")
	}

	if mac := packetMAC(macKey, interopKID, []byte(interopAAD), mustHex(t, interopIV), mustHex(t, interopCTHex)); hex.EncodeToString(mac) != interopMACHex {
		t.Fatalf("MAC 不一致：Go %x，前端 %s", mac, interopMACHex)
	}
}
