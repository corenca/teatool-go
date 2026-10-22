package fsutil_test

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/corenca/teatool-go/fsutil"
)

func TestExists(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	writeFile(t, file, "x")
	sub := filepath.Join(dir, "sub")
	mustMkdir(t, sub)
	broken := filepath.Join(dir, "broken")
	if err := os.Symlink(filepath.Join(dir, "missing"), broken); err != nil {
		t.Skipf("无法创建符号链接: %v", err)
	}

	cases := []struct {
		name string
		path string
		want bool
	}{
		{"文件", file, true},
		{"目录", sub, true},
		{"不存在", filepath.Join(dir, "nope"), false},
		{"断链符号链接", broken, false},
		{"空路径", "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := fsutil.Exists(tc.path)
			if err != nil {
				t.Fatalf("Exists(%q) 返回错误: %v", tc.path, err)
			}
			if got != tc.want {
				t.Fatalf("Exists(%q) = %v, want %v", tc.path, got, tc.want)
			}
		})
	}
}

func TestExistsReturnsNonExistError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 对 ENOTDIR 的映射不同")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	writeFile(t, file, "x")

	_, err := fsutil.Exists(filepath.Join(file, "child"))
	if err == nil {
		t.Fatal("路径中间是文件时应返回 error，而不是 false")
	}
	if errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("不应把 ENOTDIR 当成不存在: %v", err)
	}
}

func TestExistsPermissionError(t *testing.T) {
	skipIfWindowsOrRoot(t)
	dir := t.TempDir()
	locked := filepath.Join(dir, "locked")
	mustMkdir(t, locked)
	writeFile(t, filepath.Join(locked, "a.txt"), "x")
	if err := os.Chmod(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	if _, err := fsutil.Exists(filepath.Join(locked, "a.txt")); err == nil {
		t.Fatal("权限错误必须返回 error")
	}
}

func TestIsFileIsDirSpecialFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows 没有 /dev/null")
	}
	if ok, err := fsutil.Exists("/dev/null"); err != nil || !ok {
		t.Fatalf("Exists(/dev/null) = %v, %v", ok, err)
	}
	if ok, err := fsutil.IsFile("/dev/null"); err != nil || ok {
		t.Fatalf("IsFile(/dev/null) = %v, %v", ok, err)
	}
	if ok, err := fsutil.IsDir("/dev/null"); err != nil || ok {
		t.Fatalf("IsDir(/dev/null) = %v, %v", ok, err)
	}
}

func TestIsFileIsDir(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a.txt")
	writeFile(t, file, "x")
	sub := filepath.Join(dir, "sub")
	mustMkdir(t, sub)
	missing := filepath.Join(dir, "nope")

	if ok, err := fsutil.IsFile(file); err != nil || !ok {
		t.Fatalf("IsFile(文件) = %v, %v", ok, err)
	}
	if ok, err := fsutil.IsFile(sub); err != nil || ok {
		t.Fatalf("IsFile(目录) = %v, %v", ok, err)
	}
	if ok, err := fsutil.IsDir(sub); err != nil || !ok {
		t.Fatalf("IsDir(目录) = %v, %v", ok, err)
	}
	if ok, err := fsutil.IsDir(file); err != nil || ok {
		t.Fatalf("IsDir(文件) = %v, %v", ok, err)
	}
	if ok, err := fsutil.IsFile(missing); err != nil || ok {
		t.Fatalf("IsFile(不存在) = %v, %v", ok, err)
	}
}
