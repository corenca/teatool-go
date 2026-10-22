package collection

// partSizes 把 total 个元素分成 parts 份，余数从靠前的份开始各多 1。
// 调用方保证 parts >= 1 且 parts <= total。
func partSizes(total, parts int) []int {
	base := total / parts
	rem := total % parts
	sizes := make([]int, parts)
	for i := range sizes {
		sizes[i] = base
		if i < rem {
			sizes[i]++
		}
	}
	return sizes
}
