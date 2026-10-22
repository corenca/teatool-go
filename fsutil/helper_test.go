package fsutil_test

import (
	"os"
	"runtime"
	"testing"
)

// writeFile 写入测试文件，失败即终止用例。
func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// mustMkdir 递归创建测试目录，失败即终止用例。
func mustMkdir(t *testing.T, path string) {
	t.Helper()
	if err := os.MkdirAll(path, 0o755); err != nil {
		t.Fatal(err)
	}
}

// skipIfWindowsOrRoot 跳过权限语义不同的平台与 root 用户。
func skipIfWindowsOrRoot(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("Windows 权限语义不同")
	}
	if os.Geteuid() == 0 {
		t.Skip("root 不受权限位限制")
	}
}
