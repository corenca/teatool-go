package fsutil_test

import (
	"errors"
	"path/filepath"
	"testing"

	"github.com/corenca/teatool-go/fsutil"
)

func TestJoinWithin(t *testing.T) {
	base := filepath.Join(t.TempDir(), "base")

	got, err := fsutil.JoinWithin(base, "a/b.txt")
	if err != nil || got != filepath.Join(base, "a/b.txt") {
		t.Fatalf("合法相对路径: got %q, err %v", got, err)
	}
	got, err = fsutil.JoinWithin(base, "a/../b.txt")
	if err != nil || got != filepath.Join(base, "b.txt") {
		t.Fatalf("中间 .. 应被 Clean: got %q, err %v", got, err)
	}

	for _, name := range []string{"", "..", "../x", "/abs", "a/../../b"} {
		if _, err := fsutil.JoinWithin(base, name); !errors.Is(err, fsutil.ErrOutsideBase) {
			t.Fatalf("JoinWithin(%q) 应返回 ErrOutsideBase，得到 %v", name, err)
		}
	}

	if _, err := fsutil.JoinWithin("", "a.txt"); err == nil || errors.Is(err, fsutil.ErrOutsideBase) {
		t.Fatalf("空 base 应返回独立错误，得到 %v", err)
	}
}
