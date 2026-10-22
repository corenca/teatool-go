package fsutil_test

import (
	"testing"

	"github.com/corenca/teatool-go/fsutil"
)

func TestSanitizeFileName(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"空串", "", "unnamed"},
		{"单点", ".", "unnamed"},
		{"双点", "..", "unnamed"},
		{"纯点", "...", "unnamed"},
		{"路径分隔符", `a/b\c.txt`, "a_b_c.txt"},
		{"C0 控制字符", "a\x00\x1fb", "a__b"},
		{"DEL", "a\x7fb", "a_b"},
		{"C1 控制字符", "a\u0080\u009fb", "a__b"},
		{"Windows 非法字符", `a<b>c:d"e|f?g*h.txt`, "a_b_c_d_e_f_g_h.txt"},
		{"双向控制字符", "a\u202eb\u2066c", "a_b_c"},
		{"首尾空格", " a.txt ", "a.txt"},
		{"结尾点", "a.txt...", "a.txt"},
		{"前导点保留", ".env", ".env"},
		{"中间点保留", "a..b.txt", "a..b.txt"},
		{"保留名", "CON", "_CON"},
		{"保留名带扩展", "con.txt", "_con.txt"},
		{"保留名与扩展名之间有空格", "CON .txt", "_CON .txt"},
		{"保留名 COM9", "COM9.log", "_COM9.log"},
		{"保留名 COM1 与扩展名之间有空格", "COM1 .log", "_COM1 .log"},
		{"COM0 不是保留名", "COM0", "COM0"},
		{"中文与内部空格", "中文 文件名.txt", "中文 文件名.txt"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := fsutil.SanitizeFileName(tc.in); got != tc.want {
				t.Fatalf("SanitizeFileName(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
