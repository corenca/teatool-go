package dsv_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/dsv"
)

// CRLF 文件：最后一列不得残留 \r；最后一行没有换行也能读。
func TestReadCRLFAndNoFinalNewline(t *testing.T) {
	rows, err := dsv.Read(strings.NewReader("姓名!^年龄\r\n张三!^30\r\n李四!^40"), headerOptions())
	if err != nil {
		t.Fatalf("CRLF 读取失败: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if len(rows) != 2 || rows[1]["age"] != "40" || strings.Contains(rows[1]["age"], "\r") {
		t.Fatalf("CRLF 或末行无换行处理不符: %#v", rows)
	}
}

// 文件开头 UTF-8 BOM：第一列表头与值不被污染。
func TestReadBOM(t *testing.T) {
	rows, err := dsv.Read(strings.NewReader("\xEF\xBB\xBF姓名!^年龄\n张三!^30\n"), headerOptions())
	if err != nil {
		t.Fatalf("带 BOM 的文件读取失败: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if len(rows) != 1 || rows[0]["name"] != "张三" {
		t.Fatalf("BOM 未被剥离: %#v", rows)
	}
}

// SkipRows 跳过标题行；跳过计数包含空行，行号仍然是物理行号。
func TestReadSkipRows(t *testing.T) {
	opts := headerOptions()
	opts.SkipRows = 2
	var gotLine int
	err := dsv.Each(strings.NewReader("导出时间:2026-11-09\n\n姓名!^年龄\n张三!^30\n"), opts,
		func(line int, _ map[string]string) error {
			gotLine = line
			return nil
		})
	if err != nil {
		t.Fatalf("跳过行读取失败: %v", err)
	}
	t.Logf("数据行物理行号: %d", gotLine)
	if gotLine != 4 {
		t.Fatalf("数据行号应为 4，得到 %d", gotLine)
	}
}

// TrailingDelimiter=true：每行（含表头）以分隔符结尾时正常读取。
func TestReadTrailingDelimiter(t *testing.T) {
	opts := headerOptions()
	opts.TrailingDelimiter = true
	rows, err := dsv.Read(strings.NewReader("姓名!^年龄!^\n张三!^30!^\n"), opts)
	if err != nil {
		t.Fatalf("末尾分隔符读取失败: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if len(rows) != 1 || rows[0]["name"] != "张三" || rows[0]["age"] != "30" {
		t.Fatalf("末尾分隔符处理不符: %#v", rows)
	}
}

// TrailingDelimiter=true：某行缺少结尾分隔符，返回 *RowError，
// errors.Is(err, dsv.ErrIllegalField)，行号精确。
func TestReadTrailingDelimiterMissing(t *testing.T) {
	opts := headerOptions()
	opts.TrailingDelimiter = true
	_, err := dsv.Read(strings.NewReader("姓名!^年龄!^\n张三!^30\n"), opts)
	t.Logf("错误: %v", err)
	if !errors.Is(err, dsv.ErrIllegalField) {
		t.Fatalf("期望 ErrIllegalField，得到 %v", err)
	}
	var rowErr *dsv.RowError
	if !errors.As(err, &rowErr) || rowErr.Line != 2 {
		t.Fatalf("期望第 2 行的 *RowError，得到 %v", err)
	}
}

// TrailingDelimiter=true + TrimSpace=true：行尾在分隔符后还有空格也能通过。
func TestReadTrailingDelimiterWithTrimSpace(t *testing.T) {
	opts := headerOptions()
	opts.TrailingDelimiter = true
	opts.TrimSpace = true
	rows, err := dsv.Read(strings.NewReader("姓名!^年龄!^\n张三!^30!^  \n"), opts)
	if err != nil {
		t.Fatalf("行尾空白不应导致失败: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if len(rows) != 1 || rows[0]["age"] != "30" {
		t.Fatalf("TrimSpace 与末尾分隔符顺序不符: %#v", rows)
	}
}

// TrimSpace=true：Text 字段的首尾空白被去掉；为 false 时原样保留。
func TestReadTrimSpace(t *testing.T) {
	input := "姓名!^年龄\n 张三 !^ 30 \n"

	trimmed := headerOptions()
	trimmed.TrimSpace = true
	rows, err := dsv.Read(strings.NewReader(input), trimmed)
	if err != nil {
		t.Fatalf("TrimSpace 读取失败: %v", err)
	}
	t.Logf("TrimSpace=true 读到: %#v", rows)
	if rows[0]["name"] != "张三" || rows[0]["age"] != "30" {
		t.Fatalf("空白未被去掉: %#v", rows)
	}

	kept, err := dsv.Read(strings.NewReader(input), headerOptions())
	if err != nil {
		t.Fatalf("原样读取失败: %v", err)
	}
	t.Logf("TrimSpace=false 读到: %#v", kept)
	if kept[0]["name"] != " 张三 " || kept[0]["age"] != " 30 " {
		t.Fatalf("空白不应被去掉: %#v", kept)
	}
}

// 文件只有空行：有表头模式 ErrEmptyFile，无表头模式空结果。
func TestReadBlankOnlyFile(t *testing.T) {
	if _, err := dsv.Read(strings.NewReader("\n\n\n"), headerOptions()); !errors.Is(err, dsv.ErrEmptyFile) {
		t.Fatalf("有表头纯空行文件期望 ErrEmptyFile，得到 %v", err)
	}

	opts := dsv.ReadOptions{Delimiter: "!^", NoHeader: true, Columns: []dsv.Column{{Key: "a"}}}
	rows, err := dsv.Read(strings.NewReader("\n\n\n"), opts)
	if err != nil {
		t.Fatalf("无表头纯空行文件不应报错: %v", err)
	}
	if len(rows) != 0 {
		t.Fatalf("应返回空结果，得到 %#v", rows)
	}
}

// TrailingDelimiter=true + TrimSpace=true：末尾分隔符后是全角空格也应通过。
func TestReadTrailingDelimiterWithUnicodeSpace(t *testing.T) {
	opts := headerOptions()
	opts.TrailingDelimiter = true
	opts.TrimSpace = true
	rows, err := dsv.Read(strings.NewReader("姓名!^年龄!^\n张三!^30!^\u3000\u3000\n"), opts)
	if err != nil {
		t.Fatalf("全角空白不应导致末尾分隔符校验失败: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if len(rows) != 1 || rows[0]["age"] != "30" {
		t.Fatalf("全角空白处理不符: %#v", rows)
	}
}

// TrailingDelimiter=true + TrimSpace=true：分隔符本身以空格结尾时不能被预裁剪吃掉。
func TestReadTrailingDelimiterEndsWithSpace(t *testing.T) {
	opts := dsv.ReadOptions{
		Delimiter:         " | ",
		TrailingDelimiter: true,
		TrimSpace:         true,
		Columns:           []dsv.Column{{Header: "a", Key: "a"}, {Header: "b", Key: "b"}},
	}
	rows, err := dsv.Read(strings.NewReader("a | b | \n1 | 2 | \n"), opts)
	if err != nil {
		t.Fatalf("以空格结尾的分隔符不应被预裁剪破坏: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if rows[0]["a"] != "1" || rows[0]["b"] != "2" {
		t.Fatalf("分隔符以空格结尾时结果不符: %#v", rows)
	}
}

// 分隔符自身以空白结尾、行尾还有额外空白：仍应识别出末尾分隔符。
func TestReadTrailingDelimiterEndsWithSpaceAndExtraSpaces(t *testing.T) {
	opts := dsv.ReadOptions{
		Delimiter:         " | ",
		TrailingDelimiter: true,
		TrimSpace:         true,
		Columns:           []dsv.Column{{Header: "a", Key: "a"}, {Header: "b", Key: "b"}},
	}
	rows, err := dsv.Read(strings.NewReader("a | b |   \n1 | 2 |   \n"), opts)
	if err != nil {
		t.Fatalf("分隔符与行尾都含空白时不应失败: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if rows[0]["a"] != "1" || rows[0]["b"] != "2" {
		t.Fatalf("结果不符: %#v", rows)
	}
}

// BOM 只在第一物理行剥离：后续数据行里的 BOM 应原样保留。
func TestReadBOMOnlyFirstLine(t *testing.T) {
	rows, err := dsv.Read(strings.NewReader("\xEF\xBB\xBF姓名!^年龄\n张\xEF\xBB\xBF三!^30\n"), headerOptions())
	if err != nil {
		t.Fatalf("读取失败: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	if rows[0]["name"] != "张\xEF\xBB\xBF三" {
		t.Fatalf("数据行中的 BOM 不应被剥离: %q", rows[0]["name"])
	}
}
