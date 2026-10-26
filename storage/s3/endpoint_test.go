package s3

import (
	"context"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// TestBucketEndpointResolver 校验带桶前缀的 endpoint 在解析时被剥离，
// 虚拟托管寻址后主机名仍是原来的单桶域名。
func TestBucketEndpointResolver(t *testing.T) {
	const endpoint = "https://fbs-1314047435.cos.ap-chengdu.myqcloud.com"
	resolver := bucketEndpointResolver{
		endpoint: endpoint,
		base:     awss3.NewDefaultEndpointResolverV2(),
	}

	got, err := resolver.ResolveEndpoint(context.Background(), awss3.EndpointParameters{
		Bucket:   aws.String("fbs-1314047435"),
		Region:   aws.String("ap-chengdu"),
		Endpoint: aws.String(endpoint),
	})
	if err != nil {
		t.Fatalf("ResolveEndpoint 失败: %v", err)
	}
	t.Logf("解析结果 = %s", got.URI.String())
	if want := "fbs-1314047435.cos.ap-chengdu.myqcloud.com"; got.URI.Host != want {
		t.Fatalf("host = %q, want %q", got.URI.Host, want)
	}
}
