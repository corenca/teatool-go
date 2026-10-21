package filesplit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeFile 在 dir 下按 name 写入内容，返回完整路径。
func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("写入测试文件失败: %v", err)
	}
	return path
}

// readFile 读取文件内容。
func readFile(t *testing.T, path string) string {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("读取 %s 失败: %v", path, err)
	}
	return string(data)
}

// concatFiles 按顺序拼接多个文件的内容，用于验证切分后字节不变。
func concatFiles(t *testing.T, paths []string) string {
	t.Helper()

	var b strings.Builder
	for _, path := range paths {
		b.WriteString(readFile(t, path))
	}
	return b.String()
}

// baseNames 提取路径列表的文件名。
func baseNames(paths []string) []string {
	names := make([]string, len(paths))
	for i, path := range paths {
		names[i] = filepath.Base(path)
	}
	return names
}

// equalStrings 比较两个字符串切片。
func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
