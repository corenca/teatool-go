# tree 使用说明

把数据库里的扁平层级数据（`id`、`parent_id`、`sort`）一次构建成只读森林，
之后直接按 ID 查找、按层或按深度遍历、取面包屑。适用于菜单树、部门树、
分类树、权限树。

```bash
go get github.com/corenca/teatool-go
```

## 快速上手

```go
package main

import (
	"fmt"
	"strings"

	"github.com/corenca/teatool-go/tree"
)

// Menu 对应数据库里的一行菜单。
type Menu struct {
	ID       int64
	ParentID int64
	Name     string
	Sort     int
}

func main() {
	rows := []Menu{
		{ID: 1, ParentID: 0, Name: "系统管理", Sort: 1},
		{ID: 2, ParentID: 1, Name: "用户管理", Sort: 2},
		{ID: 3, ParentID: 1, Name: "角色管理", Sort: 1},
		{ID: 4, ParentID: 2, Name: "重置密码", Sort: 1},
	}

	forest, err := tree.Build(rows, tree.Config[Menu, int64]{
		ID:       func(m Menu) int64 { return m.ID },
		ParentID: func(m Menu) int64 { return m.ParentID },
		Less:     func(a, b Menu) bool { return a.Sort < b.Sort },
	})
	if err != nil {
		panic(err)
	}

	for node := range forest.Walk() {
		fmt.Println(strings.Repeat("  ", node.Depth()) + node.Value.Name)
	}

	user, _ := forest.Find(2)
	fmt.Println(user.Path()[0].Value.Name, user.Depth()) // 系统管理 1
}
```

输出：

```text
系统管理
  角色管理
  用户管理
    重置密码
系统管理 1
```

## 数据模型

`Build` 返回 `*tree.Forest[T, K]`，节点是 `*tree.Node[T]`：

- `Node.Value` 就是传入的那条记录，包不做复制或改写。
- `Node.Children` 是按构建顺序或排序结果排列的直接子节点。
- 主键与父键不重复保存在节点上：它们本来就在 `Value` 里，`Config` 负责取。
- `Value` 与 `Children` 是**只读契约**，构建后不要修改。

一个输入里可以有多棵树（多个根），所以类型叫 `Forest`；`Roots()` 返回各棵
树的根。

## Config

| 字段 | 必填 | 含义 |
| --- | --- | --- |
| `ID` | 是 | 从记录取主键。同一批输入中必须唯一，否则 `ErrDuplicateID`。 |
| `ParentID` | 是 | 从记录取父键。根节点通常返回 `0`、`""` 或 `nil`。 |
| `IsRoot` | 否 | 显式根判定；提供后启用严格校验，见“根与孤儿”。 |
| `Less` | 否 | 同层排序规则；提供后用稳定排序处理根与每层子节点。 |

`ID`、`ParentID`、`IsRoot` 对每条记录最多各调用一次，`Less` 只在排序时调用。
`Config` 不会被森林保留，闭包捕获的对象在构建结束后可以回收。

`Less` 必须是严格全序（类似 `slices.SortFunc` 的比较函数），并且对所有记录
稳定；不满足时排序结果没有保证。

## 根与孤儿

默认规则：**父键在输入中找不到时，记录成为根**。

这是菜单/部门表的常态：根节点的 `parent_id` 是 `0`，而集合里根本没有 ID 为
`0` 的记录。代价是真正的数据错误（父节点漏查、被删）也会静默变成根。

需要严格校验时提供 `IsRoot`：

```go
config := tree.Config[Menu, int64]{
	ID:       func(m Menu) int64 { return m.ID },
	ParentID: func(m Menu) int64 { return m.ParentID },
	IsRoot:   func(m Menu) bool { return m.ParentID == 0 },
}
```

提供后：

- `IsRoot` 返回 true 的记录一定是根，即使父键在输入中存在也**不会**挂到父节点下；
- `IsRoot` 返回 false 且父键找不到时，`Build` 返回 `ErrMissingParent`。

注意：如果数据里真的存在 ID 为 `0` 的节点，默认规则会让所有 `parent_id = 0`
的根节点都挂到它下面。这种数据请用 `IsRoot` 明确根。

## 查询与遍历

| 方法 | 说明 | 复杂度 |
| --- | --- | --- |
| `forest.Roots()` | 各棵树的根（新切片） | O(根数) |
| `forest.Find(id)` | 按主键查找，返回 `(node, ok)` | O(1) |
| `forest.Len()` | 节点总数 | O(1) |
| `forest.Walk()` | 先序迭代器（父在子前） | O(n) |
| `forest.WalkLevel()` | 层序迭代器（广度优先） | O(n) |
| `forest.Flatten()` | 按先序返回 `[]T` | O(n) |
| `node.Parent()` | 父节点，根为 `nil` | O(1) |
| `node.Depth()` | 根到该节点的边数，根为 0 | O(1) |
| `node.IsRoot()` / `node.IsLeaf()` | 是否根 / 是否叶子 | O(1) |
| `node.Path()` | 根 → 自身的链路（新切片） | O(深度) |
| `node.Descendants()` | 后代迭代器，不含自身 | O(子树) |
| `node.Ancestors()` | 祖先迭代器，不含自身，根在前 | O(深度) |

遍历方法返回 `iter.Seq`，可以直接 `range`，也能用 `break` 提前结束：

```go
for node := range forest.Walk() {
	if node.Value.ID == 2 {
		break // 停止遍历，不会继续访问后面的节点
	}
}
```

`Path()` 恒等于“`Ancestors()` 的全部结果 + 自身”，根节点的 `Path()` 只有它
自己，`Ancestors()` 为空。

## 输出 JSON

节点直接序列化的形状是 `{"Value":...,"Children":[...]}`。菜单接口通常要把
业务字段摊平，做一次显式转换即可，主键与父键从 `Value` 取：

```go
type MenuVO struct {
	ID       int64    `json:"id"`
	ParentID int64    `json:"parentId"`
	Value    Menu     `json:"value"`
	Children []MenuVO `json:"children"`
}

func toVO(n *tree.Node[Menu]) MenuVO {
	vo := MenuVO{ID: n.Value.ID, ParentID: n.Value.ParentID, Value: n.Value}
	for _, child := range n.Children {
		vo.Children = append(vo.Children, toVO(child))
	}
	return vo
}

roots := forest.Roots()
out := make([]MenuVO, 0, len(roots))
for _, root := range roots {
	out = append(out, toVO(root))
}
```

如果不需要 `value` 这一层，把 `Value` 换成需要的字段即可。

## 错误

构建失败时返回 `(nil, err)`，用 `errors.Is` 判断哨兵：

| 哨兵 | 触发条件 |
| --- | --- |
| `ErrInvalidConfig` | `Config.ID` 或 `Config.ParentID` 为 nil |
| `ErrDuplicateID` | 两条记录返回了相同主键；消息带冲突下标 |
| `ErrMissingParent` | 提供了 `IsRoot`，非根记录的父键不存在 |
| `ErrCycle` | 节点自引用，或沿父指针成环；消息带环路径 |

```go
if errors.Is(err, tree.ErrCycle) {
	log.Printf("层级数据有环: %v", err) // tree: 存在环: 2 -> 3 -> 2
}
```

空输入返回空森林，不是错误。

## 只读与并发

森林构建完成后没有任何写入口。遵守“不修改 `Value` 与 `Children`”的契约时，
多个 goroutine 并发调用 `Walk`、`Find`、`Roots`、`Path` 是安全的：包内没有
惰性初始化，也没有共享的可写缓存。需要变更层级就重新 `Build`。

`Forest` 与 `Node` 的 nil 指针上调方法会 panic，与 `collection` 包的约定一致。

## 性能

节点集中分配在一块连续内存里，构建后不再追加；按主键查找 O(1)，路径与祖先
O(深度)，所有遍历与环检测都用显式栈/队列，10 万节点深链不会栈溢出。
参考基线（Apple M1 Pro，1 万节点宽树）：

```text
BenchmarkBuild-8   611 µs/op   73 allocs/op
BenchmarkWalk-8    142 µs/op   17 allocs/op
```

## 非目标

- 运行时增删改、节点移动、构建后再排序。
- 选中 / 半选中 / 展开状态与 `leaf`、`checked` 字段，这些是前端职责。
- ASCII 树渲染、图结构、闭包表 / 路径枚举。
- 二叉搜索树、红黑树、AVL 等有序查找结构。
- 直接生成业务 DTO；输出形状由调用方决定，见“输出 JSON”。
