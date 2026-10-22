package collection

// addValues 把 values 全部写入 m，重复元素只保留一次。
func addValues[T comparable](m map[T]struct{}, values []T) {
	for _, v := range values {
		m[v] = struct{}{}
	}
}
