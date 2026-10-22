package fsutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
)

// moveFile 把单个普通文件移动到 dst，跨设备时回退为复制后删除。
func moveFile(src, dst string) error {
	info, err := os.Lstat(src)
	if err != nil {
		return fmt.Errorf("fsutil: 读取源文件 %s: %w", src, err)
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("fsutil: 源必须是普通文件: %s", src)
	}
	if dst == "" {
		return errors.New("fsutil: 目标路径不能为空")
	}
	if err := EnsureParent(dst); err != nil {
		return err
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("fsutil: 目标已存在 %s: %w", dst, fs.ErrExist)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("fsutil: 检查目标 %s: %w", dst, err)
	}

	renameErr := os.Rename(src, dst)
	if renameErr == nil {
		return nil
	}

	// 重命名失败不区分错误码：跨设备时回退为复制后删除，平台无关。
	if err := Copy(src, dst); err != nil {
		return fmt.Errorf("fsutil: 移动 %s 到 %s: 重命名失败（%v），复制回退失败: %w", src, dst, renameErr, err)
	}
	if err := os.Remove(src); err != nil {
		return fmt.Errorf("fsutil: 已复制到 %s，但删除源文件 %s 失败: %w", dst, src, err)
	}
	return nil
}
