package s3

import (
	"context"
	"fmt"
	"net/http"
	"strings"

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
		input.ResponseContentDisposition = aws.String(contentDisposition(opts.FileName))
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

// contentDisposition 生成 RFC 6266 兼容的下载文件名头：ASCII 回退给旧客户端，
// filename* 用 RFC 5987 百分号编码传递中文等非 ASCII 文件名。
func contentDisposition(name string) string {
	var fallback strings.Builder
	for _, r := range name {
		if r < 0x20 || r > 0x7e || r == '"' || r == '\\' {
			fallback.WriteByte('_')
			continue
		}
		fallback.WriteRune(r)
	}
	safe := fallback.String()
	if safe == "" {
		safe = "download"
	}
	return fmt.Sprintf("attachment; filename=%q; filename*=UTF-8''%s", safe, escapeRFC5987(name))
}

// escapeRFC5987 按 RFC 5987 的 attr-char 集合保留字符，其余字节百分号编码。
func escapeRFC5987(value string) string {
	var escaped strings.Builder
	for i := range len(value) {
		c := value[i]
		if isAttrChar(c) {
			escaped.WriteByte(c)
			continue
		}
		fmt.Fprintf(&escaped, "%%%02X", c)
	}
	return escaped.String()
}

// isAttrChar 判断字节是否属于 RFC 5987 attr-char。
func isAttrChar(c byte) bool {
	switch {
	case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9':
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", c) >= 0
}

// presignOptions 把有效期传给 SDK。
func presignOptions(opts storage.PresignOptions) func(*awss3.PresignOptions) {
	return func(o *awss3.PresignOptions) {
		o.Expires = opts.Expires
	}
}
