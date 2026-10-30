package excel_test

import (
	"bytes"
	"testing"

	"github.com/xuri/excelize/v2"
)

// xlsxBytes 用 Excelize 构造内存中的 xlsx，供读取测试使用。
//
// build 拿到的是已含默认工作表 "Sheet1" 的工作簿；写完后统一关闭并返回字节。
func xlsxBytes(t *testing.T, build func(f *excelize.File)) []byte {
	t.Helper()
	f := excelize.NewFile()
	build(f)
	var buf bytes.Buffer
	if err := f.Write(&buf); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// readBack 把生成的 xlsx 读成二维字符串，用于断言写出结果。
func readBack(t *testing.T, data []byte) [][]string {
	t.Helper()
	f, err := excelize.OpenReader(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	name := f.GetSheetName(0)
	rows, err := f.GetRows(name)
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
