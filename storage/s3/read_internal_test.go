package s3

import (
	"errors"
	"testing"

	"github.com/corenca/teatool-go/storage"
)

// TestRangeHeader 固定区间到 HTTP Range 头的映射，含末尾 N 字节形态。
func TestRangeHeader(t *testing.T) {
	cases := []struct {
		name string
		r    storage.ByteRange
		want string
		bad  bool
	}{
		{"闭区间", storage.ByteRange{Start: 10, End: 20}, "bytes=10-19", false},
		{"到结尾", storage.ByteRange{Start: 10, End: -1}, "bytes=10-", false},
		{"末尾N字节", storage.ByteRange{Start: -500}, "bytes=-500", false},
		{"空区间", storage.ByteRange{Start: 10, End: 10}, "", true},
		{"倒序", storage.ByteRange{Start: 20, End: 10}, "", true},
		{"末尾与End混用", storage.ByteRange{Start: -5, End: 10}, "", true},
	}
	for _, tc := range cases {
		got, err := rangeHeader(tc.r)
		t.Logf("%s: %v → %q err=%v", tc.name, tc.r, got, err)
		if tc.bad {
			if !errors.Is(err, storage.ErrInvalidArgument) {
				t.Fatalf("%s: 期望 ErrInvalidArgument，实际 %v", tc.name, err)
			}
			continue
		}
		if err != nil || got != tc.want {
			t.Fatalf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}
