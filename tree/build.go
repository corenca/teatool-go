package tree

import (
	"fmt"
	"slices"
	"strings"
)

// Build 把扁平邻接表 values 构建成只读森林。
//
// values 中的记录按 Config.ID 取主键、按 Config.ParentID 取父键。父键在
// 输入中找不到时，默认把记录当作根；提供 Config.IsRoot 后改为严格校验。
// 输入切片本身不会被修改或重新排序，返回的节点按值保存记录。
//
// 构建步骤：先建立主键索引并查重，再按输入顺序把节点挂到父节点，然后从
// 各根做层序遍历写入深度并检测环，最后在 Config.Less 非 nil 时对每层稳定
// 排序。环检测覆盖自引用、环内节点，以及父链最终进入环的旁支节点。
//
// 返回的森林不为 nil；参数或数据有问题时返回 (nil, err)，err 会包装
// ErrInvalidConfig、ErrDuplicateID、ErrMissingParent 或 ErrCycle 之一，
// 消息中带有输入下标、冲突主键或环路径。空 values 返回空森林而不是错误。
func Build[T any, K comparable](values []T, config Config[T, K]) (*Forest[T, K], error) {
	if config.ID == nil || config.ParentID == nil {
		return nil, ErrInvalidConfig
	}

	f := &Forest[T, K]{
		nodes: make([]Node[T], len(values)),
		index: make(map[K]*Node[T], len(values)),
	}
	ids := make([]K, len(values))

	for i := range values {
		id := config.ID(values[i])
		if _, exists := f.index[id]; exists {
			return nil, fmt.Errorf("%w: %v at index %d", ErrDuplicateID, id, i)
		}
		node := &f.nodes[i]
		node.Value = values[i]
		node.Children = make([]*Node[T], 0)
		f.index[id] = node
		ids[i] = id
	}

	for i := range values {
		node := &f.nodes[i]
		if config.IsRoot != nil && config.IsRoot(values[i]) {
			f.roots = append(f.roots, node)
			continue
		}

		parentID := config.ParentID(values[i])
		if parentID == ids[i] {
			return nil, fmt.Errorf("%w: %v -> %v", ErrCycle, ids[i], parentID)
		}
		parent, ok := f.index[parentID]
		if !ok {
			if config.IsRoot != nil {
				return nil, fmt.Errorf("%w: parent %v of id %v at index %d",
					ErrMissingParent, parentID, ids[i], i)
			}
			f.roots = append(f.roots, node)
			continue
		}
		node.parent = parent
		parent.Children = append(parent.Children, node)
	}

	if err := f.computeDepth(config.ID); err != nil {
		return nil, err
	}
	if config.Less != nil {
		sortSiblings(f.roots, config.Less)
		for i := range f.nodes {
			sortSiblings(f.nodes[i].Children, config.Less)
		}
	}
	return f, nil
}

// sortSiblings 用稳定排序按 less 排列同一层的节点。
//
// 先判断 a<b、再判断 b<a，两者都不成立时视为相等，保持输入顺序；less 必须是
// 一致的严格全序，否则排序结果未定义。
func sortSiblings[T any](nodes []*Node[T], less func(a, b T) bool) {
	slices.SortStableFunc(nodes, func(a, b *Node[T]) int {
		switch {
		case less(a.Value, b.Value):
			return -1
		case less(b.Value, a.Value):
			return 1
		default:
			return 0
		}
	})
}

// computeDepth 从各根做层序遍历，写入每个节点的深度，并确认所有节点都可达。
//
// 每个节点只有一个父节点，因此未被访问到的节点必然沿父指针进入环；此时调用
// cycleError 生成带环路径的错误。
func (f *Forest[T, K]) computeDepth(id func(T) K) error {
	queue := make([]*Node[T], 0, len(f.roots))
	for _, root := range f.roots {
		root.depth = 0
		queue = append(queue, root)
	}
	for head := 0; head < len(queue); head++ {
		node := queue[head]
		for _, child := range node.Children {
			child.depth = node.depth + 1
			queue = append(queue, child)
		}
	}
	if len(queue) == len(f.nodes) {
		return nil
	}
	return f.cycleError(queue, id)
}

// cycleError 从任一未访问节点出发，沿父指针找到环并返回错误。
//
// visited 是从各根可达的节点。未访问节点的父链一定不会走到 nil（否则它会
// 成为根并被访问），所以循环必然在 O(n) 步内遇到本次路径上的重复节点。
func (f *Forest[T, K]) cycleError(visited []*Node[T], id func(T) K) error {
	reached := make(map[*Node[T]]struct{}, len(visited))
	for _, node := range visited {
		reached[node] = struct{}{}
	}
	var start *Node[T]
	for i := range f.nodes {
		if _, ok := reached[&f.nodes[i]]; !ok {
			start = &f.nodes[i]
			break
		}
	}
	if start == nil {
		return ErrCycle
	}

	// positions 记录本次路径上每个节点的位置，重复出现即找到环入口。
	positions := make(map[*Node[T]]int)
	path := make([]*Node[T], 0, 8)
	for node := start; node != nil; node = node.parent {
		if pos, ok := positions[node]; ok {
			chain := make([]string, 0, len(path)-pos+1)
			for _, n := range path[pos:] {
				chain = append(chain, fmt.Sprint(id(n.Value)))
			}
			chain = append(chain, fmt.Sprint(id(node.Value)))
			return fmt.Errorf("%w: %s", ErrCycle, strings.Join(chain, " -> "))
		}
		positions[node] = len(path)
		path = append(path, node)
	}
	return ErrCycle
}
