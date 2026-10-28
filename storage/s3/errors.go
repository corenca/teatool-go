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

// mapError 把 SDK 错误映射为契约哨兵，其余错误原样返回。先按 API 错误码匹配，
// 再按 HTTP 状态码兜底：部分 S3 兼容端点只返回状态码，不带 XML 错误体。
func mapError(err error) error {
	if err == nil {
		return nil
	}
	var code string
	if apiErr, ok := errors.AsType[smithy.APIError](err); ok {
		code = apiErr.ErrorCode()
	}
	if sentinel := sentinelForCode(code); sentinel != nil {
		return fmt.Errorf("%w: %w", sentinel, err)
	}
	if responseErr, ok := errors.AsType[*smithyhttp.ResponseError](err); ok {
		if sentinel := sentinelForCode(responseErr.HTTPStatusCode()); sentinel != nil {
			return fmt.Errorf("%w: %w", sentinel, err)
		}
	}
	return err
}

// sentinelForCode 把错误码或 HTTP 状态码映射成契约哨兵。
func sentinelForCode(code any) error {
	switch code {
	case "NoSuchKey", "NotFound", 404:
		return storage.ErrObjectNotFound
	case "NoSuchBucket":
		return storage.ErrBucketNotFound
	case "AccessDenied", "Forbidden", "InvalidAccessKeyId", "SignatureDoesNotMatch", 403:
		return storage.ErrAccessDenied
	case "PreconditionFailed", 412:
		return storage.ErrPreconditionFailed
	case "InvalidRange", 416:
		return storage.ErrInvalidRange
	case "NotImplemented", 501:
		return storage.ErrUnsupported
	default:
		return nil
	}
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
