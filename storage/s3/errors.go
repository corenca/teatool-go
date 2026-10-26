package s3

import (
	"errors"
	"fmt"

	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/smithy-go"
	"github.com/aws/smithy-go/middleware"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/corenca/teatool-go/storage"
)

// mapError 把 SDK 错误映射为契约错误，其余错误原样返回。
func mapError(err error) error {
	if err == nil {
		return nil
	}
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		switch apiErr.ErrorCode() {
		case "NoSuchKey", "NotFound", "404":
			return fmt.Errorf("%w: %w", storage.ErrObjectNotFound, err)
		}
	}
	return err
}

// withContentMD5 移除 SDK 默认的 CRC32 校验中间件，改用 Content-MD5 请求头。
//
// OSS 与 COS 的批量删除接口强制要求该请求头，S3 与 MinIO 同样接受。
func withContentMD5(o *awss3.Options) {
	o.APIOptions = append(o.APIOptions, func(stack *middleware.Stack) error {
		stack.Initialize.Remove("AWSChecksum:SetupInputContext")
		stack.Build.Remove("AWSChecksum:RequestMetricsTracking")
		stack.Finalize.Remove("AWSChecksum:ComputeInputPayloadChecksum")
		stack.Finalize.Remove("addInputChecksumTrailer")
		return smithyhttp.AddContentChecksumMiddleware(stack)
	})
}
