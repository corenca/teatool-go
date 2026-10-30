package gm_test

import (
	"encoding/hex"
	"testing"

	"github.com/corenca/teatool-go/gm"
)

// 标准向量来自 GB/T 32905-2016。
func TestHashSM3(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空串", "", "1ab21d8355cfa17f8e61194831e81a8f22bec8c728fefb747ed035eb5082aa2b"},
		{"abc", "abc", "66c7f0f462eeedd9d1f2d46bdc10e4e24167c4875cf2f7a2297da02b8f4ba8e0"},
		{"中文", "国密", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := gm.HashSM3([]byte(tc.in))
			t.Logf("SM3(%q) = %x", tc.in, got)
			if tc.want == "" {
				// 中文只校验摘要长度，不在此处硬编码未经验证的向量。
				if len(got) != 32 {
					t.Fatalf("长度 = %d，期望 32", len(got))
				}
				return
			}
			if got := hex.EncodeToString(got); got != tc.want {
				t.Fatalf("SM3(%q) = %s，期望 %s", tc.in, got, tc.want)
			}
		})
	}
}
