package excel

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/xuri/excelize/v2"
)

// convertValue 按列声明校验并规范化一个非空单元格值。
//
// 空值由调用方短路，不进这里。日期类型要求 raw 是 Excel 序列号，
// date1904 来自工作簿属性。
func convertValue(col Column, value string, date1904 bool) (string, error) {
	if value == "" {
		return "", nil
	}
	if col.Type != Text {
		value = strings.TrimSpace(value)
	}
	switch col.Type {
	case Text:
		return value, nil
	case Int:
		n, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return "", conversionError(col, value, err)
		}
		return strconv.FormatInt(n, 10), nil
	case Float:
		n, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return "", conversionError(col, value, err)
		}
		return strconv.FormatFloat(n, 'f', -1, 64), nil
	case Bool:
		switch strings.ToUpper(value) {
		case "TRUE", "1":
			return "true", nil
		case "FALSE", "0":
			return "false", nil
		default:
			return "", conversionError(col, value, fmt.Errorf("不是布尔值"))
		}
	case Date, DateTime, Time:
		serial, err := strconv.ParseFloat(value, 64)
		if err != nil {
			return "", conversionError(col, value, err)
		}
		t, err := excelize.ExcelDateToTime(serial, date1904)
		if err != nil {
			return "", conversionError(col, value, err)
		}
		return t.Format(col.Layout), nil
	default:
		return "", conversionError(col, value, fmt.Errorf("未知类型 %q", col.Type))
	}
}

// conversionError 生成截断原值后的错误，避免把整格数据写进日志。
func conversionError(col Column, value string, err error) error {
	return fmt.Errorf("列 %q 的值 %q 无法解析为 %s: %w", col.Key, truncate(value, 64), col.Type, err)
}

// truncate 按字节截断字符串，用于错误信息中的原值摘要。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
