package dsv_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/dsv"
)

// 容错读取：一行里多个字段出错时汇总成一次回调；好行保留，坏行跳过。
func TestReadContinueAggregatesFields(t *testing.T) {
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		Columns: []dsv.Column{
			{Header: "姓名", Key: "name"},
			{Header: "年龄", Key: "age", Type: dsv.Int, Required: true},
			{Header: "分数", Key: "score", Type: dsv.Float, Required: true},
		},
	}
	var issues []*dsv.RowError
	opts.OnRowError = func(e *dsv.RowError) error {
		issues = append(issues, e)
		return nil
	}

	rows, err := dsv.Read(strings.NewReader(
		"姓名!^年龄!^分数\n张三!^30!^90\n李四!^abc!^\n王五!^40!^80\n"), opts)
	if err != nil {
		t.Fatalf("容错模式不应返回错误: %v", err)
	}
	t.Logf("读到的行: %#v", rows)
	t.Logf("收集到的错误: %v", issues)
	if len(rows) != 2 || rows[0]["name"] != "张三" || rows[1]["name"] != "王五" {
		t.Fatalf("好行应全部保留: %#v", rows)
	}
	if len(issues) != 1 {
		t.Fatalf("一行多个字段错误应只回调一次，得到 %d 次", len(issues))
	}
	issue := issues[0]
	if issue.Line != 3 || issue.Text != "李四!^abc!^" {
		t.Fatalf("行号或原文不符: %+v", issue)
	}
	if issue.Column != "" {
		t.Fatalf("多列出错时 Column 应为空，得到 %q", issue.Column)
	}
	for _, want := range []string{"age", "score", "必填字段为空"} {
		if !strings.Contains(issue.Error(), want) {
			t.Fatalf("汇总信息缺少 %q: %v", want, issue)
		}
	}
	if strings.Contains(issue.Error(), "李四") {
		t.Fatalf("原始行不应进入错误信息: %v", issue)
	}
}

// 单列出错：保留 Column，方便业务方按列统计。
func TestReadContinueSingleFieldKeepsColumn(t *testing.T) {
	var issues []*dsv.RowError
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		Columns: []dsv.Column{
			{Header: "姓名", Key: "name"},
			{Header: "年龄", Key: "age", Type: dsv.Int},
		},
		OnRowError: func(e *dsv.RowError) error {
			issues = append(issues, e)
			return nil
		},
	}
	if _, err := dsv.Read(strings.NewReader("姓名!^年龄\n李四!^abc\n"), opts); err != nil {
		t.Fatalf("容错模式不应返回错误: %v", err)
	}
	if len(issues) != 1 || issues[0].Column != "age" || issues[0].Text != "李四!^abc" {
		t.Fatalf("单列错误信息不符: %+v", issues)
	}
}

// 列数不符的坏行同样交给处理器，后续行继续读取。
func TestReadContinueWidthMismatch(t *testing.T) {
	var issues []*dsv.RowError
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		Columns: []dsv.Column{
			{Header: "姓名", Key: "name"},
			{Header: "年龄", Key: "age"},
		},
		OnRowError: func(e *dsv.RowError) error {
			issues = append(issues, e)
			return nil
		},
	}
	rows, err := dsv.Read(strings.NewReader(
		"姓名!^年龄\n张三!^30\n李四!^40!^多余\n王五!^50\n"), opts)
	if err != nil {
		t.Fatalf("容错模式不应返回错误: %v", err)
	}
	t.Logf("读到的行: %#v；错误: %v", rows, issues)
	if len(rows) != 2 || len(issues) != 1 {
		t.Fatalf("应跳过 1 行、保留 2 行: %#v / %#v", rows, issues)
	}
	if issues[0].Line != 3 || issues[0].Text != "李四!^40!^多余" ||
		!errors.Is(issues[0], dsv.ErrColumnMismatch) {
		t.Fatalf("列数错误信息不符: %+v", issues[0])
	}
}

// 处理器返回错误时立即中止，并原样返回该错误。
func TestOnRowErrorAborts(t *testing.T) {
	stop := errors.New("错误太多")
	calls := 0
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		Columns: []dsv.Column{
			{Header: "姓名", Key: "name"},
			{Header: "年龄", Key: "age", Type: dsv.Int},
		},
		OnRowError: func(*dsv.RowError) error {
			calls++
			return stop
		},
	}
	rows, err := dsv.Read(strings.NewReader(
		"姓名!^年龄\n张三!^abc\n李四!^40\n"), opts)
	t.Logf("错误: %v；处理器调用 %d 次", err, calls)
	if !errors.Is(err, stop) {
		t.Fatalf("应原样返回处理器错误，得到 %v", err)
	}
	if calls != 1 {
		t.Fatalf("中止后不应继续回调，实际 %d 次", calls)
	}
	if rows != nil {
		t.Fatalf("中止时 Read 应返回 nil，得到 %#v", rows)
	}
}

// 非容错模式下 RowError 也带原始行，但原文不出现在 Error() 中。
func TestReadFailFastCarriesText(t *testing.T) {
	opts := dsv.ReadOptions{Delimiter: "!^", Columns: []dsv.Column{
		{Header: "姓名", Key: "name"},
		{Header: "年龄", Key: "age", Type: dsv.Int},
	}}
	_, err := dsv.Read(strings.NewReader("姓名!^年龄\n李四!^abc\n"), opts)
	var issue *dsv.RowError
	if !errors.As(err, &issue) {
		t.Fatalf("期望 *RowError，得到 %v", err)
	}
	t.Logf("错误: %v；原文: %q", issue, issue.Text)
	if issue.Text != "李四!^abc" || issue.Line != 2 {
		t.Fatalf("原文或行号不符: %+v", issue)
	}
	if strings.Contains(issue.Error(), "李四") {
		t.Fatalf("原文不应进入错误信息: %v", issue)
	}
}

// issueScoreRow 用于结构体层的多字段错误汇总。
type issueScoreRow struct {
	Name  string  `dsv:"姓名,required"`
	Age   int     `dsv:"年龄,required"`
	Score float64 `dsv:"分数,required"`
}

// 结构体层容错：多个字段错误汇总成一次，好行保留。
func TestReadIntoContinueAggregates(t *testing.T) {
	var issues []*dsv.RowError
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		OnRowError: func(e *dsv.RowError) error {
			issues = append(issues, e)
			return nil
		},
	}
	rows, err := dsv.ReadInto[issueScoreRow](strings.NewReader(
		"姓名!^年龄!^分数\n张三!^30!^90\n!^abc!^x\n王五!^40!^80\n"), opts)
	if err != nil {
		t.Fatalf("容错模式不应返回错误: %v", err)
	}
	t.Logf("读到的行: %#v；错误: %v", rows, issues)
	if len(rows) != 2 || rows[0].Name != "张三" || rows[1].Name != "王五" {
		t.Fatalf("好行应全部保留: %#v", rows)
	}
	if len(issues) != 1 {
		t.Fatalf("应汇总为一次回调，得到 %d 次", len(issues))
	}
	for _, want := range []string{"姓名", "年龄", "分数"} {
		if !strings.Contains(issues[0].Error(), want) {
			t.Fatalf("汇总信息缺少 %q: %v", want, issues[0])
		}
	}
	if issues[0].Line != 3 || issues[0].Text != "!^abc!^x" {
		t.Fatalf("行号或原文不符: %+v", issues[0])
	}
}

// checkedRow 记录 Validate 是否被调用。
type checkedRow struct {
	Age int `dsv:"年龄"`
}

var checkedValidateCalls int

func (r checkedRow) Validate() error {
	checkedValidateCalls++
	return nil
}

// 解码已失败的行不再调用号结构体 Validate；正常行照常调用。
func TestReadIntoSkipsValidateOnDecodeError(t *testing.T) {
	checkedValidateCalls = 0
	opts := dsv.ReadOptions{
		Delimiter:  "!^",
		OnRowError: func(*dsv.RowError) error { return nil },
	}
	rows, err := dsv.ReadInto[checkedRow](strings.NewReader("年龄\nabc\n"), opts)
	t.Logf("坏行结果: rows=%#v err=%v Validate 调用=%d", rows, err, checkedValidateCalls)
	if err != nil || len(rows) != 0 {
		t.Fatalf("坏行应被跳过: %v / %#v", err, rows)
	}
	if checkedValidateCalls != 0 {
		t.Fatalf("解码失败时不应调用 Validate，实际 %d 次", checkedValidateCalls)
	}

	rows, err = dsv.ReadInto[checkedRow](strings.NewReader("年龄\n30\n"), opts)
	t.Logf("好行结果: rows=%#v err=%v Validate 调用=%d", rows, err, checkedValidateCalls)
	if err != nil || len(rows) != 1 || checkedValidateCalls != 1 {
		t.Fatalf("好行应调用一次 Validate: %v / %#v / %d", err, rows, checkedValidateCalls)
	}
}

// ReadOptions.Validate 的错误也进入容错通道，并带原始行。
func TestReadIntoContinueValidateOptionError(t *testing.T) {
	var issues []*dsv.RowError
	validateCalls := 0
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		Validate: func(_ int, row map[string]string) error {
			validateCalls++
			if row["年龄"] == "13" {
				return errors.New("年龄不能是 13")
			}
			return nil
		},
		OnRowError: func(e *dsv.RowError) error {
			issues = append(issues, e)
			return nil
		},
	}
	rows, err := dsv.ReadInto[issueScoreRow](strings.NewReader(
		"姓名!^年龄!^分数\n张三!^13!^90\n李四!^40!^80\n"), opts)
	t.Logf("读到的行: %#v；错误: %v", rows, issues)
	if err != nil || len(rows) != 1 || rows[0].Name != "李四" {
		t.Fatalf("校验失败的行应跳过: %v / %#v", err, rows)
	}
	if len(issues) != 1 || !strings.Contains(issues[0].Error(), "年龄不能是 13") ||
		issues[0].Text != "张三!^13!^90" {
		t.Fatalf("校验错误信息不符: %+v", issues)
	}
	if validateCalls != 2 {
		t.Fatalf("每个解码成功的行都应调用 Validate，实际 %d 次", validateCalls)
	}
}

// 解码失败的行不调用 ReadOptions.Validate。
func TestReadIntoSkipsValidateOptionOnDecodeError(t *testing.T) {
	calls := 0
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		Validate: func(int, map[string]string) error {
			calls++
			return nil
		},
		OnRowError: func(*dsv.RowError) error { return nil },
	}
	if _, err := dsv.ReadInto[issueScoreRow](strings.NewReader(
		"姓名!^年龄!^分数\n张三!^abc!^90\n"), opts); err != nil {
		t.Fatalf("容错模式不应返回错误: %v", err)
	}
	t.Logf("Validate 调用次数: %d", calls)
	if calls != 0 {
		t.Fatalf("解码失败的行不应调用 Validate，实际 %d 次", calls)
	}
}

// 表头级错误不受 OnRowError 影响，仍然直接返回。
func TestReadContinueHeaderErrorFatal(t *testing.T) {
	calls := 0
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		Columns: []dsv.Column{
			{Header: "姓名", Key: "name"},
			{Header: "生日", Key: "birth"},
		},
		OnRowError: func(*dsv.RowError) error {
			calls++
			return nil
		},
	}
	_, err := dsv.Read(strings.NewReader("姓名!^年龄\n张三!^30\n"), opts)
	t.Logf("错误: %v；处理器调用 %d 次", err, calls)
	if !errors.Is(err, dsv.ErrMissingColumn) {
		t.Fatalf("表头缺列应直接返回 ErrMissingColumn，得到 %v", err)
	}
	if calls != 0 {
		t.Fatalf("表头错误不应进入 OnRowError，实际 %d 次", calls)
	}
}

// 未配置 OnRowError 时仍然快速失败，但同一行的多个字段错误也会汇总。
func TestReadFailFastAggregatesFields(t *testing.T) {
	opts := dsv.ReadOptions{Delimiter: "!^", Columns: []dsv.Column{
		{Header: "姓名", Key: "name"},
		{Header: "年龄", Key: "age", Type: dsv.Int, Required: true},
		{Header: "分数", Key: "score", Type: dsv.Float, Required: true},
	}}
	_, err := dsv.Read(strings.NewReader("姓名!^年龄!^分数\n李四!^abc!^\n"), opts)
	var issue *dsv.RowError
	if !errors.As(err, &issue) {
		t.Fatalf("期望 *RowError，得到 %v", err)
	}
	t.Logf("错误: %v", issue)
	for _, want := range []string{"age", "score"} {
		if !strings.Contains(issue.Error(), want) {
			t.Fatalf("快速失败也应汇总，缺少 %q: %v", want, issue)
		}
	}
	if issue.Column != "" || issue.Text != "李四!^abc!^" {
		t.Fatalf("多列错误应清空 Column 并保留原文: %+v", issue)
	}
}

// 回调返回普通错误表示业务失败，不进入容错通道。
func TestOnRowErrorIgnoresCallbackError(t *testing.T) {
	boom := errors.New("业务处理失败")
	calls := 0
	opts := dsv.ReadOptions{
		Delimiter: "!^",
		Columns: []dsv.Column{
			{Header: "姓名", Key: "name"},
			{Header: "年龄", Key: "age"},
		},
		OnRowError: func(*dsv.RowError) error {
			calls++
			return nil
		},
	}
	err := dsv.Each(strings.NewReader("姓名!^年龄\n张三!^30\n"), opts,
		func(int, map[string]string) error { return boom })
	t.Logf("错误: %v；处理器调用 %d 次", err, calls)
	if !errors.Is(err, boom) {
		t.Fatalf("应原样保留回调错误，得到 %v", err)
	}
	var issue *dsv.RowError
	if !errors.As(err, &issue) || issue.Line != 2 {
		t.Fatalf("回调错误仍应带行号: %+v", issue)
	}
	if calls != 0 {
		t.Fatalf("业务错误不应交给 OnRowError，实际 %d 次", calls)
	}
}
