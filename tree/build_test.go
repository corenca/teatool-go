package tree_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/tree"
)

// menu 是测试用的业务记录，模拟数据库里的菜单行。
type menu struct {
	ID       int64
	ParentID int64
	Name     string
	Sort     int
}

// menuConfig 返回按 menu 字段取键的基础配置。
func menuConfig() tree.Config[menu, int64] {
	return tree.Config[menu, int64]{
		ID:       func(m menu) int64 { return m.ID },
		ParentID: func(m menu) int64 { return m.ParentID },
	}
}

// shape 把森林渲染成缩进文本，供 t.Log 展示构建结果。
func shape(t *testing.T, roots []*tree.Node[menu]) []string {
	t.Helper()
	var out []string
	var walk func(*tree.Node[menu], int)
	walk = func(n *tree.Node[menu], depth int) {
		out = append(out, strings.Repeat("  ", depth)+n.Value.Name)
		for _, c := range n.Children {
			walk(c, depth+1)
		}
	}
	for _, r := range roots {
		walk(r, 0)
	}
	return out
}

// 单根三层：根 1，子 2、3，2 的子 4；断言节点数量、根、查找与层级信息。
func TestBuildSingleRoot(t *testing.T) {
	rows := []menu{
		{ID: 1, ParentID: 0, Name: "root"},
		{ID: 2, ParentID: 1, Name: "a"},
		{ID: 3, ParentID: 1, Name: "b"},
		{ID: 4, ParentID: 2, Name: "a1"},
	}
	f, err := tree.Build(rows, menuConfig())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	t.Logf("树形:\n%s", strings.Join(shape(t, f.Roots()), "\n"))
	if f.Len() != 4 {
		t.Fatalf("Len() = %d, want 4", f.Len())
	}
	roots := f.Roots()
	if len(roots) != 1 || roots[0].Value.ID != 1 {
		t.Fatalf("Roots() 长度 = %d, 首根 = %v, want [1]", len(roots), roots[0].Value.ID)
	}
	node, ok := f.Find(4)
	if !ok || node.Value.Name != "a1" {
		t.Fatalf("Find(4) = %v, %v", node, ok)
	}
	if node.Depth() != 2 {
		t.Fatalf("Depth() = %d, want 2", node.Depth())
	}
	if node.Parent().Value.ID != 2 {
		t.Fatalf("Parent() = %d, want 2", node.Parent().Value.ID)
	}
	if !roots[0].IsRoot() || node.IsRoot() {
		t.Fatal("IsRoot() 判断错误")
	}
	if !node.IsLeaf() || roots[0].IsLeaf() {
		t.Fatal("IsLeaf() 判断错误")
	}
	if _, ok := f.Find(99); ok {
		t.Fatal("Find(99) 不应命中")
	}
}

// 空输入：返回非 nil 空森林，长度 0、没有根、查找不命中。
func TestBuildEmpty(t *testing.T) {
	f, err := tree.Build(nil, menuConfig())
	if err != nil {
		t.Fatalf("Build 空输入失败: %v", err)
	}
	if f == nil {
		t.Fatal("Build 返回了 nil 森林")
	}
	if f.Len() != 0 || len(f.Roots()) != 0 {
		t.Fatalf("空森林 Len() = %d, 根数 = %d", f.Len(), len(f.Roots()))
	}
	if _, ok := f.Find(1); ok {
		t.Fatal("空森林 Find 不应命中")
	}
}

// 回调缺失：ID 或 ParentID 为 nil 时必须返回 ErrInvalidConfig。
func TestBuildNilAccessor(t *testing.T) {
	rows := []menu{{ID: 1, Name: "root"}}

	_, err := tree.Build(rows, tree.Config[menu, int64]{
		ParentID: func(m menu) int64 { return m.ParentID },
	})
	if !errors.Is(err, tree.ErrInvalidConfig) {
		t.Fatalf("ID 为 nil 时 err = %v, want ErrInvalidConfig", err)
	}

	_, err = tree.Build(rows, tree.Config[menu, int64]{
		ID: func(m menu) int64 { return m.ID },
	})
	if !errors.Is(err, tree.ErrInvalidConfig) {
		t.Fatalf("ParentID 为 nil 时 err = %v, want ErrInvalidConfig", err)
	}
}

// 无 Less 时根与兄弟节点都保持输入相对顺序。
func TestBuildKeepsInputOrder(t *testing.T) {
	rows := []menu{
		{ID: 1, ParentID: 0, Name: "root"},
		{ID: 2, ParentID: 1, Name: "a"},
		{ID: 3, ParentID: 1, Name: "b"},
		{ID: 4, ParentID: 2, Name: "a1"},
	}
	f, err := tree.Build(rows, menuConfig())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	got := shape(t, f.Roots())
	want := []string{"root", "  a", "    a1", "  b"}
	t.Logf("实际顺序: %v", got)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("顺序 = %v, want %v", got, want)
	}
}

// 多个根节点都保持输入顺序，各自挂自己的子树。
func TestBuildMultipleRoots(t *testing.T) {
	rows := []menu{
		{ID: 3, ParentID: 0, Name: "r3"},
		{ID: 1, ParentID: 0, Name: "r1"},
		{ID: 2, ParentID: 3, Name: "c2"},
	}
	f, err := tree.Build(rows, menuConfig())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	t.Logf("树形: %v", shape(t, f.Roots()))
	roots := f.Roots()
	if len(roots) != 2 || roots[0].Value.ID != 3 || roots[1].Value.ID != 1 {
		t.Fatalf("根顺序 = %v, want [3 1]", []int64{roots[0].Value.ID, roots[1].Value.ID})
	}
	if len(roots[0].Children) != 1 || roots[0].Children[0].Value.ID != 2 {
		t.Fatalf("根 3 的子节点 = %v, want [2]", roots[0].Children)
	}
	if len(roots[1].Children) != 0 {
		t.Fatalf("根 1 不应有子节点, got %d", len(roots[1].Children))
	}
}

// Less 按 Sort 升序；Sort 相同时保持输入相对顺序（稳定排序），根列表也参与排序。
func TestBuildLessStable(t *testing.T) {
	rows := []menu{
		{ID: 1, ParentID: 0, Name: "root", Sort: 5},
		{ID: 2, ParentID: 1, Name: "b", Sort: 2},
		{ID: 3, ParentID: 1, Name: "a", Sort: 1},
		{ID: 4, ParentID: 1, Name: "a2", Sort: 1},
		{ID: 5, ParentID: 0, Name: "root2", Sort: 1},
	}
	config := menuConfig()
	config.Less = func(a, b menu) bool { return a.Sort < b.Sort }
	f, err := tree.Build(rows, config)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	t.Logf("树形: %v", shape(t, f.Roots()))
	roots := f.Roots()
	if roots[0].Value.Name != "root2" || roots[1].Value.Name != "root" {
		t.Fatalf("根顺序 = %v, want [root2 root]", []string{roots[0].Value.Name, roots[1].Value.Name})
	}
	children := roots[1].Children
	if len(children) != 3 || children[0].Value.Name != "a" || children[1].Value.Name != "a2" || children[2].Value.Name != "b" {
		t.Fatalf("子节点顺序 = %v, want [a a2 b]", []string{children[0].Value.Name, children[1].Value.Name, children[2].Value.Name})
	}
}

// 同一输入连续构建两次，根与每层子节点的顺序完全一致（不依赖 map 迭代）。
func TestBuildDeterministic(t *testing.T) {
	rows := []menu{
		{ID: 1, ParentID: 0, Name: "root", Sort: 1},
		{ID: 2, ParentID: 1, Name: "b", Sort: 2},
		{ID: 3, ParentID: 1, Name: "a", Sort: 1},
		{ID: 4, ParentID: 2, Name: "b1", Sort: 1},
		{ID: 5, ParentID: 0, Name: "root2", Sort: 2},
	}
	config := menuConfig()
	config.Less = func(a, b menu) bool { return a.Sort < b.Sort }
	first, err := tree.Build(rows, config)
	if err != nil {
		t.Fatalf("第一次 Build 失败: %v", err)
	}
	second, err := tree.Build(rows, config)
	if err != nil {
		t.Fatalf("第二次 Build 失败: %v", err)
	}
	a := strings.Join(shape(t, first.Roots()), "|")
	b := strings.Join(shape(t, second.Roots()), "|")
	t.Logf("第一次: %s", a)
	t.Logf("第二次: %s", b)
	if a != b {
		t.Fatalf("两次构建顺序不一致:\n%s\n%s", a, b)
	}
}

// 重复 ID：两行主键相同，期望 ErrDuplicateID，消息带冲突下标。
func TestBuildDuplicateID(t *testing.T) {
	rows := []menu{
		{ID: 1, ParentID: 0, Name: "root"},
		{ID: 1, ParentID: 0, Name: "dup"},
	}
	_, err := tree.Build(rows, menuConfig())
	if !errors.Is(err, tree.ErrDuplicateID) {
		t.Fatalf("err = %v, want ErrDuplicateID", err)
	}
	t.Logf("错误信息: %v", err)
	if !strings.Contains(err.Error(), "index 1") {
		t.Fatalf("错误信息未包含输入下标: %v", err)
	}
}

// 默认宽容：父键 99 不存在时，节点 2 成为第二个根。
func TestBuildMissingParentBecomesRoot(t *testing.T) {
	rows := []menu{
		{ID: 1, ParentID: 0, Name: "root"},
		{ID: 2, ParentID: 99, Name: "orphan"},
	}
	f, err := tree.Build(rows, menuConfig())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	t.Logf("树形: %v", shape(t, f.Roots()))
	roots := f.Roots()
	if len(roots) != 2 || roots[1].Value.ID != 2 || !roots[1].IsRoot() {
		t.Fatalf("根 = %v, want [1 2] 且 2 是根", []int64{roots[0].Value.ID, roots[1].Value.ID})
	}
}

// 严格模式：设置 IsRoot 后，父键 99 缺失的节点 2 报 ErrMissingParent。
func TestBuildStrictMissingParent(t *testing.T) {
	rows := []menu{
		{ID: 1, ParentID: 0, Name: "root"},
		{ID: 2, ParentID: 99, Name: "orphan"},
	}
	config := menuConfig()
	config.IsRoot = func(m menu) bool { return m.ParentID == 0 }
	_, err := tree.Build(rows, config)
	if !errors.Is(err, tree.ErrMissingParent) {
		t.Fatalf("err = %v, want ErrMissingParent", err)
	}
	t.Logf("错误信息: %v", err)
	if !strings.Contains(err.Error(), "99") {
		t.Fatalf("错误信息未包含缺失父键: %v", err)
	}
}

// IsRoot 优先：节点 2 的父键指向存在的 1，但被标为根；
// 它不能挂到 1 下面，1 也不应有子节点。
func TestBuildIsRootOverridesParent(t *testing.T) {
	rows := []menu{
		{ID: 1, ParentID: 0, Name: "root"},
		{ID: 2, ParentID: 1, Name: "forced"},
	}
	config := menuConfig()
	config.IsRoot = func(m menu) bool { return m.ParentID == 0 || m.ID == 2 }
	f, err := tree.Build(rows, config)
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	t.Logf("树形: %v", shape(t, f.Roots()))
	roots := f.Roots()
	if len(roots) != 2 || roots[1].Value.ID != 2 {
		t.Fatalf("根 = %v, want [1 2]", []int64{roots[0].Value.ID, roots[1].Value.ID})
	}
	if roots[1].Parent() != nil {
		t.Fatal("被标为根的节点 2 仍有父节点")
	}
	if len(roots[0].Children) != 0 {
		t.Fatalf("节点 1 不应有子节点, got %d", len(roots[0].Children))
	}
}

// 自引用、两节点环、三节点环都必须报 ErrCycle，消息给出环路径。
func TestBuildCycle(t *testing.T) {
	cases := []struct {
		name string
		rows []menu
	}{
		{
			name: "自引用",
			rows: []menu{{ID: 5, ParentID: 5, Name: "self"}},
		},
		{
			name: "两节点环",
			rows: []menu{
				{ID: 1, ParentID: 2, Name: "a"},
				{ID: 2, ParentID: 1, Name: "b"},
			},
		},
		{
			name: "三节点环",
			rows: []menu{
				{ID: 1, ParentID: 2, Name: "a"},
				{ID: 2, ParentID: 3, Name: "b"},
				{ID: 3, ParentID: 1, Name: "c"},
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := tree.Build(tc.rows, menuConfig())
			if !errors.Is(err, tree.ErrCycle) {
				t.Fatalf("err = %v, want ErrCycle", err)
			}
			t.Logf("错误信息: %v", err)
			if !strings.Contains(err.Error(), "->") {
				t.Fatalf("错误信息未包含环路径: %v", err)
			}
		})
	}
}

// 环与根不可达，且另有节点沿父链进入环：从根 BFS 访问不到这些节点，同样报 ErrCycle。
func TestBuildCycleWithLooseBranch(t *testing.T) {
	rows := []menu{
		{ID: 1, ParentID: 0, Name: "root"},
		{ID: 10, ParentID: 11, Name: "c10"},
		{ID: 11, ParentID: 12, Name: "c11"},
		{ID: 12, ParentID: 10, Name: "c12"},
		{ID: 13, ParentID: 10, Name: "branch"},
	}
	_, err := tree.Build(rows, menuConfig())
	if !errors.Is(err, tree.ErrCycle) {
		t.Fatalf("err = %v, want ErrCycle", err)
	}
	t.Logf("错误信息: %v", err)
	if !strings.Contains(err.Error(), "->") {
		t.Fatalf("错误信息未包含环路径: %v", err)
	}
}
