package filesplit

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// writeParts 按 sizes 指定的每份行数顺序写出子文件，返回绝对路径列表。
// 任一步失败都会删除本次已写出的子文件。
func writeParts(src, dstDir string, sizes []int64) ([]string, error) {
	f, err := os.Open(src)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	absDir, err := filepath.Abs(dstDir)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(absDir, 0o755); err != nil {
		return nil, err
	}

	base, ext := splitName(filepath.Base(src))
	// 补零宽度取总份数的十进制位数，保证按文件名排序与切分顺序一致。
	width := len(strconv.Itoa(len(sizes)))
	reader := bufio.NewReaderSize(f, bufferSize)
	paths := make([]string, 0, len(sizes))

	for i, size := range sizes {
		path := filepath.Join(absDir, fmt.Sprintf("%s_part_%0*d%s", base, width, i+1, ext))
		paths = append(paths, path)
		if err := writeFilePart(reader, path, size); err != nil {
			removeFiles(paths)
			return nil, err
		}
	}
	return paths, nil
}

// writeFilePart 从 reader 读取 size 行写入 path，行内容与行尾原样保留。
func writeFilePart(reader *bufio.Reader, path string, size int64) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() {
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}()

	w := bufio.NewWriterSize(f, bufferSize)
	if err := copyLines(reader, w, size); err != nil {
		return err
	}
	return w.Flush()
}

// copyLines 从 reader 读取 size 行写入 w，行内容与行尾原样保留。
func copyLines(reader *bufio.Reader, w *bufio.Writer, size int64) error {
	for i := range size {
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			if _, err := w.Write(line); err != nil {
				return err
			}
		}
		if readErr != nil {
			// 最后一行没有换行符时 ReadBytes 也会返回 EOF，属于正常结束。
			if errors.Is(readErr, io.EOF) && i == size-1 {
				return nil
			}
			if errors.Is(readErr, io.EOF) {
				return fmt.Errorf("filesplit: 源文件在切分期间被截断: %w", io.ErrUnexpectedEOF)
			}
			return readErr
		}
	}
	return nil
}

// splitName 分离文件名主干与扩展名；点开头的文件名（如 .env）视为无扩展名。
func splitName(name string) (base, ext string) {
	ext = filepath.Ext(name)
	base = strings.TrimSuffix(name, ext)
	if base == "" {
		return name, ""
	}
	return base, ext
}

// removeFiles 尽力删除已写出的子文件。
func removeFiles(paths []string) {
	for _, path := range paths {
		os.Remove(path)
	}
}
