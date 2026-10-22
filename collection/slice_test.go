package collection_test

import (
	"slices"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/collection"
)

func TestSplitBySize(t *testing.T) {
	s := []int{1, 2, 3, 4, 5}
	tests := []struct {
		name string
		size int
		want [][]int
	}{
		{"恰好一份", 5, [][]int{{1, 2, 3, 4, 5}}},
		{"不整除", 3, [][]int{{1, 2, 3}, {4, 5}}},
		{"大于长度", 10, [][]int{{1, 2, 3, 4, 5}}},
		{"每份一个", 1, [][]int{{1}, {2}, {3}, {4}, {5}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := collection.SplitBySize(s, tt.size)
			if err != nil {
				t.Fatalf("SplitBySize() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("份数 = %d, want %d", len(got), len(tt.want))
			}
			for i := range tt.want {
				if !slices.Equal(got[i], tt.want[i]) {
					t.Fatalf("第 %d 份 = %v, want %v", i, got[i], tt.want[i])
				}
			}
		})
	}
}

func TestSplitBySizeEmpty(t *testing.T) {
	got, err := collection.SplitBySize([]int(nil), 2)
	if err != nil {
		t.Fatalf("SplitBySize() error = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("空输入应返回非 nil 空切片，得到 %#v", got)
	}
}

func TestSplitBySizeInvalidSize(t *testing.T) {
	for _, size := range []int{0, -1} {
		got, err := collection.SplitBySize([]int{1}, size)
		if err == nil {
			t.Fatalf("size=%d 应返回错误", size)
		}
		if !strings.Contains(err.Error(), "collection:") {
			t.Fatalf("错误信息缺少包前缀: %v", err)
		}
		if got != nil {
			t.Fatalf("参数错误应返回 nil，得到 %#v", got)
		}
	}
}

func TestSplitBySizeSubslices(t *testing.T) {
	src := []int{1, 2, 3, 4}
	parts, err := collection.SplitBySize(src, 2)
	if err != nil {
		t.Fatalf("SplitBySize() error = %v", err)
	}
	if cap(parts[0]) != 2 {
		t.Fatalf("cap(parts[0]) = %d, want 2", cap(parts[0]))
	}
	parts[0][0] = 42
	if src[0] != 42 {
		t.Fatal("子切片应共享源底层数组")
	}
	parts[0] = append(parts[0], 99)
	if parts[1][0] != 3 {
		t.Fatalf("append 覆写了相邻份: %v", parts[1])
	}
}

func TestSplitByParts(t *testing.T) {
	tests := []struct {
		name  string
		total int
		parts int
		want  []int // 每份长度
	}{
		{"十元素三分", 10, 3, []int{4, 3, 3}},
		{"五元素两分", 5, 2, []int{3, 2}},
		{"份数等于长度", 5, 5, []int{1, 1, 1, 1, 1}},
		{"份数大于长度", 3, 5, []int{1, 1, 1}},
		{"一份", 5, 1, []int{5}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := make([]int, tt.total)
			for i := range s {
				s[i] = i
			}
			got, err := collection.SplitByParts(s, tt.parts)
			if err != nil {
				t.Fatalf("SplitByParts() error = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("份数 = %d, want %d", len(got), len(tt.want))
			}
			start := 0
			for i, wantLen := range tt.want {
				if len(got[i]) != wantLen {
					t.Fatalf("第 %d 份长度 = %d, want %d", i, len(got[i]), wantLen)
				}
				if !slices.Equal(got[i], s[start:start+wantLen]) {
					t.Fatalf("第 %d 份内容 = %v", i, got[i])
				}
				start += wantLen
			}
		})
	}
}

func TestSplitByPartsEmpty(t *testing.T) {
	got, err := collection.SplitByParts([]string(nil), 3)
	if err != nil {
		t.Fatalf("SplitByParts() error = %v", err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("空输入应返回非 nil 空切片，得到 %#v", got)
	}
}

func TestSplitByPartsInvalidParts(t *testing.T) {
	for _, parts := range []int{0, -1} {
		got, err := collection.SplitByParts([]int{1}, parts)
		if err == nil {
			t.Fatalf("parts=%d 应返回错误", parts)
		}
		if !strings.Contains(err.Error(), "collection:") {
			t.Fatalf("错误信息缺少包前缀: %v", err)
		}
		if got != nil {
			t.Fatalf("参数错误应返回 nil，得到 %#v", got)
		}
	}
}

func TestSplitByPartsSubslices(t *testing.T) {
	src := []int{1, 2, 3}
	parts, err := collection.SplitByParts(src, 2)
	if err != nil {
		t.Fatalf("SplitByParts() error = %v", err)
	}
	if cap(parts[0]) != len(parts[0]) {
		t.Fatalf("cap(parts[0]) = %d, want %d", cap(parts[0]), len(parts[0]))
	}
	parts[0] = append(parts[0], 99)
	if parts[1][0] != 3 {
		t.Fatalf("append 覆写了相邻份: %v", parts[1])
	}
}

func TestDedupeKeepsFirstOccurrenceOrder(t *testing.T) {
	got := collection.Dedupe([]int{3, 1, 3, 2, 1})
	if !slices.Equal(got, []int{3, 1, 2}) {
		t.Fatalf("Dedupe() = %v, want [3 1 2]", got)
	}
}

func TestDedupeEmpty(t *testing.T) {
	got := collection.Dedupe([]string(nil))
	if got == nil || len(got) != 0 {
		t.Fatalf("空输入应返回非 nil 空切片，得到 %#v", got)
	}
}

func TestDedupeReturnsIndependentSlice(t *testing.T) {
	src := []int{1, 1, 2}
	got := collection.Dedupe(src)
	got[0] = 99
	if src[0] != 1 {
		t.Fatal("Dedupe 结果与源共享底层数组")
	}
}

func TestDedupeStruct(t *testing.T) {
	type point struct{ X, Y int }
	got := collection.Dedupe([]point{{1, 2}, {1, 2}, {3, 4}})
	if !slices.Equal(got, []point{{1, 2}, {3, 4}}) {
		t.Fatalf("Dedupe() = %v", got)
	}
}

func TestSplitBySizeMaxInt(t *testing.T) {
	size := int(^uint(0) >> 1)
	got, err := collection.SplitBySize([]int{1, 2}, size)
	if err != nil {
		t.Fatalf("SplitBySize() error = %v", err)
	}
	if len(got) != 1 || !slices.Equal(got[0], []int{1, 2}) {
		t.Fatalf("SplitBySize() = %v, want [[1 2]]", got)
	}
}
