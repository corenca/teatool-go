package fsutil_test

import (
	"path/filepath"
	"testing"

	"github.com/corenca/teatool-go/fsutil"
)

func TestUniqueName(t *testing.T) {
	dir := t.TempDir()

	got, err := fsutil.UniqueName(dir, "a.txt")
	if err != nil || got != filepath.Join(dir, "a.txt") {
		t.Fatalf("首个候选: got %q, err %v", got, err)
	}
	writeFile(t, got, "1")

	got, err = fsutil.UniqueName(dir, "a.txt")
	if err != nil || got != filepath.Join(dir, "a_1.txt") {
		t.Fatalf("第二个候选: got %q, err %v", got, err)
	}
	writeFile(t, got, "2")

	got, err = fsutil.UniqueName(dir, "a.txt")
	if err != nil || got != filepath.Join(dir, "a_2.txt") {
		t.Fatalf("第三个候选: got %q, err %v", got, err)
	}
}

func TestUniqueNameKeepsExtension(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.tar.gz"), "x")
	if got, _ := fsutil.UniqueName(dir, "a.tar.gz"); got != filepath.Join(dir, "a.tar_1.gz") {
		t.Fatalf("多段扩展名: got %q", got)
	}

	writeFile(t, filepath.Join(dir, ".env"), "x")
	if got, _ := fsutil.UniqueName(dir, ".env"); got != filepath.Join(dir, ".env_1") {
		t.Fatalf("前导点文件名: got %q", got)
	}
}

func TestUniqueNameRejectsNonSegment(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"", ".", "..", "a/b.txt", `a\b.txt`} {
		if _, err := fsutil.UniqueName(dir, name); err == nil {
			t.Fatalf("UniqueName(%q) 应返回 error", name)
		}
	}

	file := filepath.Join(t.TempDir(), "f")
	writeFile(t, file, "x")
	if _, err := fsutil.UniqueName(file, "a.txt"); err == nil {
		t.Fatal("dir 是文件时应返回 error")
	}
}
