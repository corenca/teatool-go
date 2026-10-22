package fsutil_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/corenca/teatool-go/fsutil"
)

func TestCopy(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	writeFile(t, src, "hello")
	dst := filepath.Join(dir, "deep", "dst.txt")

	if err := fsutil.Copy(src, dst); err != nil {
		t.Fatalf("Copy: %v", err)
	}
	got, err := os.ReadFile(dst)
	if err != nil || string(got) != "hello" {
		t.Fatalf("目标内容 = %q, err %v", got, err)
	}
	if ok, _ := fsutil.IsDir(filepath.Join(dir, "deep")); !ok {
		t.Fatal("目标父目录未创建")
	}
}

func TestCopyPreservesPerm(t *testing.T) {
	skipIfWindowsOrRoot(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	writeFile(t, src, "x")
	if err := os.Chmod(src, 0o640); err != nil {
		t.Fatal(err)
	}
	dst := filepath.Join(dir, "dst.txt")
	if err := fsutil.Copy(src, dst); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o640 {
		t.Fatalf("目标权限 = %o, want 640", got)
	}
}

func TestCopyRefusesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	writeFile(t, src, "new")
	dst := filepath.Join(dir, "dst.txt")
	writeFile(t, dst, "old")

	err := fsutil.Copy(src, dst)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("目标已存在应满足 fs.ErrExist，得到 %v", err)
	}
	if got, _ := os.ReadFile(dst); string(got) != "old" {
		t.Fatalf("目标被覆盖: %q", got)
	}
}

func TestCopyRejectsNonRegularSource(t *testing.T) {
	dir := t.TempDir()
	if err := fsutil.Copy(dir, filepath.Join(dir, "dst.txt")); err == nil {
		t.Fatal("目录源应返回 error")
	}
	if err := fsutil.Copy(filepath.Join(dir, "nope.txt"), filepath.Join(dir, "dst.txt")); err == nil {
		t.Fatal("不存在的源应返回 error")
	}
	file := filepath.Join(dir, "a.txt")
	writeFile(t, file, "x")
	if err := fsutil.Copy(file, ""); err == nil {
		t.Fatal("空目标路径应返回 error")
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(file, link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	if err := fsutil.Copy(link, filepath.Join(dir, "b.txt")); err == nil {
		t.Fatal("符号链接源应返回 error")
	}
}

func TestCopyDoesNotLeaveTargetWhenOpenFails(t *testing.T) {
	skipIfWindowsOrRoot(t)
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	writeFile(t, src, "x")
	dstDir := filepath.Join(dir, "ro")
	mustMkdir(t, dstDir)
	if err := os.Chmod(dstDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dstDir, 0o755) })

	dst := filepath.Join(dstDir, "dst.txt")
	if err := fsutil.Copy(src, dst); err == nil {
		t.Fatal("目标目录不可写时应返回 error")
	}
	if _, err := os.Lstat(dst); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("失败后不应留下目标文件: %v", err)
	}
}
