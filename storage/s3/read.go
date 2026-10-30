package s3

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/corenca/teatool-go/storage"
)

// Stat 是 HEAD 请求，并把元数据键统一转成小写（跨厂商一致）。
func (c *Client) Stat(ctx context.Context, bucket, key string) (storage.ObjectInfo, error) {
	out, err := c.sdk.HeadObject(ctx, &awss3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return storage.ObjectInfo{}, mapError(err)
	}
	return storage.ObjectInfo{
		Key:                key,
		Size:               aws.ToInt64(out.ContentLength),
		LastModified:       aws.ToTime(out.LastModified),
		ETag:               trimETag(out.ETag),
		ContentType:        aws.ToString(out.ContentType),
		ContentDisposition: aws.ToString(out.ContentDisposition),
		CacheControl:       aws.ToString(out.CacheControl),
		StorageClass:       string(out.StorageClass),
		Metadata:           lowerMetadata(out.Metadata),
	}, nil
}

// Exists 复用 Stat（HEAD），只有 404 视为不存在。
func (c *Client) Exists(ctx context.Context, bucket, key string) (bool, error) {
	_, err := c.Stat(ctx, bucket, key)
	switch {
	case err == nil:
		return true, nil
	case errors.Is(err, storage.ErrObjectNotFound):
		return false, nil
	default:
		return false, err
	}
}

// Get 用 Range 请求头实现区间读取；响应带 Content-Range 时用其中的总大小覆盖
// Info.Size。
func (c *Client) Get(ctx context.Context, bucket, key string, opts storage.ReaderOptions) (*storage.Reader, error) {
	input := &awss3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}
	if opts.Range != nil {
		header, err := rangeHeader(*opts.Range)
		if err != nil {
			return nil, err
		}
		input.Range = aws.String(header)
	}

	out, err := c.sdk.GetObject(ctx, input)
	if err != nil {
		if opts.Range != nil && errors.Is(mapError(err), storage.ErrInvalidRange) {
			// 契约要求越界宽容：起始位置不小于对象大小时返回空流。
			info, statErr := c.Stat(ctx, bucket, key)
			if statErr != nil {
				return nil, statErr
			}
			return &storage.Reader{ReadCloser: io.NopCloser(bytes.NewReader(nil)), Info: info}, nil
		}
		return nil, mapError(err)
	}

	info := storage.ObjectInfo{
		Key:                key,
		Size:               aws.ToInt64(out.ContentLength),
		LastModified:       aws.ToTime(out.LastModified),
		ETag:               trimETag(out.ETag),
		ContentType:        aws.ToString(out.ContentType),
		ContentDisposition: aws.ToString(out.ContentDisposition),
		CacheControl:       aws.ToString(out.CacheControl),
		StorageClass:       string(out.StorageClass),
		Metadata:           lowerMetadata(out.Metadata),
	}
	if total, ok := contentRangeTotal(aws.ToString(out.ContentRange)); ok {
		info.Size = total
	}
	return &storage.Reader{ReadCloser: out.Body, Info: info}, nil
}

// GetBytes 在 Get 之上 io.ReadAll，读完即 Close。
func (c *Client) GetBytes(ctx context.Context, bucket, key string, opts storage.ReaderOptions) ([]byte, error) {
	reader, err := c.Get(ctx, bucket, key, opts)
	if err != nil {
		return nil, err
	}
	defer reader.Close()
	return io.ReadAll(reader)
}

// rangeHeader 把前闭后开区间转成 HTTP Range 头；End < 0 表示到对象结尾，
// Start < 0 表示对象末尾 |Start| 字节。
func rangeHeader(r storage.ByteRange) (string, error) {
	if r.Start < 0 {
		if r.End > 0 {
			return "", fmt.Errorf("%w: 非法字节区间 [%d, %d)", storage.ErrInvalidArgument, r.Start, r.End)
		}
		return fmt.Sprintf("bytes=-%d", -r.Start), nil
	}
	if r.End >= 0 && r.End <= r.Start {
		return "", fmt.Errorf("%w: 非法字节区间 [%d, %d)", storage.ErrInvalidArgument, r.Start, r.End)
	}
	if r.End < 0 {
		return fmt.Sprintf("bytes=%d-", r.Start), nil
	}
	return fmt.Sprintf("bytes=%d-%d", r.Start, r.End-1), nil
}

// contentRangeTotal 从 "bytes 10-19/100" 中解析对象总大小。
func contentRangeTotal(contentRange string) (int64, bool) {
	i := strings.LastIndex(contentRange, "/")
	if i < 0 || i == len(contentRange)-1 {
		return 0, false
	}
	total, err := strconv.ParseInt(contentRange[i+1:], 10, 64)
	if err != nil || total < 0 {
		return 0, false
	}
	return total, true
}

// trimETag 去掉 S3 返回的 ETag 引号。
func trimETag(etag *string) string {
	return strings.Trim(aws.ToString(etag), `"`)
}

// lowerMetadata 把用户元数据的键统一为小写。
func lowerMetadata(metadata map[string]string) map[string]string {
	if len(metadata) == 0 {
		return nil
	}
	lowered := make(map[string]string, len(metadata))
	for key, value := range metadata {
		lowered[strings.ToLower(key)] = value
	}
	return lowered
}
