package masking_test

import (
	"testing"

	"github.com/corenca/teatool-go/masking"
)

const digits = "0123456789"

func mustDetector(t *testing.T, pattern string, opts ...masking.RegexOption) masking.Detector {
	t.Helper()

	detector, err := masking.Regex(pattern, opts...)
	if err != nil {
		t.Fatalf("编译正则 %q 失败: %v", pattern, err)
	}
	return detector
}

func TestRegexMatches(t *testing.T) {
	detector := mustDetector(t, `\d{3}`)

	matches := detector("a123b456")
	if len(matches) != 2 {
		t.Fatalf("命中数 = %d, 期望 2", len(matches))
	}

	first := matches[0]
	if first.Start != 1 || first.End != 4 || first.Value != "123" {
		t.Fatalf("首个命中 = %+v, 期望 {Start:1 End:4 Value:\"123\"}", first)
	}
}

func TestRegexBoundary(t *testing.T) {
	detector := mustDetector(t, `\d{11}`,
		masking.NotPrecededBy(digits),
		masking.NotFollowedBy(digits),
	)

	if got := len(detector("a13812345678b")); got != 1 {
		t.Fatalf("字母包裹的手机号应命中，命中数 = %d", got)
	}

	if got := len(detector("138123456789")); got != 0 {
		t.Fatalf("12 位连续数字不应命中 11 位号码，命中数 = %d", got)
	}
}

func TestRegexUnicodeBoundary(t *testing.T) {
	detector := mustDetector(t, `\d+`, masking.NotPrecededBy(digits))

	matches := detector("中123")
	if len(matches) != 1 {
		t.Fatalf("命中数 = %d, 期望 1", len(matches))
	}

	// 字节下标：中文占 3 字节
	if matches[0].Start != 3 {
		t.Fatalf("Start = %d, 期望 3", matches[0].Start)
	}
}

func TestRegexIgnoresZeroWidth(t *testing.T) {
	detector := mustDetector(t, `\d*`)

	if got := len(detector("abc")); got != 0 {
		t.Fatalf("零宽命中应被忽略，命中数 = %d", got)
	}
}

func TestRegexInvalidPattern(t *testing.T) {
	if _, err := masking.Regex("["); err == nil {
		t.Fatal("非法正则应返回错误")
	}
}
