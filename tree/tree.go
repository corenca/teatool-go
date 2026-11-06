// Package tree 把扁平邻接表构建成只读的层级森林，并提供查找、遍历与路径查询。
//
// 业务库里的层级数据通常是 (id, parent_id, sort) 这样的扁平行，本包负责
// 一次建成树，之后所有查询都走索引：Find 为 O(1)，Path/Ancestors 为
// O(深度)，遍历为 O(n)。典型用途是菜单树、部门树、分类树与权限树。
//
// # 数据模型
//
// Build 接收 []T 与 Config[T, K]，返回 *Forest[T, K]。节点 Node[T] 只承载
// 业务载荷 T：ID 与父 ID 本来就在 T 里，不由节点重复保存。结构信息
// （父指针、深度）由包内部维护，不导出。
//
// # 只读
//
// 森林构建完成后不提供增删改入口。Node 的 Value 与 Children 是只读契约：
// 调用方可以读、可以序列化，但不得修改其中的字段、追加子节点或替换切片。
// 需要不同的数据就重新 Build。遵守该契约时，多个 goroutine 并发读取是安全的：
// 包内没有惰性初始化，也没有共享的可写缓存。
//
// # 顺序
//
// 根与兄弟节点的顺序只由输入顺序决定；Config.Less 非 nil 时对每一层做稳定
// 排序。构建结果与 map 迭代顺序无关，同一输入重复构建得到相同顺序。
//
// # 根与孤儿
//
// 默认规则：父键在输入中不存在时，该节点成为根。根节点本来就要用 0、"" 或
// nil 之类的哨兵值表示"没有父节点"，因此这条宽容规则无法与数据错误区分。
// 需要严格校验时提供 Config.IsRoot：非根节点父键缺失会返回 ErrMissingParent。
//
// # 错误
//
// 构建期错误使用哨兵，可用 errors.Is 判断：ErrInvalidConfig（回调缺失）、
// ErrDuplicateID（主键重复）、ErrMissingParent（严格模式下的孤儿）、
// ErrCycle（自引用或父链成环）。构建失败返回 (nil, err)，不会产生部分可用的
// 森林。
//
// # 复杂度
//
// Build 为 O(n log n)（有 Less）或 O(n)（无 Less）；Find 为 O(1)；Path 与
// Ancestors 为 O(深度)；Walk、WalkLevel 与 Flatten 为 O(n)。所有遍历与环
// 检测都使用显式栈/队列，深链不会导致栈溢出。
package tree

import (
	"errors"
	"slices"
)

// 构建期可能返回的哨兵错误。
//
// 调用方应使用 errors.Is 判断；具体冲突值（输入下标、ID、环路径）在错误
// 消息里，便于定位是哪一行数据有问题。
var (
	// ErrInvalidConfig 表示 Config.ID 或 Config.ParentID 为 nil。
	ErrInvalidConfig = errors.New("tree: ID 或 ParentID 为 nil")
	// ErrDuplicateID 表示多个输入值返回了相同的主键。
	ErrDuplicateID = errors.New("tree: 重复的节点 ID")
	// ErrMissingParent 表示严格模式下非根节点的父键在输入中不存在。
	ErrMissingParent = errors.New("tree: 父节点不存在")
	// ErrCycle 表示节点自引用，或沿父指针形成了环。
	ErrCycle = errors.New("tree: 存在环")
)

// Config 声明如何从业务记录 T 中取主键、父键，以及可选的根判定与排序规则。
//
// ID 与 ParentID 是必填项，Build 会为每个输入值各调用一次。IsRoot 与 Less
// 可选。Config 只在 Build 期间使用，Forest 不保留它，因此闭包捕获的对象在
// 构建结束后可以被回收。
type Config[T any, K comparable] struct {
	// ID 返回业务记录的主键；同一批输入中必须唯一。
	ID func(T) K

	// ParentID 返回业务记录的父键。根节点通常返回 0、"" 或 nil 等哨兵值；
	// 该值在输入中找不到对应节点时，默认把当前节点当作根（见 IsRoot）。
	ParentID func(T) K

	// IsRoot 可选，用于严格校验与显式根标记。提供后：
	//   - 返回 true 的记录一定是根，即使它的父键在输入中存在（优先于父键）；
	//   - 返回 false 且父键在输入中找不到时，Build 返回 ErrMissingParent。
	// 不提供时，父键找不到的记录一律视为根。
	IsRoot func(T) bool

	// Less 可选，定义同一层内两个业务记录的先后。非 nil 时 Build 用稳定
	// 排序处理根列表与每层子节点，Less 必须是一致且不可逆的严格全序
	// （类似 slices.SortFunc 的比较函数返回 -1 的条件）。
	Less func(a, b T) bool
}

// Forest 是一次构建得到的只读森林：若干棵互不相交的树，加上按主键的索引。
//
// Forest 只能通过 Build 获得，零值不可用；nil *Forest 上调用方法会 panic。
// 构建完成后没有任何写入口，遵守 Node 的只读契约时可以并发读取。
type Forest[T any, K comparable] struct {
	// nodes 是所有节点连续存放的底层数组，节点指针指向其中元素。
	nodes []Node[T]
	// roots 按构建顺序（或 Less 排序后顺序）保存各棵树的根。
	roots []*Node[T]
	// index 以业务主键索引节点，保证 Find 为 O(1)。
	index map[K]*Node[T]
}

// Node 是森林中的一个节点，承载业务载荷 T 及其子节点。
//
// Value 保存传入 Build 的原始记录，Children 按构建/排序顺序保存直接子节点。
// 两者都是只读契约：构建后不得修改 Value 内的可变字段，也不得增删 Children
// 或替换切片；需要变更请重新 Build。
//
// IsRoot 为 true 时 Parent 返回 nil；叶子的 Children 是空切片而不是 nil。
type Node[T any] struct {
	// Value 是构建时传入的业务记录，不做任何复制或改写。
	Value T
	// Children 是直接子节点，顺序为输入顺序或 Less 的稳定排序结果。
	Children []*Node[T]

	// parent 指向父节点，根为 nil；depth 为根到该节点的边数。
	parent *Node[T]
	depth  int
}

// Roots 返回各棵树的根节点，顺序与构建顺序（或 Less 排序结果）一致。
//
// 返回的是新切片，修改它不会影响森林；元素本身仍是森林内部的节点指针。
func (f *Forest[T, K]) Roots() []*Node[T] {
	return slices.Clone(f.roots)
}

// Find 按业务主键查找节点，第二个返回值表示是否存在。
//
// 查找为 O(1)。返回的节点指针在森林存活期间保持有效；调用方不得修改其内容。
func (f *Forest[T, K]) Find(id K) (*Node[T], bool) {
	node, ok := f.index[id]
	return node, ok
}

// Len 返回森林中的节点总数，包括所有根。
func (f *Forest[T, K]) Len() int {
	return len(f.nodes)
}

// Parent 返回父节点；根节点返回 nil。
func (n *Node[T]) Parent() *Node[T] {
	return n.parent
}

// Depth 返回根到该节点的边数，根为 0。
func (n *Node[T]) Depth() int {
	return n.depth
}

// IsRoot 报告该节点是否为某棵树的根（没有父节点）。
func (n *Node[T]) IsRoot() bool {
	return n.parent == nil
}

// IsLeaf 报告该节点是否没有子节点。
func (n *Node[T]) IsLeaf() bool {
	return len(n.Children) == 0
}
