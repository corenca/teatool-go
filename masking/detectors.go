package masking

// 内置检测器与它们依赖的正则。此文件只放实现细节，对外 API 见 masking.go。

const (
	digitChars = "0123456789"

	// 邮箱本地部分的合法字符集合，作为左边界断言使用。
	emailLocalChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.+-"
	// 邮箱右侧不允许紧邻的字符集合。
	emailRightChars = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789_.-"

	ipv4Boundary = "0123456789."
	ipv6Boundary = "0123456789abcdefABCDEF:"

	emailPattern = "[A-Za-z0-9.!#$%&'*+/=?^_`{|}~-]+@[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?(?:\\.[A-Za-z0-9](?:[A-Za-z0-9-]{0,61}[A-Za-z0-9])?)+"

	ipv4Pattern = `(?:25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)(?:\.(?:25[0-5]|2[0-4]\d|1\d{2}|[1-9]?\d)){3}`

	// ipv6Pattern 为简化实现，不覆盖内嵌 IPv4 的形态（如 ::ffff:1.2.3.4）。
	ipv6Pattern = `(?i)(?:(?:[0-9a-f]{1,4}:){7}[0-9a-f]{1,4}|(?:[0-9a-f]{1,4}:){1,7}:|(?:[0-9a-f]{1,4}:){1,6}:[0-9a-f]{1,4}|(?:[0-9a-f]{1,4}:){1,5}(?::[0-9a-f]{1,4}){1,2}|(?:[0-9a-f]{1,4}:){1,4}(?::[0-9a-f]{1,4}){1,3}|(?:[0-9a-f]{1,4}:){1,3}(?::[0-9a-f]{1,4}){1,4}|(?:[0-9a-f]{1,4}:){1,2}(?::[0-9a-f]{1,4}){1,5}|[0-9a-f]{1,4}:(?:(?::[0-9a-f]{1,4}){1,6})|:(?:(?::[0-9a-f]{1,4}){1,7}|:))`
)

// 自动识别用的检测器。只有能从自由文本中可靠定位的类型才配有检测器，
// 姓名、地址等依赖上下文的信息仍须按类型名显式脱敏。
var (
	phoneDetector = mustRegex(
		`1[3-9]\d{9}`,
		NotPrecededBy(digitChars),
		NotFollowedBy(digitChars),
	)

	telephoneDetector = mustRegex(
		`(?:0\d{2,3}[- ]?\d{7,8}|400[- ]?\d{3}[- ]?\d{4})`,
		NotPrecededBy(digitChars),
		NotFollowedBy(digitChars),
	)

	idCardDetector = mustRegex(
		`(?:\d{17}[\dXx]|\d{15})`,
		NotPrecededBy(digitChars),
		NotFollowedBy(digitChars),
	)

	// 银行卡号先由正则定位 13~19 位数字，再用 Luhn 校验过滤伪号。
	bankCardDetector = luhnFilter(mustRegex(
		`\d{13,19}`,
		NotPrecededBy(digitChars),
		NotFollowedBy(digitChars),
	))

	emailDetector = mustRegex(
		emailPattern,
		NotPrecededBy(emailLocalChars),
		NotFollowedBy(emailRightChars),
	)

	ipDetector = combine(
		mustRegex(ipv4Pattern, NotPrecededBy(ipv4Boundary), NotFollowedBy(ipv4Boundary)),
		mustRegex(ipv6Pattern, NotPrecededBy(ipv6Boundary), NotFollowedBy(ipv6Boundary)),
	)
)

// mustRegex 用于装配内置检测器，正则非法属于编码错误，构造期直接 panic。
func mustRegex(pattern string, opts ...RegexOption) Detector {
	detector, err := Regex(pattern, opts...)
	if err != nil {
		panic("masking: 内置正则非法: " + err.Error())
	}
	return detector
}

// combine 把多个检测器的命中合并为一个检测器。
func combine(detectors ...Detector) Detector {
	return func(text string) []Match {
		var matches []Match
		for _, detector := range detectors {
			matches = append(matches, detector(text)...)
		}
		return matches
	}
}

// luhnFilter 过滤未通过 Luhn 校验的候选命中。
func luhnFilter(detector Detector) Detector {
	return func(text string) []Match {
		matches := detector(text)
		kept := matches[:0]
		for _, match := range matches {
			if luhnValid(match.Value) {
				kept = append(kept, match)
			}
		}
		return kept
	}
}

// luhnValid 判断纯数字串是否通过 Luhn 校验（ISO/IEC 7812）。
//
// 仅能排除偶然写错的号码，不能验证号码真实存在或属于某发卡行。
func luhnValid(number string) bool {
	if len(number) < 2 {
		return false
	}

	total := 0
	double := false
	for i := len(number) - 1; i >= 0; i-- {
		digit := number[i]
		if digit < '0' || digit > '9' {
			return false
		}
		value := int(digit - '0')
		if double {
			value *= 2
			if value > 9 {
				value -= 9
			}
		}
		total += value
		double = !double
	}
	return total%10 == 0
}
