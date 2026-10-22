package fsutil_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	"github.com/corenca/teatool-go/fsutil"
)

func TestMoveRenamesFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	writeFile(t, src, "hello")
	dst := filepath.Join(dir, "deep", "dst.txt")

	if err := fsutil.Move(src, dst); err != nil {
		t.Fatalf("Move: %v", err)
	}
	if _, err := os.Lstat(src); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("源文件应已不存在: %v", err)
	}
	if got, err := os.ReadFile(dst); err != nil || string(got) != "hello" {
		t.Fatalf("目标内容 = %q, err %v", got, err)
	}
}

func TestMoveRefusesExistingTarget(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src.txt")
	writeFile(t, src, "new")
	dst := filepath.Join(dir, "dst.txt")
	writeFile(t, dst, "old")

	err := fsutil.Move(src, dst)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("目标已存在应满足 fs.ErrExist，得到 %v", err)
	}
	if got, _ := os.ReadFile(src); string(got) != "new" {
		t.Fatalf("源文件被改动: %q", got)
	}
	if got, _ := os.ReadFile(dst); string(got) != "old" {
		t.Fatalf("目标被覆盖: %q", got)
	}
}

func TestMoveRejectsNonRegularSource(t *testing.T) {
	dir := t.TempDir()
	if err := fsutil.Move(dir, filepath.Join(dir, "dst")); err == nil {
		t.Fatal("目录源应返回 error")
	}
	if err := fsutil.Move(filepath.Join(dir, "nope.txt"), filepath.Join(dir, "dst")); err == nil {
		t.Fatal("不存在的源应返回 error")
	}
	file := filepath.Join(dir, "a.txt")
	writeFile(t, file, "x")
	if err := fsutil.Move(file, ""); err == nil {
		t.Fatal("空目标路径应返回 error")
	}
	link := filepath.Join(dir, "link.txt")
	if err := os.Symlink(file, link); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}
	if err := fsutil.Move(link, filepath.Join(dir, "b.txt")); err == nil {
		t.Fatal("符号链接源应返回 error")
	}
}

func TestMoveFallsBackToCopy(t *testing.T) {
	skipIfWindowsOrRoot(t)
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "srcdir")
	mustMkdir(t, srcDir)
	src := filepath.Join(srcDir, "a.txt")
	writeFile(t, src, "hello")
	if err := os.Chmod(srcDir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(srcDir, 0o700) })

	dst := filepath.Join(t.TempDir(), "b.txt")
	err := fsutil.Move(src, dst)
	if err == nil {
		t.Fatal("删除源失败时应返回 error")
	}
	if got, readErr := os.ReadFile(dst); readErr != nil || string(got) != "hello" {
		t.Fatalf("回退复制未成功: %q, %v", got, readErr)
	}
	if _, statErr := os.Stat(src); statErr != nil {
		t.Fatalf("源文件应仍在: %v", statErr)
	}
}
