package tree_test

import (
	"strings"
	"testing"

	"github.com/corenca/teatool-go/tree"
)

// forest 构造固定形状的测试树：1 -> [2 -> [4], 3]。
func forest(t *testing.T) *tree.Forest[menu, int64] {
	t.Helper()
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
	return f
}

// names 把节点序列转成名称切片，便于断言顺序。
func names(nodes []*tree.Node[menu]) []string {
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, n.Value.Name)
	}
	return out
}

// 先序 DFS 为 1,2,4,3；层序 BFS 为 1,2,3,4。
func TestWalkOrders(t *testing.T) {
	f := forest(t)
	var pre, level []*tree.Node[menu]
	for node := range f.Walk() {
		pre = append(pre, node)
	}
	for node := range f.WalkLevel() {
		level = append(level, node)
	}
	t.Logf("Walk: %v", names(pre))
	t.Logf("WalkLevel: %v", names(level))
	if got := strings.Join(names(pre), ","); got != "root,a,a1,b" {
		t.Fatalf("Walk 顺序 = %s, want root,a,a1,b", got)
	}
	if got := strings.Join(names(level), ","); got != "root,a,b,a1" {
		t.Fatalf("WalkLevel 顺序 = %s, want root,a,b,a1", got)
	}
}

// Descendants 不含自身且先序；Ancestors 不含自身且根在前；Path 根→自身。
func TestNodeRelations(t *testing.T) {
	f := forest(t)
	a, _ := f.Find(2)
	a1, _ := f.Find(4)
	root, _ := f.Find(1)

	var descendants []*tree.Node[menu]
	for node := range a.Descendants() {
		descendants = append(descendants, node)
	}
	t.Logf("a.Descendants = %v", names(descendants))
	if got := strings.Join(names(descendants), ","); got != "a1" {
		t.Fatalf("Descendants = %s, want a1", got)
	}

	var ancestors []*tree.Node[menu]
	for node := range a1.Ancestors() {
		ancestors = append(ancestors, node)
	}
	t.Logf("a1.Ancestors = %v", names(ancestors))
	if got := strings.Join(names(ancestors), ","); got != "root,a" {
		t.Fatalf("Ancestors = %s, want root,a", got)
	}

	t.Logf("a1.Path = %v", names(a1.Path()))
	if got := strings.Join(names(a1.Path()), ","); got != "root,a,a1" {
		t.Fatalf("Path = %s, want root,a,a1", got)
	}
	if got := strings.Join(names(root.Path()), ","); got != "root" {
		t.Fatalf("根 Path = %s, want root", got)
	}
	var rootAncestors []*tree.Node[menu]
	for node := range root.Ancestors() {
		rootAncestors = append(rootAncestors, node)
	}
	if len(rootAncestors) != 0 {
		t.Fatalf("根 Ancestors = %v, want 空", names(rootAncestors))
	}
	if len(a1.Children) != 0 {
		t.Fatalf("叶子不应有子节点, got %d", len(a1.Children))
	}
}

// Walk 中途 break 后只收到前两个节点，不继续遍历。
func TestWalkBreak(t *testing.T) {
	f := forest(t)
	var got []string
	for node := range f.Walk() {
		got = append(got, node.Value.Name)
		if len(got) == 2 {
			break
		}
	}
	t.Logf("break 后收到: %v", got)
	if strings.Join(got, ",") != "root,a" {
		t.Fatalf("收到 = %v, want [root a]", got)
	}
}

// Flatten 按先序返回载荷；返回的是新切片，修改它不影响森林。
func TestFlatten(t *testing.T) {
	f := forest(t)
	values := f.Flatten()
	t.Logf("Flatten = %v", values)
	if len(values) != 4 || values[0].ID != 1 || values[1].ID != 2 || values[2].ID != 4 || values[3].ID != 3 {
		t.Fatalf("Flatten 顺序错误: %v", values)
	}
	values[0] = menu{ID: 99, Name: "changed"}
	if again := f.Flatten(); again[0].ID != 1 {
		t.Fatal("修改 Flatten 返回值影响了森林")
	}
}

// 空森林：Walk/WalkLevel 不产出节点，Flatten 为空。
func TestWalkEmpty(t *testing.T) {
	f, err := tree.Build(nil, menuConfig())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	count := 0
	for range f.Walk() {
		count++
	}
	for range f.WalkLevel() {
		count++
	}
	if count != 0 {
		t.Fatalf("空森林遍历出 %d 个节点", count)
	}
	if len(f.Flatten()) != 0 {
		t.Fatalf("空森林 Flatten = %v", f.Flatten())
	}
}
