package filesplit_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/filesplit"
)

// numberedLines 生成 n 行内容，每行形如 "line1\n"。
func numberedLines(n int) string {
	var b strings.Builder
	for i := range n {
		fmt.Fprintf(&b, "line%d\n", i+1)
	}
	return b.String()
}

func TestSplitByParts(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		parts     int
		wantFiles []string
	}{
		{
			name:      "十行切三份余数靠前",
			content:   numberedLines(10),
			parts:     3,
			wantFiles: []string{"line1\nline2\nline3\nline4\n", "line5\nline6\nline7\n", "line8\nline9\nline10\n"},
		},
		{
			name:      "整除均分",
			content:   numberedLines(12),
			parts:     3,
			wantFiles: []string{"line1\nline2\nline3\nline4\n", "line5\nline6\nline7\nline8\n", "line9\nline10\nline11\nline12\n"},
		},
		{
			name:      "份数多于行数不产生空文件",
			content:   numberedLines(3),
			parts:     5,
			wantFiles: []string{"line1\n", "line2\n", "line3\n"},
		},
		{
			name:      "每行一份",
			content:   numberedLines(5),
			parts:     5,
			wantFiles: []string{"line1\n", "line2\n", "line3\n", "line4\n", "line5\n"},
		},
		{
			name:      "保留 CRLF 行尾",
			content:   "a\r\nb\r\nc\r\n",
			parts:     2,
			wantFiles: []string{"a\r\nb\r\n", "c\r\n"},
		},
		{
			name:      "最后一行无换行符",
			content:   "a\nb\nc",
			parts:     2,
			wantFiles: []string{"a\nb\n", "c"},
		},
		{
			name:      "空文件不产生子文件",
			content:   "",
			parts:     3,
			wantFiles: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			src := writeFile(t, dir, "data.txt", tt.content)

			paths, err := filesplit.SplitByParts(src, filepath.Join(dir, "out"), tt.parts)
			if err != nil {
				t.Fatalf("SplitByParts 返回错误: %v", err)
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

func TestSplitByPartsNames(t *testing.T) {
	t.Run("按份数补零", func(t *testing.T) {
		dir := t.TempDir()
		src := writeFile(t, dir, "data.txt", numberedLines(100))

		paths, err := filesplit.SplitByParts(src, filepath.Join(dir, "out"), 10)
		if err != nil {
			t.Fatalf("SplitByParts 返回错误: %v", err)
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

	t.Run("文件名边界", func(t *testing.T) {
		tests := []struct {
			name     string
			srcName  string
			wantName string
		}{
			{name: "保留扩展名", srcName: "report.csv", wantName: "report_part_1.csv"},
			{name: "无扩展名", srcName: "data", wantName: "data_part_1"},
			{name: "多段扩展名", srcName: "a.b.csv", wantName: "a.b_part_1.csv"},
			{name: "点开头视为无扩展名", srcName: ".env", wantName: ".env_part_1"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				dir := t.TempDir()
				src := writeFile(t, dir, tt.srcName, "a\nb\n")

				paths, err := filesplit.SplitByParts(src, filepath.Join(dir, "out"), 2)
				if err != nil {
					t.Fatalf("SplitByParts 返回错误: %v", err)
				}
				if len(paths) != 2 {
					t.Fatalf("返回 %d 个子文件，期望 2", len(paths))
				}
				if got := filepath.Base(paths[0]); got != tt.wantName {
					t.Fatalf("子文件名 = %q, 期望 %q", got, tt.wantName)
				}
			})
		}
	})
}

func TestSplitByPartsErrors(t *testing.T) {
	dir := t.TempDir()
	src := writeFile(t, dir, "data.txt", "1\n2\n")
	sub := filepath.Join(dir, "sub")
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatalf("创建目录失败: %v", err)
	}

	tests := []struct {
		name  string
		src   string
		dst   string
		parts int
	}{
		{name: "份数为零", src: src, dst: filepath.Join(dir, "out"), parts: 0},
		{name: "份数为负", src: src, dst: filepath.Join(dir, "out"), parts: -1},
		{name: "输出目录为空", src: src, dst: "", parts: 1},
		{name: "源文件不存在", src: filepath.Join(dir, "missing.txt"), dst: filepath.Join(dir, "out"), parts: 1},
		{name: "源路径是目录", src: sub, dst: filepath.Join(dir, "out"), parts: 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			paths, err := filesplit.SplitByParts(tt.src, tt.dst, tt.parts)
			if err == nil {
				t.Fatal("期望返回错误，得到 nil")
			}
			if len(paths) != 0 {
				t.Fatalf("出错时不应返回子文件，得到 %v", paths)
			}
		})
	}
}
