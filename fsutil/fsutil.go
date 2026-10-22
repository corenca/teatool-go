// Package fsutil 提供业务开发中常用的文件系统工具：路径判定、父目录准备、
// 文件名清洗与安全拼接、单文件复制/移动以及目录内文件遍历。
//
// 判定类函数跟随符号链接；复制、移动与遍历不跟随符号链接，只处理普通文件。
// 所有返回 error 的函数都用 %w 保留底层错误，调用方可用 errors.Is 判断。
package fsutil

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// Exists 判断 path 是否存在。
//
// 判定跟随符号链接：断链的符号链接视为不存在。只有底层错误满足
// fs.ErrNotExist 时才返回 (false, nil)；权限不足、路径中间是文件等
// 无法判断的情况返回 (false, err)，不会伪装成"不存在"。
func Exists(path string) (bool, error) {
	_, ok, err := statPath(path)
	if err != nil {
		return false, fmt.Errorf("fsutil: 读取 %s 的状态: %w", path, err)
	}
	return ok, nil
}

// IsFile 判断 path 是否存在且为普通文件。
//
// 判定跟随符号链接；目录、设备、FIFO、socket 都返回 false。
// 路径不存在时返回 (false, nil)，其余错误原样上报。
func IsFile(path string) (bool, error) {
	info, ok, err := statPath(path)
	if err != nil {
		return false, fmt.Errorf("fsutil: 读取 %s 的状态: %w", path, err)
	}
	return ok && info.Mode().IsRegular(), nil
}

// IsDir 判断 path 是否存在且为目录。
//
// 判定跟随符号链接；路径不存在时返回 (false, nil)，其余错误原样上报。
func IsDir(path string) (bool, error) {
	info, ok, err := statPath(path)
	if err != nil {
		return false, fmt.Errorf("fsutil: 读取 %s 的状态: %w", path, err)
	}
	return ok && info.IsDir(), nil
}

// EnsureParent 创建 filePath 的父目录，父目录已存在时直接返回 nil。
//
// 参数按文件路径处理：EnsureParent("/a/b/") 创建的是 "/a/b"，即
// filepath.Dir 的结果。文件路径没有目录部分（如 "a.txt"）时不做任何操作。
func EnsureParent(filePath string) error {
	if filePath == "" {
		return errors.New("fsutil: 文件路径不能为空")
	}
	dir := filepath.Dir(filePath)
	if dir == "." {
		return nil
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("fsutil: 创建父目录 %s: %w", dir, err)
	}
	return nil
}

// ErrOutsideBase 表示 JoinWithin 的 name 越出了基准目录。
var ErrOutsideBase = errors.New("fsutil: 路径越出基准目录")

// JoinWithin 把 name 作为相对路径拼到 base 下，并拒绝越出 base 的路径。
//
// 这是纯词法校验：拒绝绝对路径、空串和 .. 越界；不解析符号链接。
// 需要解析级防护时请使用 os.Root。Windows 上还会拒绝保留设备名。
func JoinWithin(base, name string) (string, error) {
	if base == "" {
		return "", errors.New("fsutil: 基准目录不能为空")
	}
	if !filepath.IsLocal(name) {
		return "", fmt.Errorf("%w: %q", ErrOutsideBase, name)
	}
	return filepath.Join(base, name), nil
}

// UniqueName 在 dir 下为 name 生成不冲突的文件名，返回完整路径。
//
// name 必须是单个路径段；函数依次探测 name、stem_1.ext、stem_2.ext……
// 最多 10000 次。检查使用 os.Lstat，目录、断链符号链接等任何已存在的
// 条目都算占用。检查与创建之间存在竞态，函数只保证"当时不存在"；
// 需要强保证时请调用方用 O_EXCL 打开。通常先调用 SanitizeFileName。
func UniqueName(dir, name string) (string, error) {
	if dir == "" {
		return "", errors.New("fsutil: 目录不能为空")
	}
	if name == "" || name == "." || name == ".." || strings.ContainsAny(name, `\/`) {
		return "", fmt.Errorf("fsutil: 文件名必须是单个路径段: %q", name)
	}

	stem, ext := splitNameExt(name)
	for i := range maxUniqueAttempts {
		candidate := name
		if i > 0 {
			candidate = fmt.Sprintf("%s_%d%s", stem, i, ext)
		}
		path := filepath.Join(dir, candidate)
		_, err := os.Lstat(path)
		if errors.Is(err, fs.ErrNotExist) {
			return path, nil
		}
		if err != nil {
			return "", fmt.Errorf("fsutil: 检查文件名 %s: %w", path, err)
		}
	}
	return "", fmt.Errorf("fsutil: 在 %s 下找不到可用文件名（尝试 %d 次）", dir, maxUniqueAttempts)
}

// Copy 把单个普通文件复制到 dst。
//
// src 必须是普通文件（不跟随符号链接）；dst 是完整目标文件路径，
// 父目录不存在时自动创建。目标已存在时报错不覆盖，错误满足
// errors.Is(err, fs.ErrExist)。复制失败会删除本次创建的目标文件，
// 源文件不受影响。目标权限精确还原为源文件的权限位；不保留 mtime。
// 在 Windows 上 os.Chmod 只能切换只读属性，权限还原以平台能力为限。
func Copy(src, dst string) error {
	return copyFile(src, dst)
}

// Move 把单个普通文件移动到 dst。
//
// src 必须是普通文件（不跟随符号链接）；dst 是完整目标文件路径，
// 父目录不存在时自动创建。目标已存在时报错不覆盖，错误满足
// errors.Is(err, fs.ErrExist)。先尝试 os.Rename；失败时回退为
// Copy + 删除源文件，以覆盖跨设备场景。复制成功但删除源失败时，
// 保留目标文件并返回明确错误。与 os.Rename 一样，目标存在检查与
// 重命名之间存在竞态窗口。
func Move(src, dst string) error {
	return moveFile(src, dst)
}

// ListFiles 递归列出 root 下所有普通文件的绝对路径。
//
// root 必须是目录；root 本身是符号链接时跟随。遍历不跟随任何子目录
// 符号链接，符号链接、设备、FIFO、socket 都不计入结果。返回路径按
// 字典序全局排序；没有文件时返回非 nil 空切片。任一层读取失败立即
// 返回错误，不返回部分结果。
func ListFiles(root string) ([]string, error) {
	return listFiles(root)
}

// SanitizeFileName 把任意字符串清洗成单个跨平台安全的文件名。
//
// 规则在所有平台一致执行：路径分隔符、C0/C1 控制字符、Windows 非法字符、
// 双向控制字符替换为下划线；去掉首尾空格与结尾的点；Windows 保留设备名
// 前加下划线；空结果回退为 "unnamed"。不做长度截断与 Unicode 归一化。
func SanitizeFileName(name string) string {
	return sanitizeName(name)
}
