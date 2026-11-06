package tree_test

import (
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/corenca/teatool-go/tree"
)

// Roots/Path 返回副本：改动返回值后森林与节点不受影响。
func TestRootsAndPathAreCopies(t *testing.T) {
	f := forest(t)
	roots := f.Roots()
	roots[0] = nil
	if again := f.Roots(); again[0] == nil {
		t.Fatal("修改 Roots 返回值影响了森林")
	}

	node, _ := f.Find(4)
	path := node.Path()
	path[0] = nil
	if again := node.Path(); again[0] == nil {
		t.Fatal("修改 Path 返回值影响了节点")
	}
}

// 并发只读：多个 goroutine 同时 Walk/Find/Roots/Path/Flatten，配合 -race 无竞态。
func TestConcurrentReads(t *testing.T) {
	rows := make([]menu, 0, 500)
	rows = append(rows, menu{ID: 1, ParentID: 0, Name: "root"})
	for i := int64(2); i <= 500; i++ {
		rows = append(rows, menu{ID: i, ParentID: 1, Name: fmt.Sprintf("n%d", i)})
	}
	f, err := tree.Build(rows, menuConfig())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}

	var wg sync.WaitGroup
	for worker := 0; worker < 8; worker++ {
		wg.Go(func() {
			for round := 0; round < 50; round++ {
				count := 0
				for range f.Walk() {
					count++
				}
				if count != 500 {
					t.Errorf("Walk 计数 = %d, want 500", count)
					return
				}
				if node, ok := f.Find(250); !ok || len(node.Path()) != 2 {
					t.Errorf("Find(250) 结果错误: %v, %v", node, ok)
					return
				}
				if len(f.Roots()) != 1 || len(f.Flatten()) != 500 {
					t.Error("Roots/Flatten 结果长度错误")
					return
				}
			}
		})
	}
	wg.Wait()
}

// 10 万节点深链：构建、遍历、取路径都不爆栈，深度逐层递增。
func TestBuildDeepChain(t *testing.T) {
	const n = 100_000
	rows := make([]menu, n)
	rows[0] = menu{ID: 1, ParentID: 0, Name: "n1"}
	for i := 1; i < n; i++ {
		rows[i] = menu{ID: int64(i + 1), ParentID: int64(i), Name: fmt.Sprintf("n%d", i+1)}
	}

	start := time.Now()
	f, err := tree.Build(rows, menuConfig())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	t.Logf("构建 %d 节点耗时: %v", n, time.Since(start))

	if f.Len() != n {
		t.Fatalf("Len() = %d, want %d", f.Len(), n)
	}
	root, _ := f.Find(1)
	last, _ := f.Find(n)
	if root.Depth() != 0 || last.Depth() != n-1 {
		t.Fatalf("深度 = %d / %d, want 0 / %d", root.Depth(), last.Depth(), n-1)
	}

	count := 0
	for range f.Walk() {
		count++
	}
	if count != n {
		t.Fatalf("Walk 计数 = %d, want %d", count, n)
	}
	if len(last.Path()) != n {
		t.Fatalf("Path 长度 = %d, want %d", len(last.Path()), n)
	}
}

// 1 万节点宽树：根有 9999 个子节点，查找与遍历结果正确。
func TestBuildWideTree(t *testing.T) {
	const children = 9_999
	rows := make([]menu, 0, children+1)
	rows = append(rows, menu{ID: 1, ParentID: 0, Name: "root"})
	for i := 1; i <= children; i++ {
		rows = append(rows, menu{ID: int64(i + 1), ParentID: 1, Name: fmt.Sprintf("c%d", i)})
	}

	start := time.Now()
	f, err := tree.Build(rows, menuConfig())
	if err != nil {
		t.Fatalf("Build 失败: %v", err)
	}
	t.Logf("构建 %d 节点宽树耗时: %v", children+1, time.Since(start))

	root, _ := f.Find(1)
	if len(root.Children) != children {
		t.Fatalf("根子节点数 = %d, want %d", len(root.Children), children)
	}
	last, ok := f.Find(int64(children + 1))
	if !ok || last.Depth() != 1 {
		t.Fatalf("Find(%d) = %v, %v", children+1, last, ok)
	}
}

// BenchmarkBuild 以 1 万节点宽树为输入，测量构建的耗时与分配。
func BenchmarkBuild(b *testing.B) {
	const children = 9_999
	rows := make([]menu, 0, children+1)
	rows = append(rows, menu{ID: 1, ParentID: 0, Name: "root"})
	for i := 1; i <= children; i++ {
		rows = append(rows, menu{ID: int64(i + 1), ParentID: 1, Name: fmt.Sprintf("c%d", i)})
	}
	config := menuConfig()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		if _, err := tree.Build(rows, config); err != nil {
			b.Fatalf("Build 失败: %v", err)
		}
	}
}

// BenchmarkWalk 测量 1 万节点宽树的全量先序遍历。
func BenchmarkWalk(b *testing.B) {
	const children = 9_999
	rows := make([]menu, 0, children+1)
	rows = append(rows, menu{ID: 1, ParentID: 0, Name: "root"})
	for i := 1; i <= children; i++ {
		rows = append(rows, menu{ID: int64(i + 1), ParentID: 1, Name: fmt.Sprintf("c%d", i)})
	}
	f, err := tree.Build(rows, menuConfig())
	if err != nil {
		b.Fatalf("Build 失败: %v", err)
	}
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		count := 0
		for range f.Walk() {
			count++
		}
		if count != children+1 {
			b.Fatalf("Walk 计数 = %d, want %d", count, children+1)
		}
	}
}
