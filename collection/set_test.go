package collection_test

import (
	"slices"
	"testing"

	"github.com/corenca/teatool-go/collection"
)

func TestSetBasics(t *testing.T) {
	s := collection.NewSet("b", "a", "b", "c")
	if s.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", s.Len())
	}
	s.Add("d", "d")
	if !s.Contains("d") || s.Len() != 4 {
		t.Fatalf("Add 后 Len() = %d, want 4", s.Len())
	}
	s.Remove("a", "missing")
	if s.Contains("a") || s.Len() != 3 {
		t.Fatalf("Remove 后 Len() = %d, want 3", s.Len())
	}
	if !s.Contains("b") || !s.Contains("c") || !s.Contains("d") {
		t.Fatal("Remove 误删了其他元素")
	}
	s.Clear()
	if s.Len() != 0 || len(s.Values()) != 0 {
		t.Fatalf("Clear 后 Len() = %d", s.Len())
	}
}

func TestSetZeroValue(t *testing.T) {
	var s collection.Set[int]
	if s.Len() != 0 {
		t.Fatalf("零值 Len() = %d, want 0", s.Len())
	}
	if s.Values() == nil {
		t.Fatal("空集合的 Values() 应为非 nil 空切片")
	}
	s.Add(1)
	if !s.Contains(1) {
		t.Fatal("零值 Set 不能写入")
	}
}

func TestSetValuesReturnsCopy(t *testing.T) {
	s := collection.NewSet("a", "b")
	values := s.Values()
	slices.Sort(values)
	if !slices.Equal(values, []string{"a", "b"}) {
		t.Fatalf("Values() = %v, want [a b]", values)
	}
	values[0] = "mutated"
	if s.Contains("mutated") || !s.Contains("a") {
		t.Fatal("修改 Values() 的返回值影响了集合")
	}
}

func TestSetCloneIsIndependent(t *testing.T) {
	s := collection.NewSet(1, 2)
	clone := s.Clone()
	clone.Add(3)
	clone.Remove(1)
	if s.Contains(3) || !s.Contains(1) || s.Len() != 2 {
		t.Fatal("Clone 与原集合共享状态")
	}
}

func TestSetOperations(t *testing.T) {
	a := collection.NewSet(1, 2, 3)
	b := collection.NewSet(3, 4)

	assertIntSet(t, a.Union(b), 1, 2, 3, 4)
	assertIntSet(t, a.Intersect(b), 3)
	assertIntSet(t, a.Difference(b), 1, 2)
	assertIntSet(t, a, 1, 2, 3)
	assertIntSet(t, b, 3, 4)

	if !a.Equal(collection.NewSet(3, 2, 1)) {
		t.Fatal("Equal 应忽略顺序")
	}
	if a.Equal(collection.NewSet(1, 2)) {
		t.Fatal("Equal 对不同长度的集合返回 true")
	}
	if !collection.NewSet[int]().Equal(collection.NewSet[int]()) {
		t.Fatal("空集合应相等")
	}
}

func assertIntSet(t *testing.T, s *collection.Set[int], want ...int) {
	t.Helper()
	if s.Len() != len(want) {
		t.Fatalf("Len() = %d, want %d", s.Len(), len(want))
	}
	for _, v := range want {
		if !s.Contains(v) {
			t.Fatalf("缺少元素 %d", v)
		}
	}
}
