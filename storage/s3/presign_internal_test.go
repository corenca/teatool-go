package s3

import "testing"

// TestContentDisposition 固定 RFC 6266 头的生成规则：ASCII 回退 + filename*。
func TestContentDisposition(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"report.pdf", `attachment; filename="report.pdf"; filename*=UTF-8''report.pdf`},
		{"报告.pdf", `attachment; filename="__.pdf"; filename*=UTF-8''%E6%8A%A5%E5%91%8A.pdf`},
		{`a"b.txt`, `attachment; filename="a_b.txt"; filename*=UTF-8''a%22b.txt`},
		{"100% 完成.txt", `attachment; filename="100% __.txt"; filename*=UTF-8''100%25%20%E5%AE%8C%E6%88%90.txt`},
	}
	for _, tc := range cases {
		got := contentDisposition(tc.name)
		t.Logf("contentDisposition(%q) = %s", tc.name, got)
		if got != tc.want {
			t.Fatalf("got  %s\nwant %s", got, tc.want)
		}
	}
}
