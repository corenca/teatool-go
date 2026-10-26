package s3

import "testing"

// TestNormalizeRegion 校验 region 归一化：OSS 的 oss- 前缀要去掉，空值回退。
func TestNormalizeRegion(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"oss-cn-hangzhou", "cn-hangzhou"},
		{"cn-southwest-2", "cn-southwest-2"},
		{"  oss-cn-chengdu ", "cn-chengdu"},
		{"", "us-east-1"},
	}

	for _, tc := range cases {
		got := normalizeRegion(tc.in)
		t.Logf("region %q -> %q", tc.in, got)
		if got != tc.want {
			t.Fatalf("normalizeRegion(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestNormalizeEndpoint 校验 endpoint 归一化：去尾部斜杠、补 scheme。
func TestNormalizeEndpoint(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"https://cos.ap-chengdu.myqcloud.com/", "https://cos.ap-chengdu.myqcloud.com"},
		{"cos.ap-chengdu.myqcloud.com", "https://cos.ap-chengdu.myqcloud.com"},
		{"  https://obs.cn-southwest-2.myhuaweicloud.com  ", "https://obs.cn-southwest-2.myhuaweicloud.com"},
		{"", ""},
	}

	for _, tc := range cases {
		got := normalizeEndpoint(tc.in)
		t.Logf("endpoint %q -> %q", tc.in, got)
		if got != tc.want {
			t.Fatalf("normalizeEndpoint(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestEndpointForBucket 校验含桶前缀的 endpoint 会剥掉桶标签（COS 常见形态）。
func TestEndpointForBucket(t *testing.T) {
	cases := []struct {
		endpoint string
		bucket   string
		want     string
	}{
		{
			endpoint: "https://fbs-1314047435.cos.ap-chengdu.myqcloud.com",
			bucket:   "fbs-1314047435",
			want:     "https://cos.ap-chengdu.myqcloud.com",
		},
		{
			endpoint: "https://obs.cn-southwest-2.myhuaweicloud.com",
			bucket:   "rionca",
			want:     "https://obs.cn-southwest-2.myhuaweicloud.com",
		},
		{endpoint: "", bucket: "b", want: ""},
	}

	for _, tc := range cases {
		got := endpointForBucket(tc.endpoint, tc.bucket)
		t.Logf("endpoint %q bucket %q -> %q", tc.endpoint, tc.bucket, got)
		if got != tc.want {
			t.Fatalf("endpointForBucket(%q, %q) = %q, want %q", tc.endpoint, tc.bucket, got, tc.want)
		}
	}
}
