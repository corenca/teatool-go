package collection_test

import (
	"slices"
	"testing"

	"github.com/corenca/teatool-go/collection"
)

func TestOrderedSetKeepsInsertionOrder(t *testing.T) {
	s := collection.NewOrderedSet("c", "a", "b", "a")
	if got := s.Values(); !slices.Equal(got, []string{"c", "a", "b"}) {
		t.Fatalf("Values() = %v, want [c a b]", got)
	}
	if s.Len() != 3 {
		t.Fatalf("Len() = %d, want 3", s.Len())
	}
}

func TestOrderedSetAddExistingDoesNotMove(t *testing.T) {
	s := collection.NewOrderedSet(1, 2, 3)
	s.Add(1, 3)
	if got := s.Values(); !slices.Equal(got, []int{1, 2, 3}) {
		t.Fatalf("Values() = %v, want [1 2 3]", got)
	}
}

func TestOrderedSetRemoveAndReAddGoesToEnd(t *testing.T) {
	s := collection.NewOrderedSet(1, 2, 3)
	s.Remove(2)
	s.Add(2)
	if got := s.Values(); !slices.Equal(got, []int{1, 3, 2}) {
		t.Fatalf("Values() = %v, want [1 3 2]", got)
	}
}

func TestOrderedSetZeroValue(t *testing.T) {
	var s collection.OrderedSet[string]
	s.Add("a")
	if !s.Contains("a") || s.Len() != 1 {
		t.Fatal("零值 OrderedSet 不能写入")
	}
}

func TestOrderedSetValuesReturnsCopy(t *testing.T) {
	s := collection.NewOrderedSet("a", "b")
	values := s.Values()
	values[0] = "mutated"
	if s.Contains("mutated") || !s.Contains("a") {
		t.Fatal("修改 Values() 的返回值影响了集合")
	}
}

func TestOrderedSetUnionOrder(t *testing.T) {
	a := collection.NewOrderedSet(3, 1, 2)
	b := collection.NewOrderedSet(2, 4, 1, 5)

	got := a.Union(b).Values()
	if !slices.Equal(got, []int{3, 1, 2, 4, 5}) {
		t.Fatalf("Union 顺序 = %v, want [3 1 2 4 5]", got)
	}
	if !slices.Equal(a.Values(), []int{3, 1, 2}) || !slices.Equal(b.Values(), []int{2, 4, 1, 5}) {
		t.Fatal("Union 修改了原集合")
	}
}

func TestOrderedSetIntersectAndDifferenceKeepReceiverOrder(t *testing.T) {
	a := collection.NewOrderedSet(3, 1, 2, 4)
	b := collection.NewOrderedSet(2, 3)

	if got := a.Intersect(b).Values(); !slices.Equal(got, []int{3, 2}) {
		t.Fatalf("Intersect 顺序 = %v, want [3 2]", got)
	}
	if got := a.Difference(b).Values(); !slices.Equal(got, []int{1, 4}) {
		t.Fatalf("Difference 顺序 = %v, want [1 4]", got)
	}
	if !slices.Equal(a.Values(), []int{3, 1, 2, 4}) {
		t.Fatal("Intersect/Difference 修改了接收者")
	}
}

func TestOrderedSetEqualIgnoresOrder(t *testing.T) {
	a := collection.NewOrderedSet(1, 2, 3)
	b := collection.NewOrderedSet(3, 2, 1)
	if !a.Equal(b) {
		t.Fatal("Equal 应忽略顺序")
	}
	if a.Equal(collection.NewOrderedSet(1, 2)) {
		t.Fatal("Equal 对不同长度的集合返回 true")
	}
}

func TestOrderedSetCloneIsIndependent(t *testing.T) {
	s := collection.NewOrderedSet(1, 2)
	clone := s.Clone()
	clone.Add(3)
	clone.Remove(1)
	if !slices.Equal(s.Values(), []int{1, 2}) || !slices.Equal(clone.Values(), []int{2, 3}) {
		t.Fatal("Clone 与原集合共享状态或顺序")
	}
}
