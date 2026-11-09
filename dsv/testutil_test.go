package dsv_test

import (
	"os"
	"path/filepath"
	"testing"
)

// tempFile 把内容写入临时文件并返回路径，用于文件入口用例。
func tempFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "in.txt")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	return path
}

// readFile 读取文件内容，用于写出用例断言字节级结果。
func readFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(b)
}
