package fsutil_test

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/corenca/teatool-go/fsutil"
)

func TestListFilesSortedAbsolute(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "root.txt"), "1")
	writeFile(t, filepath.Join(root, ".hidden"), "2")
	writeFile(t, filepath.Join(root, "a.txt"), "3")
	mustMkdir(t, filepath.Join(root, "a"))
	writeFile(t, filepath.Join(root, "a", "x.txt"), "4")
	writeFile(t, filepath.Join(root, "a", "y.txt"), "5")
	mustMkdir(t, filepath.Join(root, "b"))
	writeFile(t, filepath.Join(root, "b", "z.txt"), "6")

	got, err := fsutil.ListFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(root, ".hidden"),
		filepath.Join(root, "a.txt"),
		filepath.Join(root, "a", "x.txt"),
		filepath.Join(root, "a", "y.txt"),
		filepath.Join(root, "b", "z.txt"),
		filepath.Join(root, "root.txt"),
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestListFilesEmptyDirReturnsNonNil(t *testing.T) {
	got, err := fsutil.ListFiles(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || len(got) != 0 {
		t.Fatalf("空目录应返回非 nil 空切片，得到 %#v", got)
	}
}

func TestListFilesSkipsSymlinks(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target.txt")
	writeFile(t, target, "x")
	if err := os.Symlink(target, filepath.Join(root, "link.txt")); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	realDir := filepath.Join(root, "real")
	mustMkdir(t, realDir)
	writeFile(t, filepath.Join(realDir, "in.txt"), "y")
	if err := os.Symlink(realDir, filepath.Join(root, "linkdir")); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	got, err := fsutil.ListFiles(root)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		filepath.Join(realDir, "in.txt"),
		target,
	}
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestListFilesRootSymlink(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	mustMkdir(t, real)
	writeFile(t, filepath.Join(real, "a.txt"), "x")
	link := filepath.Join(parent, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	got, err := fsutil.ListFiles(link)
	if err != nil {
		t.Fatal(err)
	}
	want := []string{filepath.Join(link, "a.txt")}
	if !slices.Equal(got, want) {
		t.Fatalf("got %v\nwant %v", got, want)
	}
}

func TestListFilesRejectsNonDir(t *testing.T) {
	file := filepath.Join(t.TempDir(), "a.txt")
	writeFile(t, file, "x")
	if _, err := fsutil.ListFiles(file); err == nil {
		t.Fatal("文件作为 root 应返回 error")
	}
	if _, err := fsutil.ListFiles(filepath.Join(t.TempDir(), "nope")); err == nil {
		t.Fatal("不存在的 root 应返回 error")
	}
	if _, err := fsutil.ListFiles(""); err == nil {
		t.Fatal("空 root 应返回 error")
	}
}

func TestListFilesPermissionError(t *testing.T) {
	skipIfWindowsOrRoot(t)
	root := t.TempDir()
	locked := filepath.Join(root, "locked")
	mustMkdir(t, locked)
	writeFile(t, filepath.Join(locked, "a.txt"), "x")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	got, err := fsutil.ListFiles(root)
	if err == nil {
		t.Fatal("无权限子目录应返回 error")
	}
	if got != nil {
		t.Fatalf("出错时不应返回部分结果: %#v", got)
	}
}
