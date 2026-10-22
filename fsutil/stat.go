package fsutil

import (
	"errors"
	"io/fs"
	"os"
)

// statPath 读取路径状态，并把"不存在"与"无法判断"区分开。
//
// 返回 ok=false、err=nil 表示路径确实不存在；err 非 nil 表示权限、
// 路径中间不是目录等无法判断的情况。
func statPath(path string) (fs.FileInfo, bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return info, true, nil
	}
	if errors.Is(err, fs.ErrNotExist) {
		return nil, false, nil
	}
	return nil, false, err
}
