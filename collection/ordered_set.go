package collection

import "slices"

// addOrdered 按顺序写入 values，已存在的元素保持原位置。
func (s *OrderedSet[T]) addOrdered(values []T) {
	if s.m == nil {
		s.m = make(map[T]struct{}, len(values))
	}
	for _, v := range values {
		if _, ok := s.m[v]; ok {
			continue
		}
		s.m[v] = struct{}{}
		s.order = append(s.order, v)
	}
}

// removeOrdered 删除 v 并保持其余元素的相对顺序。
func (s *OrderedSet[T]) removeOrdered(v T) {
	if _, ok := s.m[v]; !ok {
		return
	}
	delete(s.m, v)
	if i := slices.Index(s.order, v); i >= 0 {
		s.order = slices.Delete(s.order, i, i+1)
	}
}
