// Package filesplit 提供按行切分大文件的能力。
//
// 切分面向"超大业务文件分发给多台机器并行处理"的场景：整个文件不会读入
// 内存，行内容与行尾（\n 或 \r\n）按字节原样拷贝，不重排、不转码。
//
// 两种切分入口共用同一套行语义：
//
//   - 最后一段内容即使没有换行符也计为一行，这与 wc -l 只数换行符的口径不同。
//   - 空文件视为 0 行，切分时不创建任何子文件。
//
// 子文件命名为 <原名>_part_<序号><扩展名>，序号补零到总份数的位数，
// 保证按文件名排序与切分顺序一致。
package filesplit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
)

// bufferSize 是读取块与 bufio 缓冲的统一大小。
//
// 实测 188MB / 200 万行文件（page cache 命中）：读取块从 4KB 提升到 64KB
// 约快 2.2 倍，64KB 之后基本饱和；切分的 bufio 缓冲取 64KB 以上可避免默认
// 4KB 带来的多次小读写。256KB 与 64KB、1MB 的差距都在 10% 以内，取折中值。
const bufferSize = 256 << 10

// CountLines 统计文件的逻辑行数。
//
// 文件末尾没有换行符的最后一段内容同样计为一行；空文件返回 0。
// 统计按块读取，内存占用与文件大小无关。
func CountLines(path string) (int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	buf := make([]byte, bufferSize)
	var (
		lines    int64
		hasData  bool
		lastByte byte
	)
	for {
		n, err := f.Read(buf)
		if n > 0 {
			hasData = true
			lines += int64(bytes.Count(buf[:n], []byte{'\n'}))
			lastByte = buf[n-1]
		}
		if err != nil {
			if errors.Is(err, io.EOF) {
				break
			}
			return 0, err
		}
	}
	if hasData && lastByte != '\n' {
		lines++
	}
	return lines, nil
}

// SplitByLines 按行数切分 src：每 linesPerFile 行写一个子文件。
//
// 子文件写入 dstDir（不存在时自动创建），返回按序号排列的绝对路径列表。
// 源文件为 0 行时不生成子文件，返回空列表。切分中途失败会删除本次已写出
// 的子文件，不会留下残缺集合。
func SplitByLines(src, dstDir string, linesPerFile int) ([]string, error) {
	if linesPerFile < 1 {
		return nil, fmt.Errorf("filesplit: 每份行数必须大于 0，得到 %d", linesPerFile)
	}
	if dstDir == "" {
		return nil, errors.New("filesplit: 输出目录不能为空")
	}

	total, err := CountLines(src)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return []string{}, nil
	}

	sizes := make([]int64, 0, (total+int64(linesPerFile)-1)/int64(linesPerFile))
	for remaining := total; remaining > 0; {
		size := min(int64(linesPerFile), remaining)
		sizes = append(sizes, size)
		remaining -= size
	}
	return writeParts(src, dstDir, sizes)
}

// SplitByParts 把 src 均分为 parts 份，每份写一个子文件。
//
// 行数不能整除时，余数从靠前的份开始各多 1 行，任意两份的行数相差不超过 1。
// parts 大于总行数时实际只产出总行数个非空子文件，不会生成空文件。
// 其余行为与 SplitByLines 一致：自动创建 dstDir、序号补零、返回绝对路径、
// 中途失败清理已写出的子文件。
func SplitByParts(src, dstDir string, parts int) ([]string, error) {
	if parts < 1 {
		return nil, fmt.Errorf("filesplit: 份数必须大于 0，得到 %d", parts)
	}
	if dstDir == "" {
		return nil, errors.New("filesplit: 输出目录不能为空")
	}

	total, err := CountLines(src)
	if err != nil {
		return nil, err
	}
	if total == 0 {
		return []string{}, nil
	}

	actual := parts
	if int64(actual) > total {
		actual = int(total)
	}
	base := total / int64(actual)
	rem := int(total % int64(actual))

	sizes := make([]int64, actual)
	for i := range sizes {
		sizes[i] = base
		if i < rem {
			sizes[i]++
		}
	}
	return writeParts(src, dstDir, sizes)
}
