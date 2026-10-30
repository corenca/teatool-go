package gm_test

import (
	"encoding/hex"
	"testing"

	"github.com/emmansun/gmsm/sm2"

	"github.com/corenca/teatool-go/gm"
)

// 固定向量由 sm-crypto 0.5.8 生成，证明前后端字节级一致。
const (
	interopKey32  = "000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f"
	interopKID    = "2026-01"
	interopAAD    = "POST|/api/login|1730000000"
	interopPlain  = "接口报文 hello 🔐"
	interopSealed = "v1.sm4cbc.2026-01.oKGio6SlpqeoqaqrrK2urz3cr85SuxIXtCpL33ZRa6Hh17OdeUMXtE61b4VcjPe4oJiDsw8sm3i_V3B74FnpUONFWPJBZCAyEdZk83_OUYE"

	interopSM2Priv  = "1833914b47cda19bf5b4cbe7221743f6518cf3df392ccb337703b2c72d8ba392"
	interopSM2Plain = "SM2 互操作 🔐"
	interopSM2NoPfx = "de9701a4890fda8a7bda24005dd36887fb80949e988fa419f0f4e118a5acda8e6b0268e43862db8263194315b4c1a5045cb414412a1cea8c80764880c03146543b08660cd76708905bb1fbeeba532d408fa25473268b7ecf552bcd3dcca15d286abf990c6bc6989757f2a9e40dbfa28de9d7"
)

func TestInteropDecryptSM4PacketFromFrontend(t *testing.T) {
	key, err := hex.DecodeString(interopKey32)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	got, err := gm.DecryptSM4Packet(key, interopSealed, []byte(interopAAD))
	if err != nil {
		t.Fatalf("DecryptSM4Packet: %v", err)
	}
	t.Logf("前端密文解密得到 %q", got)
	if string(got) != interopPlain {
		t.Fatalf("明文 = %q，期望 %q", got, interopPlain)
	}
}

func TestInteropDecryptSM2FromFrontend(t *testing.T) {
	key, err := hex.DecodeString(interopSM2Priv)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	priv, err := sm2.NewPrivateKey(key)
	if err != nil {
		t.Fatalf("NewPrivateKey: %v", err)
	}

	// sm-crypto 的输出不带 04，按契约由前端补上后再交给后端。
	raw, err := hex.DecodeString("04" + interopSM2NoPfx)
	if err != nil {
		t.Fatalf("hex: %v", err)
	}
	got, err := gm.DecryptSM2(priv, raw, gm.SM2Options{})
	if err != nil {
		t.Fatalf("DecryptSM2: %v", err)
	}
	t.Logf("前端 SM2 密文解密得到 %q", got)
	if string(got) != interopSM2Plain {
		t.Fatalf("明文 = %q，期望 %q", got, interopSM2Plain)
	}
}
