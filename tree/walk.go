package tree

import "iter"

// Walk 返回按先序（父在子前）遍历整个森林的迭代器。
//
// 根按 Roots 的顺序依次展开，兄弟节点按构建顺序或 Less 排序结果展开；
// 同一森林多次遍历顺序一致。遍历使用显式栈，深链不会导致栈溢出。
// 调用方在循环里 break 时迭代立即停止，不会继续访问剩余节点。
func (f *Forest[T, K]) Walk() iter.Seq[*Node[T]] {
	return walkPreorder(f.roots)
}

// WalkLevel 返回按层序（广度优先）遍历整个森林的迭代器。
//
// 深度小的节点先产出，同层节点按父节点的遍历顺序展开。与 Walk 一样，
// 顺序确定、可用 break 提前结束，并且不递归。
func (f *Forest[T, K]) WalkLevel() iter.Seq[*Node[T]] {
	return func(yield func(*Node[T]) bool) {
		queue := make([]*Node[T], 0, len(f.roots))
		queue = append(queue, f.roots...)
		for head := 0; head < len(queue); head++ {
			node := queue[head]
			if !yield(node) {
				return
			}
			queue = append(queue, node.Children...)
		}
	}
}

// Descendants 返回该节点的所有后代，按先序、不包含自身。
//
// 与 Walk 相同，使用显式栈并支持 break。叶子节点不产出任何值。
func (n *Node[T]) Descendants() iter.Seq[*Node[T]] {
	return walkPreorder(n.Children)
}

// Ancestors 返回该节点的祖先，不包含自身，顺序为根在前、父在后。
//
// 根节点不产出任何值。因此 Path() 恒等于 Ancestors() 的结果再接上自身，
// 两个方法不会给出互相矛盾的顺序。
func (n *Node[T]) Ancestors() iter.Seq[*Node[T]] {
	return func(yield func(*Node[T]) bool) {
		chain := make([]*Node[T], 0, n.depth+1)
		for node := n; node != nil; node = node.parent {
			chain = append(chain, node)
		}
		for i := len(chain) - 1; i >= 1; i-- {
			if !yield(chain[i]) {
				return
			}
		}
	}
}

// Path 返回从根到该节点的完整链路，包含根与自身。
//
// 根节点的 Path 只有一个元素。返回值是新切片，修改它不会影响森林。
func (n *Node[T]) Path() []*Node[T] {
	path := make([]*Node[T], n.depth+1)
	for node := n; node != nil; node = node.parent {
		path[node.depth] = node
	}
	return path
}

// Flatten 按 Walk 的先序顺序返回所有节点的业务载荷。
//
// 返回的是新切片，长度等于 Len；修改切片元素不影响森林。
func (f *Forest[T, K]) Flatten() []T {
	values := make([]T, 0, len(f.nodes))
	for node := range f.Walk() {
		values = append(values, node.Value)
	}
	return values
}

// walkPreorder 返回从 roots 开始的先序遍历迭代器。
//
// 用显式栈代替递归；倒序压栈保证子节点按原顺序产出。
func walkPreorder[T any](roots []*Node[T]) iter.Seq[*Node[T]] {
	return func(yield func(*Node[T]) bool) {
		if len(roots) == 0 {
			return
		}
		stack := make([]*Node[T], 0, len(roots))
		for i := len(roots) - 1; i >= 0; i-- {
			stack = append(stack, roots[i])
		}
		for len(stack) > 0 {
			node := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			if !yield(node) {
				return
			}
			for i := len(node.Children) - 1; i >= 0; i-- {
				stack = append(stack, node.Children[i])
			}
		}
	}
}
