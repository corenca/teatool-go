// Package collection 提供业务开发中常用的集合与序列工具。
//
// Set 是无序集合，OrderedSet 保持首次插入顺序；两者都只支持可比较元素，
// 都不是并发安全的，需要并发访问时由调用方加锁。
//
// 两个集合类型都必须通过指针使用，不要按值拷贝结构体：Set 的值拷贝只共享
// 成员 map，Clear 等重置字段的操作不会同步到原对象；OrderedSet 的值拷贝还会
// 让成员 map 与 order 切片脱节，产生不一致状态。Clone 才返回独立副本。
// Values 返回新切片，修改返回值不会影响集合；集合运算返回新集合，不修改
// 接收者与参数。
//
// SplitBySize 与 SplitByParts 返回的子切片与源切片共享底层数组，但容量
// 裁剪到各自长度，对子切片 append 不会覆写相邻份；Dedupe 返回独立底层数组。
//
// Set 与 OrderedSet 的 nil 指针接收者会 panic；请使用 NewSet/NewOrderedSet
// 或可寻址的零值变量。Set[any] 存入不可哈希的动态值（如 []int）会在 Add 时 panic。
package collection

import "fmt"

// Set 是一个无序集合，元素只支持可比较类型。
//
// Set 零值可用，首次写入时才创建内部 map。Set 必须通过指针使用，不要按值
// 拷贝结构体；需要独立副本时使用 Clone。Set 不是并发安全的。
type Set[T comparable] struct {
	m map[T]struct{}
}

// NewSet 用 values 构造集合，重复元素只保留一次。
func NewSet[T comparable](values ...T) *Set[T] {
	s := &Set[T]{m: make(map[T]struct{}, len(values))}
	addValues(s.m, values)
	return s
}

// Add 把 values 加入集合，已存在的元素保持不变。
func (s *Set[T]) Add(values ...T) {
	if s.m == nil {
		s.m = make(map[T]struct{}, len(values))
	}
	addValues(s.m, values)
}

// Remove 从集合中删除 values，不存在的元素会被忽略。
func (s *Set[T]) Remove(values ...T) {
	for _, v := range values {
		delete(s.m, v)
	}
}

// Contains 报告集合是否包含 v。
func (s *Set[T]) Contains(v T) bool {
	_, ok := s.m[v]
	return ok
}

// Len 返回集合中的元素个数。
func (s *Set[T]) Len() int {
	return len(s.m)
}

// Clear 清空集合，清空后仍可继续使用。
func (s *Set[T]) Clear() {
	s.m = nil
}

// Values 返回集合中所有元素的新切片，顺序不保证。
func (s *Set[T]) Values() []T {
	values := make([]T, 0, len(s.m))
	for v := range s.m {
		values = append(values, v)
	}
	return values
}

// Clone 返回包含相同元素的独立集合。
func (s *Set[T]) Clone() *Set[T] {
	clone := &Set[T]{m: make(map[T]struct{}, len(s.m))}
	for v := range s.m {
		clone.m[v] = struct{}{}
	}
	return clone
}

// Union 返回 s 与 other 的并集，不修改原集合。
func (s *Set[T]) Union(other *Set[T]) *Set[T] {
	out := s.Clone()
	for v := range other.m {
		out.m[v] = struct{}{}
	}
	return out
}

// Intersect 返回 s 与 other 的交集，不修改原集合。
func (s *Set[T]) Intersect(other *Set[T]) *Set[T] {
	out := &Set[T]{m: make(map[T]struct{}, min(len(s.m), len(other.m)))}
	for v := range s.m {
		if other.Contains(v) {
			out.m[v] = struct{}{}
		}
	}
	return out
}

// Difference 返回属于 s 但不属于 other 的元素，不修改原集合。
func (s *Set[T]) Difference(other *Set[T]) *Set[T] {
	out := &Set[T]{m: make(map[T]struct{}, len(s.m))}
	for v := range s.m {
		if !other.Contains(v) {
			out.m[v] = struct{}{}
		}
	}
	return out
}

// Equal 报告两个集合的元素是否完全相同，与迭代顺序无关。
func (s *Set[T]) Equal(other *Set[T]) bool {
	if s.Len() != other.Len() {
		return false
	}
	for v := range s.m {
		if !other.Contains(v) {
			return false
		}
	}
	return true
}

// OrderedSet 是保持首次插入顺序的集合，元素只支持可比较类型。
//
// 重复 Add 不会改变元素位置；元素被删除后重新加入时排到末尾。
// OrderedSet 零值可用。与 Set 一样，OrderedSet 必须通过指针使用，按值拷贝
// 结构体会让成员 map 与 order 切片脱节，且不是并发安全的。
type OrderedSet[T comparable] struct {
	m     map[T]struct{}
	order []T
}

// NewOrderedSet 用 values 构造保序集合，重复元素保留首次出现的位置。
func NewOrderedSet[T comparable](values ...T) *OrderedSet[T] {
	s := &OrderedSet[T]{m: make(map[T]struct{}, len(values))}
	s.addOrdered(values)
	return s
}

// Add 把 values 加入集合，已存在的元素保持原位置。
func (s *OrderedSet[T]) Add(values ...T) {
	s.addOrdered(values)
}

// Remove 从集合中删除 values，不存在的元素会被忽略。
func (s *OrderedSet[T]) Remove(values ...T) {
	for _, v := range values {
		s.removeOrdered(v)
	}
}

// Contains 报告集合是否包含 v。
func (s *OrderedSet[T]) Contains(v T) bool {
	_, ok := s.m[v]
	return ok
}

// Len 返回集合中的元素个数。
func (s *OrderedSet[T]) Len() int {
	return len(s.order)
}

// Clear 清空集合，清空后仍可继续使用。
func (s *OrderedSet[T]) Clear() {
	s.m = nil
	s.order = nil
}

// Values 按插入顺序返回所有元素的新切片。
func (s *OrderedSet[T]) Values() []T {
	values := make([]T, len(s.order))
	copy(values, s.order)
	return values
}

// SplitBySize 把 s 按每份最多 size 个元素切分。
//
// 除最后一份外每份恰好 size 个；空输入返回空切片。子切片与 s 共享
// 底层数组，但容量裁剪到各自长度，对子切片 append 不会覆写相邻份。
func SplitBySize[T any](s []T, size int) ([][]T, error) {
	if size < 1 {
		return nil, fmt.Errorf("collection: 每份数量必须大于 0，得到 %d", size)
	}
	if len(s) == 0 {
		return [][]T{}, nil
	}

	parts := make([][]T, 0, (len(s)-1)/size+1)
	for start := 0; start < len(s); start += size {
		end := min(start+size, len(s))
		parts = append(parts, s[start:end:end])
	}
	return parts, nil
}

// Clone 返回包含相同元素与顺序的独立集合。
func (s *OrderedSet[T]) Clone() *OrderedSet[T] {
	clone := &OrderedSet[T]{m: make(map[T]struct{}, len(s.m))}
	clone.order = make([]T, len(s.order))
	copy(clone.order, s.order)
	for v := range s.m {
		clone.m[v] = struct{}{}
	}
	return clone
}

// Union 返回 s 与 other 的并集，顺序为 s 的顺序再接 other 的新增元素。
func (s *OrderedSet[T]) Union(other *OrderedSet[T]) *OrderedSet[T] {
	out := s.Clone()
	out.addOrdered(other.order)
	return out
}

// Intersect 返回 s 与 other 的交集，保持 s 的顺序。
func (s *OrderedSet[T]) Intersect(other *OrderedSet[T]) *OrderedSet[T] {
	out := &OrderedSet[T]{m: make(map[T]struct{}, min(len(s.m), len(other.m)))}
	for _, v := range s.order {
		if _, ok := other.m[v]; ok {
			out.m[v] = struct{}{}
			out.order = append(out.order, v)
		}
	}
	return out
}

// Difference 返回属于 s 但不属于 other 的元素，保持 s 的顺序。
func (s *OrderedSet[T]) Difference(other *OrderedSet[T]) *OrderedSet[T] {
	out := &OrderedSet[T]{m: make(map[T]struct{}, len(s.m))}
	for _, v := range s.order {
		if _, ok := other.m[v]; !ok {
			out.m[v] = struct{}{}
			out.order = append(out.order, v)
		}
	}
	return out
}

// Equal 报告两个集合的元素是否完全相同，不比较顺序。
func (s *OrderedSet[T]) Equal(other *OrderedSet[T]) bool {
	if s.Len() != other.Len() {
		return false
	}
	for v := range s.m {
		if !other.Contains(v) {
			return false
		}
	}
	return true
}

// SplitByParts 把 s 均分为 parts 份。
//
// 不能整除时余数从靠前的份开始各多 1 个，任意两份长度相差不超过 1。
// parts 大于元素个数时只产出非空份，不会生成空份。子切片与 s 共享
// 底层数组，但容量裁剪到各自长度。
func SplitByParts[T any](s []T, parts int) ([][]T, error) {
	if parts < 1 {
		return nil, fmt.Errorf("collection: 份数必须大于 0，得到 %d", parts)
	}
	if len(s) == 0 {
		return [][]T{}, nil
	}

	sizes := partSizes(len(s), min(parts, len(s)))
	out := make([][]T, 0, len(sizes))
	start := 0
	for _, size := range sizes {
		end := start + size
		out = append(out, s[start:end:end])
		start = end
	}
	return out, nil
}

// Dedupe 按首次出现顺序去重，返回新切片。
//
// 返回值使用独立底层数组，不与 s 共享；空输入返回空切片。
func Dedupe[T comparable](s []T) []T {
	out := make([]T, 0, len(s))
	seen := make(map[T]struct{}, len(s))
	for _, v := range s {
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	return out
}
