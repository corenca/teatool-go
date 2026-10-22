package fsutil_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/corenca/teatool-go/fsutil"
)

func TestEnsureParent(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "a", "b", "c.txt")
	if err := fsutil.EnsureParent(target); err != nil {
		t.Fatalf("EnsureParent: %v", err)
	}
	if ok, err := fsutil.IsDir(filepath.Join(dir, "a", "b")); err != nil || !ok {
		t.Fatalf("父目录未创建: %v, %v", ok, err)
	}
	if err := fsutil.EnsureParent(target); err != nil {
		t.Fatalf("父目录已存在时应返回 nil: %v", err)
	}
	if err := fsutil.EnsureParent("a.txt"); err != nil {
		t.Fatalf("没有目录部分时应返回 nil: %v", err)
	}
	if err := fsutil.EnsureParent(""); err == nil {
		t.Fatal("空路径应返回 error")
	}

	file := filepath.Join(dir, "plain")
	writeFile(t, file, "x")
	if err := fsutil.EnsureParent(filepath.Join(file, "x.txt")); err == nil {
		t.Fatal("父路径是文件时应返回 error")
	}

	trailing := filepath.Join(dir, "trailing") + string(os.PathSeparator)
	if err := fsutil.EnsureParent(trailing); err != nil {
		t.Fatalf("带结尾分隔符的路径: %v", err)
	}
	if ok, err := fsutil.IsDir(filepath.Join(dir, "trailing")); err != nil || !ok {
		t.Fatalf("带结尾分隔符时应创建该目录: %v, %v", ok, err)
	}
}
