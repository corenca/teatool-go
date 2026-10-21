package filesplit_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/filesplit"
)

func TestSplitByLines(t *testing.T) {
	tests := []struct {
		name         string
		content      string
		linesPerFile int
		wantFiles    []string
	}{
		{
			name:         "整除",
			content:      "1\n2\n3\n4\n",
			linesPerFile: 2,
			wantFiles:    []string{"1\n2\n", "3\n4\n"},
		},
		{
			name:         "有余数",
			content:      "1\n2\n3\n4\n5\n",
			linesPerFile: 2,
			wantFiles:    []string{"1\n2\n", "3\n4\n", "5\n"},
		},
		{
			name:         "每份行数超过总行数",
			content:      "1\n2\n3\n",
			linesPerFile: 10,
			wantFiles:    []string{"1\n2\n3\n"},
		},
		{
			name:         "最后一行无换行符",
			content:      "1\n2\n3",
			linesPerFile: 2,
			wantFiles:    []string{"1\n2\n", "3"},
		},
		{
			name:         "保留 CRLF 行尾",
			content:      "a\r\nb\r\nc\r\n",
			linesPerFile: 1,
			wantFiles:    []string{"a\r\n", "b\r\n", "c\r\n"},
		},
		{
			name:         "空行各自成行",
			content:      "\n\n\n",
			linesPerFile: 1,
			wantFiles:    []string{"\n", "\n", "\n"},
		},
		{
			name:         "空文件不产生子文件",
			content:      "",
			linesPerFile: 10,
			wantFiles:    nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := writeFile(t, dir, "data.txt", tt.content)
			dst := filepath.Join(dir, "out")

			paths, err := filesplit.SplitByLines(src, dst, tt.linesPerFile)
			if err != nil {
				t.Fatalf("SplitByLines 返回错误: %v", err)
			}
			if len(paths) != len(tt.wantFiles) {
				t.Fatalf("返回 %d 个子文件，期望 %d: %v", len(paths), len(tt.wantFiles), paths)
			}
			for i, path := range paths {
				if !filepath.IsAbs(path) {
					t.Fatalf("返回路径不是绝对路径: %s", path)
				}
				if got := readFile(t, path); got != tt.wantFiles[i] {
					t.Fatalf("第 %d 个子文件内容 = %q, 期望 %q", i+1, got, tt.wantFiles[i])
				}
			}
		})
	}
}

func TestSplitByLinesNames(t *testing.T) {
	t.Run("按总份数补零", func(t *testing.T) {
		dir := t.TempDir()
		// 100 行、每份 10 行，共 10 份，序号补零到两位。
		var content strings.Builder
		for i := range 100 {
			fmt.Fprintf(&content, "%d\n", i+1)
		}
		src := writeFile(t, dir, "data.txt", content.String())

		paths, err := filesplit.SplitByLines(src, filepath.Join(dir, "out"), 10)
		if err != nil {
			t.Fatalf("SplitByLines 返回错误: %v", err)
		}
		want := []string{
			"data_part_01.txt", "data_part_02.txt", "data_part_03.txt", "data_part_04.txt",
			"data_part_05.txt", "data_part_06.txt", "data_part_07.txt", "data_part_08.txt",
			"data_part_09.txt", "data_part_10.txt",
		}
		if got := baseNames(paths); !equalStrings(got, want) {
			t.Fatalf("子文件名 = %v, 期望 %v", got, want)
		}
	})
}

func TestSplitByLinesCreatesDir(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "data.txt", "1\n2\n")
	dst := filepath.Join(dir, "a", "b", "c")

	if _, err := filesplit.SplitByLines(src, dst, 1); err != nil {
		t.Fatalf("SplitByLines 返回错误: %v", err)
	}
	info, err := os.Stat(dst)
	if err != nil {
		t.Fatalf("输出目录未创建: %v", err)
	}
	if !info.IsDir() {
		t.Fatalf("输出路径不是目录: %s", dst)
	}
}

func TestSplitByLinesKeepsBytes(t *testing.T) {
	content := "第一行\r\n\r\n第三行没有换行符"
	dir := t.TempDir()
	src := writeFile(t, dir, "data.txt", content)

	paths, err := filesplit.SplitByLines(src, filepath.Join(dir, "out"), 2)
	if err != nil {
		t.Fatalf("SplitByLines 返回错误: %v", err)
	}
	if got := concatFiles(t, paths); got != content {
		t.Fatalf("子文件拼接 = %q, 期望与源文件一致 %q", got, content)
	}
}

func TestSplitByLinesErrors(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "data.txt", "1\n2\n")
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	tests := []struct {
		name         string
		src          string
		dst          string
		linesPerFile int
	}{
		{name: "行数为零", src: src, dst: filepath.Join(dir, "out"), linesPerFile: 0},
		{name: "行数为负", src: src, dst: filepath.Join(dir, "out"), linesPerFile: -1},
		{name: "输出目录为空", src: src, dst: "", linesPerFile: 1},
		{name: "源文件不存在", src: filepath.Join(dir, "missing.txt"), dst: filepath.Join(dir, "out"), linesPerFile: 1},
		{name: "源路径是目录", src: sub, dst: filepath.Join(dir, "out"), linesPerFile: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, err := filesplit.SplitByLines(tt.src, tt.dst, tt.linesPerFile)
			if err == nil {
				t.Fatal("期望返回错误，得到 nil")
			}
			if len(paths) != 0 {
				t.Fatalf("出错时不应返回子文件，得到 %v", paths)
			}
		})
	}
}
