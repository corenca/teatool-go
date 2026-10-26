package s3

import (
	"context"
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/corenca/teatool-go/storage"
)

// PresignGet 生成下载预签名 URL；FileName 通过响应头覆写下载文件名。
func (c *Client) PresignGet(ctx context.Context, bucket, key string, opts storage.PresignOptions) (storage.PresignedURL, error) {
	if err := validatePresign(opts, http.MethodGet); err != nil {
		return storage.PresignedURL{}, err
	}

	input := &awss3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if opts.FileName != "" {
		input.ResponseContentDisposition = aws.String(fmt.Sprintf("attachment; filename=%q", opts.FileName))
	}

	presigned, err := awss3.NewPresignClient(c.sdk).PresignGetObject(ctx, input, presignOptions(opts))
	if err != nil {
		return storage.PresignedURL{}, mapError(err)
	}
	return storage.PresignedURL{URL: presigned.URL}, nil
}

// PresignPut 生成上传预签名 URL。
//
// 注意：SDK 的 PresignPutObject 会剥离 Content-Type 请求头，故预签名上传无法
// 约束对象的 Content-Type，对象类型由服务端或上传请求自行决定。
func (c *Client) PresignPut(ctx context.Context, bucket, key string, opts storage.PresignOptions) (storage.PresignedURL, error) {
	if err := validatePresign(opts, http.MethodPut); err != nil {
		return storage.PresignedURL{}, err
	}

	input := &awss3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}

	presigned, err := awss3.NewPresignClient(c.sdk).PresignPutObject(ctx, input, presignOptions(opts))
	if err != nil {
		return storage.PresignedURL{}, mapError(err)
	}
	return storage.PresignedURL{URL: presigned.URL}, nil
}

// validatePresign 校验有效期与按方法区分的可选参数。
func validatePresign(opts storage.PresignOptions, method string) error {
	if opts.Expires <= 0 || opts.Expires > storage.MaxPresignExpiry {
		return fmt.Errorf("%w: 预签名有效期 %v 必须在 (0, %v] 内",
			storage.ErrInvalidArgument, opts.Expires, storage.MaxPresignExpiry)
	}
	if method == http.MethodPut && opts.FileName != "" {
		return fmt.Errorf("%w: FileName 仅对 PresignGet 有效", storage.ErrInvalidArgument)
	}
	return nil
}

// presignOptions 把有效期传给 SDK。
func presignOptions(opts storage.PresignOptions) func(*awss3.PresignOptions) {
	return func(o *awss3.PresignOptions) {
		o.Expires = opts.Expires
	}
}
