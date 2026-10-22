package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

// listFiles 递归列出 root 下所有普通文件的绝对路径。
func listFiles(root string) ([]string, error) {
	if root == "" {
		return nil, errors.New("fsutil: 目录路径不能为空")
	}
	abs, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("fsutil: 解析目录 %s: %w", root, err)
	}
	info, err := os.Stat(abs)
	if err != nil {
		return nil, fmt.Errorf("fsutil: 读取目录 %s: %w", root, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("fsutil: 不是目录: %s", root)
	}

	files := make([]string, 0)
	var walk func(dir string) error
	walk = func(dir string) error {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		for _, entry := range entries {
			path := filepath.Join(dir, entry.Name())
			if entry.IsDir() {
				if err := walk(path); err != nil {
					return err
				}
				continue
			}
			// 符号链接、设备、FIFO、socket 的 Type 不是普通文件，直接跳过。
			if entry.Type().IsRegular() {
				files = append(files, path)
			}
		}
		return nil
	}
	if err := walk(abs); err != nil {
		return nil, fmt.Errorf("fsutil: 遍历 %s: %w", abs, err)
	}

	slices.Sort(files)
	return files, nil
}
