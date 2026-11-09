package excel_test

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/excel"
	"github.com/xuri/excelize/v2"
)

// tolIssueRow 是结构体层容错测试用的目标类型。
type tolIssueRow struct {
	No     string  `excel:"单号,required"`
	Amount float64 `excel:"金额,required"`
	Count  int     `excel:"数量,required"`
}

// tolColumns 返回与 tolIssueRow 对应的 map 层列声明。
func tolColumns() []excel.Column {
	return []excel.Column{
		{Header: "单号", Key: "no", Required: true},
		{Header: "金额", Key: "amount", Type: excel.Float, Required: true},
		{Header: "数量", Key: "count", Type: excel.Int, Required: true},
	}
}

// tolSheet 构造三行数据：好行、多字段错误行、好行。
func tolSheet(t *testing.T) []byte {
	return xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"单号", "金额", "数量"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"A001", "12.5", "3"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"", "abc", "x"})
		f.SetSheetRow("Sheet1", "A4", &[]any{"A003", "9", "1"})
	})
}

// 容错读取：一行多个单元格出错时汇总成一次回调，好行保留、坏行跳过。
func TestReadContinueAggregatesCells(t *testing.T) {
	var issues []*excel.RowError
	opts := excel.ReadOptions{
		Columns: tolColumns(),
		OnRowError: func(e *excel.RowError) error {
			issues = append(issues, e)
			return nil
		},
	}
	rows, err := readAll(t, bytes.NewReader(tolSheet(t)), opts)
	if err != nil {
		t.Fatalf("容错模式不应返回错误: %v", err)
	}
	t.Logf("读到的行: %v", rows)
	t.Logf("收集到的错误: %v", issues)
	if len(rows) != 2 || rows[0]["no"] != "A001" || rows[1]["no"] != "A003" {
		t.Fatalf("好行应全部保留: %v", rows)
	}
	if len(issues) != 1 {
		t.Fatalf("一行多个错误应只回调一次，得到 %d 次", len(issues))
	}
	issue := issues[0]
	if issue.Row != 3 || issue.Column != "" {
		t.Fatalf("行号或列名不符: %+v", issue)
	}
	if len(issue.Cells) != 3 || issue.Cells[0] != "" || issue.Cells[1] != "abc" || issue.Cells[2] != "x" {
		t.Fatalf("原始单元格不符: %v", issue.Cells)
	}
	for _, want := range []string{"no", "amount", "count", "必填列为空"} {
		if !strings.Contains(issue.Error(), want) {
			t.Fatalf("汇总信息缺少 %q: %v", want, issue)
		}
	}
}

// 单列出错：保留 Column，Cells 仍保留整行。
func TestReadContinueSingleCellKeepsColumn(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"单号", "金额", "数量"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"A001", "abc", "3"})
	})
	var issues []*excel.RowError
	opts := excel.ReadOptions{
		Columns: tolColumns(),
		OnRowError: func(e *excel.RowError) error {
			issues = append(issues, e)
			return nil
		},
	}
	if _, err := readAll(t, bytes.NewReader(data), opts); err != nil {
		t.Fatalf("容错模式不应返回错误: %v", err)
	}
	if len(issues) != 1 || issues[0].Column != "amount" ||
		len(issues[0].Cells) != 3 || issues[0].Cells[0] != "A001" {
		t.Fatalf("单列错误信息不符: %+v", issues)
	}
}

// 无表头模式下单元格数超出声明的行同样可跳过，后续行继续读取。
func TestReadContinueWidthMismatch(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"A001", "12.5"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"A002", "13", "多余"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"A003", "9"})
	})
	var issues []*excel.RowError
	opts := excel.ReadOptions{
		NoHeader: true,
		Columns: []excel.Column{
			{Header: "单号", Key: "no"},
			{Header: "金额", Key: "amount"},
		},
		OnRowError: func(e *excel.RowError) error {
			issues = append(issues, e)
			return nil
		},
	}
	rows, err := readAll(t, bytes.NewReader(data), opts)
	if err != nil {
		t.Fatalf("容错模式不应返回错误: %v", err)
	}
	t.Logf("读到的行: %v；错误: %v", rows, issues)
	if len(rows) != 2 || len(issues) != 1 {
		t.Fatalf("应跳过 1 行、保留 2 行: %v / %v", rows, issues)
	}
	if issues[0].Row != 2 || !errors.Is(issues[0], excel.ErrColumnMismatch) ||
		len(issues[0].Cells) != 3 {
		t.Fatalf("列数错误信息不符: %+v", issues[0])
	}
}

// 处理器返回错误时立即中止，并原样返回该错误。
func TestOnRowErrorAborts(t *testing.T) {
	stop := errors.New("错误太多")
	calls := 0
	opts := excel.ReadOptions{
		Columns: tolColumns(),
		OnRowError: func(*excel.RowError) error {
			calls++
			return stop
		},
	}
	rows, err := readAll(t, bytes.NewReader(tolSheet(t)), opts)
	t.Logf("错误: %v；处理器调用 %d 次", err, calls)
	if !errors.Is(err, stop) {
		t.Fatalf("应原样返回处理器错误，得到 %v", err)
	}
	if calls != 1 {
		t.Fatalf("中止后不应继续回调，实际 %d 次", calls)
	}
	if rows != nil {
		t.Fatalf("中止时 Read 应返回 nil，得到 %v", rows)
	}
}

// 未配置 OnRowError 时快速失败，但同一行的多个错误也会汇总。
func TestReadFailFastAggregatesCells(t *testing.T) {
	_, err := readAll(t, bytes.NewReader(tolSheet(t)), excel.ReadOptions{Columns: tolColumns()})
	var issue *excel.RowError
	if !errors.As(err, &issue) {
		t.Fatalf("期望 *RowError，得到 %v", err)
	}
	t.Logf("错误: %v", issue)
	for _, want := range []string{"no", "amount", "count"} {
		if !strings.Contains(issue.Error(), want) {
			t.Fatalf("快速失败也应汇总，缺少 %q: %v", want, issue)
		}
	}
	if issue.Row != 3 || issue.Column != "" || len(issue.Cells) != 3 {
		t.Fatalf("多列错误应清空 Column 并保留 Cells: %+v", issue)
	}
}

// 结构体层容错：多个字段错误汇总成一次，好行保留。
func TestReadIntoContinueAggregatesCells(t *testing.T) {
	var issues []*excel.RowError
	rows, err := readIntoData[tolIssueRow](t, tolSheet(t), excel.ReadOptions{
		OnRowError: func(e *excel.RowError) error {
			issues = append(issues, e)
			return nil
		},
	})
	if err != nil {
		t.Fatalf("容错模式不应返回错误: %v", err)
	}
	t.Logf("读到的行: %+v；错误: %v", rows, issues)
	if len(rows) != 2 || rows[0].No != "A001" || rows[1].Count != 1 {
		t.Fatalf("好行应全部保留: %+v", rows)
	}
	if len(issues) != 1 {
		t.Fatalf("应汇总为一次回调，得到 %d 次", len(issues))
	}
	for _, want := range []string{"单号", "金额", "数量"} {
		if !strings.Contains(issues[0].Error(), want) {
			t.Fatalf("汇总信息缺少 %q: %v", want, issues[0])
		}
	}
	if issues[0].Row != 3 || len(issues[0].Cells) != 3 {
		t.Fatalf("行号或原始单元格不符: %+v", issues[0])
	}
}

// tolCheckedRow 记录 Validate 是否被调用。
type tolCheckedRow struct {
	Count int `excel:"数量"`
}

var tolValidateCalls int

func (r tolCheckedRow) Validate() error {
	tolValidateCalls++
	return nil
}

// 解码已失败的行不再调用结构体 Validate；正常行照常调用。
func TestReadIntoSkipsValidateOnDecodeError(t *testing.T) {
	tolValidateCalls = 0
	bad := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"数量"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"abc"})
	})
	rows, err := readIntoData[tolCheckedRow](t, bad, excel.ReadOptions{
		OnRowError: func(*excel.RowError) error { return nil },
	})
	t.Logf("坏行结果: rows=%+v err=%v Validate 调用=%d", rows, err, tolValidateCalls)
	if err != nil || len(rows) != 0 {
		t.Fatalf("坏行应被跳过: %v / %+v", err, rows)
	}
	if tolValidateCalls != 0 {
		t.Fatalf("解码失败时不应调用 Validate，实际 %d 次", tolValidateCalls)
	}

	good := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"数量"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"3"})
	})
	rows, err = readIntoData[tolCheckedRow](t, good, excel.ReadOptions{
		OnRowError: func(*excel.RowError) error { return nil },
	})
	t.Logf("好行结果: rows=%+v err=%v Validate 调用=%d", rows, err, tolValidateCalls)
	if err != nil || len(rows) != 1 || tolValidateCalls != 1 {
		t.Fatalf("好行应调用一次 Validate: %v / %+v / %d", err, rows, tolValidateCalls)
	}
}

// ReadOptions.Validate 的错误也进入容错通道，并带原始单元格。
func TestReadIntoContinueValidateOptionError(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"单号", "金额", "数量"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"A001", "12.5", "3"})
		f.SetSheetRow("Sheet1", "A3", &[]any{"A002", "13", "2"})
		f.SetSheetRow("Sheet1", "A4", &[]any{"A003", "9", "1"})
	})
	var issues []*excel.RowError
	calls := 0
	rows, err := readIntoData[tolIssueRow](t, data, excel.ReadOptions{
		Validate: func(_ int, row map[string]string) error {
			calls++
			if row["金额"] == "13" {
				return errors.New("金额不能是 13")
			}
			return nil
		},
		OnRowError: func(e *excel.RowError) error {
			issues = append(issues, e)
			return nil
		},
	})
	t.Logf("读到的行: %+v；错误: %v", rows, issues)
	if err != nil || len(rows) != 2 || rows[0].No != "A001" || rows[1].No != "A003" {
		t.Fatalf("校验失败的行应跳过: %v / %+v", err, rows)
	}
	if len(issues) != 1 || !strings.Contains(issues[0].Error(), "金额不能是 13") ||
		issues[0].Row != 3 || len(issues[0].Cells) != 3 {
		t.Fatalf("校验错误信息不符: %+v", issues)
	}
	if calls != 3 {
		t.Fatalf("三个解码成功的行都应调用 Validate，实际 %d 次", calls)
	}
}

// 表头级错误不受 OnRowError 影响，仍然直接返回。
func TestReadContinueHeaderErrorFatal(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"单号", "金额"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"A001", "1"})
	})
	calls := 0
	opts := excel.ReadOptions{
		Columns: tolColumns(), // 缺少「数量」列
		OnRowError: func(*excel.RowError) error {
			calls++
			return nil
		},
	}
	_, err := readAll(t, bytes.NewReader(data), opts)
	t.Logf("错误: %v；处理器调用 %d 次", err, calls)
	if !errors.Is(err, excel.ErrMissingColumn) {
		t.Fatalf("表头缺列应直接返回 ErrMissingColumn，得到 %v", err)
	}
	if calls != 0 {
		t.Fatalf("表头错误不应进入 OnRowError，实际 %d 次", calls)
	}
}

// 原始单元格不进入 Error()，避免日志整体打印时泄漏整行数据。
func TestRowErrorCellsNotInMessage(t *testing.T) {
	data := xlsxBytes(t, func(f *excelize.File) {
		f.SetSheetRow("Sheet1", "A1", &[]any{"单号", "金额", "数量"})
		f.SetSheetRow("Sheet1", "A2", &[]any{"李四", "abc", "3"})
	})
	var issues []*excel.RowError
	opts := excel.ReadOptions{
		Columns: tolColumns(),
		OnRowError: func(e *excel.RowError) error {
			issues = append(issues, e)
			return nil
		},
	}
	if _, err := readAll(t, bytes.NewReader(data), opts); err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || issues[0].Cells[0] != "李四" {
		t.Fatalf("原始单元格不符: %+v", issues)
	}
	if strings.Contains(issues[0].Error(), "李四") {
		t.Fatalf("整行数据不应进入错误信息: %v", issues[0])
	}
}

// readIntoData 从内存 xlsx 读取第一个 sheet 并映射成结构体。
func readIntoData[T any](t *testing.T, data []byte, opts excel.ReadOptions) ([]T, error) {
	t.Helper()
	r, err := excel.Open(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	s, err := r.Sheet("")
	if err != nil {
		return nil, errors.Join(err, r.Close())
	}
	out, err := s.ReadInto[T](opts)
	return out, errors.Join(err, r.Close())
}
