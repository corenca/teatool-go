package s3

import (
	"net/url"
	"strings"
)

// normalizeRegion 归一化 region：去掉 OSS 的 oss- 前缀，空值回退 us-east-1。
func normalizeRegion(region string) string {
	region = strings.TrimSpace(region)
	if region == "" {
		return "us-east-1"
	}
	return strings.TrimPrefix(region, "oss-")
}

// normalizeEndpoint 归一化 endpoint：去空白与尾部斜杠，缺少 scheme 时补 https。
func normalizeEndpoint(endpoint string) string {
	endpoint = strings.TrimSpace(endpoint)
	if endpoint == "" {
		return ""
	}
	endpoint = strings.TrimRight(endpoint, "/")
	if !strings.Contains(endpoint, "://") {
		endpoint = "https://" + endpoint
	}
	return endpoint
}

// endpointForBucket 剥掉 endpoint 中与桶同名的子域名标签。
func endpointForBucket(endpoint, bucket string) string {
	if endpoint == "" || bucket == "" {
		return endpoint
	}
	u, err := url.Parse(endpoint)
	if err != nil || u.Host == "" {
		return endpoint
	}
	if !strings.HasPrefix(u.Host, bucket+".") {
		return endpoint
	}
	return u.Scheme + "://" + strings.TrimPrefix(u.Host, bucket+".")
}
