package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
)

// copyFile 把单个普通文件复制到 dst，目标已存在时报错。
func copyFile(src, dst string) error {
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

	// O_EXCL 同时保证不覆盖，并让"目标已存在"的错误满足 fs.ErrExist。
	out, err := os.OpenFile(dst, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("fsutil: 创建目标文件 %s: %w", dst, err)
	}

	in, err := os.Open(src)
	if err != nil {
		_ = out.Close()
		_ = os.Remove(dst)
		return fmt.Errorf("fsutil: 打开源文件 %s: %w", src, err)
	}

	_, copyErr := io.Copy(out, in)
	_ = in.Close()
	closeErr := out.Close()
	if copyErr != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("fsutil: 复制 %s 到 %s: %w", src, dst, copyErr)
	}
	if closeErr != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("fsutil: 关闭目标文件 %s: %w", dst, closeErr)
	}

	// 创建时固定 0o600，写入成功后再精确还原源的权限位。
	if err := os.Chmod(dst, info.Mode().Perm()); err != nil {
		_ = os.Remove(dst)
		return fmt.Errorf("fsutil: 设置目标文件 %s 的权限: %w", dst, err)
	}
	return nil
}
