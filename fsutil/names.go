package fsutil

import "strings"

// fallbackFileName 是清洗结果为空时使用的确定性文件名。
const fallbackFileName = "unnamed"

// maxUniqueAttempts 限制 UniqueName 的探测次数，避免异常目录无限循环。
const maxUniqueAttempts = 10000

// reservedNames 是 Windows 保留设备名，匹配时大小写不敏感。
var reservedNames = map[string]struct{}{
	"CON": {}, "PRN": {}, "AUX": {}, "NUL": {},
	"COM1": {}, "COM2": {}, "COM3": {}, "COM4": {}, "COM5": {},
	"COM6": {}, "COM7": {}, "COM8": {}, "COM9": {},
	"LPT1": {}, "LPT2": {}, "LPT3": {}, "LPT4": {}, "LPT5": {},
	"LPT6": {}, "LPT7": {}, "LPT8": {}, "LPT9": {},
}

// sanitizeName 把任意字符串清洗成单个跨平台安全的文件名。
func sanitizeName(name string) string {
	if name == "" {
		return fallbackFileName
	}

	var b strings.Builder
	b.Grow(len(name))
	for _, r := range name {
		if unsafeFileNameRune(r) {
			b.WriteByte('_')
			continue
		}
		b.WriteRune(r)
	}

	// 一次性去掉结尾的空格与点，避免 "a. ." 这类组合残留。
	s := strings.TrimLeft(b.String(), " ")
	s = strings.TrimRightFunc(s, func(r rune) bool { return r == '.' || r == ' ' })
	if s == "" {
		return fallbackFileName
	}

	// 保留名判断只看第一个点之前的部分；前导点（如 .env）取整个名字。
	// Windows 会忽略组名末尾的空格与点，因此检查前先裁掉。
	stem := s
	if i := strings.IndexByte(s, '.'); i > 0 {
		stem = s[:i]
	}
	stem = strings.TrimRight(stem, " .")
	if _, ok := reservedNames[strings.ToUpper(stem)]; ok {
		s = "_" + s
	}
	return s
}

// unsafeFileNameRune 报告 r 是否必须替换为下划线。
func unsafeFileNameRune(r rune) bool {
	switch {
	case r == '/' || r == '\\':
		return true
	case r < 0x20 || r == 0x7F:
		return true
	case r >= 0x80 && r <= 0x9F:
		return true
	case r == '<' || r == '>' || r == ':' || r == '"' || r == '|' || r == '?' || r == '*':
		return true
	case r >= 0x202A && r <= 0x202E:
		return true
	case r >= 0x2066 && r <= 0x2069:
		return true
	}
	return false
}

// splitNameExt 拆分文件名与扩展名。
//
// 只有最后一个点不在开头、也不在结尾时才算扩展名；因此 ".env" 没有
// 扩展名，"a.tar.gz" 的扩展名是 ".gz"。
func splitNameExt(name string) (stem, ext string) {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 || i == len(name)-1 {
		return name, ""
	}
	return name[:i], name[i:]
}
