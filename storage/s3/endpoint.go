package s3

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	smithyendpoints "github.com/aws/smithy-go/endpoints"
)

// bucketEndpointResolver 在解析端点时剥掉 endpoint 中的桶前缀标签。
//
// endpoint 带桶前缀（COS 常见形态）时，虚拟托管寻址会把桶名拼两次；按请求的
// 桶在解析阶段修正后，整个客户端只需要一个 SDK 实例。
type bucketEndpointResolver struct {
	endpoint string
	base     awss3.EndpointResolverV2
}

// ResolveEndpoint 修正桶前缀后交给 SDK 默认解析器。
func (r bucketEndpointResolver) ResolveEndpoint(ctx context.Context, params awss3.EndpointParameters) (smithyendpoints.Endpoint, error) {
	if params.Bucket != nil {
		params.Endpoint = aws.String(endpointForBucket(r.endpoint, *params.Bucket))
	}
	return r.base.ResolveEndpoint(ctx, params)
}
