package dsv

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// normalizeValue 按列声明校验并规范化一个非空字段值。
//
// 空值由调用方短路，不进这里。除 Text 外解析前会去掉首尾空白。
func normalizeValue(col Column, value string) (string, error) {
	if value == "" || col.Type == Text {
		return value, nil
	}
	text := strings.TrimSpace(value)
	switch col.Type {
	case Int:
		n, err := strconv.ParseInt(text, 10, 64)
		if err != nil {
			return "", conversionError(col, value, err)
		}
		return strconv.FormatInt(n, 10), nil
	case Float:
		n, err := strconv.ParseFloat(text, 64)
		if err != nil {
			return "", conversionError(col, value, err)
		}
		return strconv.FormatFloat(n, 'f', -1, 64), nil
	case Bool:
		b, err := parseBool(text)
		if err != nil {
			return "", conversionError(col, value, err)
		}
		return strconv.FormatBool(b), nil
	case Date, DateTime, Time:
		t, err := time.Parse(col.Layout, text)
		if err != nil {
			return "", conversionError(col, value, err)
		}
		return t.Format(col.Layout), nil
	default:
		return "", conversionError(col, value, fmt.Errorf("未知类型 %q", col.Type))
	}
}

// parseBool 接受 TRUE/FALSE（大小写不敏感）与 1/0，其余返回错误。
func parseBool(text string) (bool, error) {
	switch strings.ToUpper(text) {
	case "TRUE", "1":
		return true, nil
	case "FALSE", "0":
		return false, nil
	default:
		return false, errors.New("不是布尔值")
	}
}

// conversionError 生成截断原值后的错误，避免把整段数据写进日志。
func conversionError(col Column, value string, err error) error {
	return fmt.Errorf("值 %q 无法解析为 %s: %w", truncate(value, 64), col.Type, err)
}

// truncate 按字节截断字符串，用于错误信息中的原值摘要。
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}
