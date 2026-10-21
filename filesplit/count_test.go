package filesplit_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/filesplit"
)

func TestCountLines(t *testing.T) {
	tests := []struct {
		name    string
		content string
		want    int64
	}{
		{name: "末尾有换行", content: "a\nb\nc\n", want: 3},
		{name: "末尾无换行", content: "a\nb\nc", want: 3},
		{name: "空文件", content: "", want: 0},
		{name: "仅一个换行", content: "\n", want: 1},
		{name: "连续空行", content: "\n\n", want: 2},
		{name: "中间含空行", content: "a\n\nc\n", want: 3},
		{name: "CRLF 行尾", content: "a\r\nb\r\n", want: 2},
		{name: "CRLF 且末尾无换行", content: "a\r\nb\r", want: 2},
		{name: "中文内容", content: "第一行\n第二行", want: 2},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			src := writeFile(t, t.TempDir(), "data.txt", tt.content)

			got, err := filesplit.CountLines(src)
			if err != nil {
				t.Fatalf("CountLines 返回错误: %v", err)
			}
			if got != tt.want {
				t.Fatalf("CountLines = %d, 期望 %d", got, tt.want)
			}
		})
	}
}

func TestCountLinesErrors(t *testing.T) {
	dir := t.TempDir()

	t.Run("文件不存在", func(t *testing.T) {
		if _, err := filesplit.CountLines(filepath.Join(dir, "missing.txt")); err == nil {
			t.Fatal("期望文件不存在的错误，得到 nil")
		}
	})

	t.Run("路径是目录", func(t *testing.T) {
		sub := filepath.Join(dir, "sub")
		if err := os.Mkdir(sub, 0o755); err != nil {
			t.Fatalf("创建目录失败: %v", err)
		}
		if _, err := filesplit.CountLines(sub); err == nil {
			t.Fatal("期望路径是目录的错误，得到 nil")
		}
	})
}

func TestCountLinesLargeFile(t *testing.T) {
	// 总大小超过 64KB 读取块，覆盖行跨块与块边界恰好落在换行符上的情况。
	var content strings.Builder
	for range 10000 {
		content.WriteString(strings.Repeat("x", 99))
		content.WriteByte('\n')
	}
	src := writeFile(t, t.TempDir(), "large.txt", content.String())

	got, err := filesplit.CountLines(src)
	if err != nil {
		t.Fatalf("CountLines 返回错误: %v", err)
	}
	if got != 10000 {
		t.Fatalf("CountLines = %d, 期望 10000", got)
	}
}
