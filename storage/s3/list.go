package s3

import (
	"context"
	"encoding/base64"
	"fmt"
	"math"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/corenca/teatool-go/storage"
)

// List 的游标是 continuation-token 的 base64url 编码，对调用方不透明。
func (c *Client) List(ctx context.Context, bucket string, opts storage.ListOptions) (storage.ListPage, error) {
	pageSize := opts.MaxKeys
	if pageSize <= 0 {
		pageSize = storage.DefaultListPageSize
	}
	// 服务端每页上限 1000，且 SDK 参数是 int32：裁剪避免整数溢出。
	pageSize = min(pageSize, math.MaxInt32)

	input := &awss3.ListObjectsV2Input{
		Bucket:  aws.String(bucket),
		MaxKeys: aws.Int32(int32(pageSize)),
	}
	if opts.Prefix != "" {
		input.Prefix = aws.String(opts.Prefix)
	}
	if opts.Delimiter != "" {
		input.Delimiter = aws.String(opts.Delimiter)
	}
	if opts.Cursor != "" {
		token, err := decodeCursor(opts.Cursor)
		if err != nil {
			return storage.ListPage{}, err
		}
		input.ContinuationToken = aws.String(token)
	}

	out, err := c.sdk.ListObjectsV2(ctx, input)
	if err != nil {
		return storage.ListPage{}, mapError(err)
	}

	// 服务端会在响应中回填生效的每页条数（可能被上限截断）。
	page := storage.ListPage{PageSize: int(aws.ToInt32(out.MaxKeys))}
	if page.PageSize <= 0 {
		page.PageSize = pageSize
	}
	for _, object := range out.Contents {
		page.Objects = append(page.Objects, storage.ObjectInfo{
			Key:          aws.ToString(object.Key),
			Size:         aws.ToInt64(object.Size),
			LastModified: aws.ToTime(object.LastModified),
			ETag:         trimETag(object.ETag),
			StorageClass: string(object.StorageClass),
		})
	}
	for _, prefix := range out.CommonPrefixes {
		page.CommonPrefixes = append(page.CommonPrefixes, aws.ToString(prefix.Prefix))
	}
	if aws.ToBool(out.IsTruncated) {
		page.NextCursor = encodeCursor(aws.ToString(out.NextContinuationToken))
	}
	return page, nil
}

// ListAll 逐页调用 List 并累积结果。
func (c *Client) ListAll(ctx context.Context, bucket, prefix string) ([]storage.ObjectInfo, error) {
	var objects []storage.ObjectInfo
	err := c.Walk(ctx, bucket, prefix, func(object storage.ObjectInfo) error {
		objects = append(objects, object)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return objects, nil
}

// Walk 边翻页边回调，每页只保留一条列举结果。
func (c *Client) Walk(ctx context.Context, bucket, prefix string, fn func(storage.ObjectInfo) error) error {
	cursor := ""
	for {
		page, err := c.List(ctx, bucket, storage.ListOptions{Prefix: prefix, Cursor: cursor})
		if err != nil {
			return err
		}
		for _, object := range page.Objects {
			if err := fn(object); err != nil {
				return err
			}
		}
		if page.NextCursor == "" {
			return nil
		}
		cursor = page.NextCursor
	}
}

// ListDir 用 delimiter="/" 实现单层目录视图；prefix 自动补齐尾斜杠，返回的
// DirInfo.Prefix 是补齐后的值。
func (c *Client) ListDir(ctx context.Context, bucket, prefix string) (storage.DirInfo, error) {
	prefix = dirPrefix(prefix)
	info := storage.DirInfo{Prefix: prefix}
	cursor := ""
	for {
		page, err := c.List(ctx, bucket, storage.ListOptions{
			Prefix:    prefix,
			Delimiter: "/",
			Cursor:    cursor,
		})
		if err != nil {
			return storage.DirInfo{}, err
		}
		info.Objects = append(info.Objects, page.Objects...)
		for _, commonPrefix := range page.CommonPrefixes {
			name := strings.TrimSuffix(strings.TrimPrefix(commonPrefix, prefix), "/")
			if name != "" {
				info.Dirs = append(info.Dirs, name)
			}
		}
		if page.NextCursor == "" {
			return info, nil
		}
		cursor = page.NextCursor
	}
}

// ListBuckets 直接转发 SDK 的 ListBuckets。
func (c *Client) ListBuckets(ctx context.Context) ([]string, error) {
	out, err := c.sdk.ListBuckets(ctx, &awss3.ListBucketsInput{})
	if err != nil {
		return nil, mapError(err)
	}
	buckets := make([]string, 0, len(out.Buckets))
	for _, bucket := range out.Buckets {
		buckets = append(buckets, aws.ToString(bucket.Name))
	}
	return buckets, nil
}

// dirPrefix 把目录前缀归一化为以 "/" 结尾（空串保持为空）。
func dirPrefix(prefix string) string {
	if prefix == "" || strings.HasSuffix(prefix, "/") {
		return prefix
	}
	return prefix + "/"
}

// encodeCursor 把服务端 continuation-token 编码成不透明游标。
func encodeCursor(token string) string {
	if token == "" {
		return ""
	}
	return base64.RawURLEncoding.EncodeToString([]byte(token))
}

// decodeCursor 解析游标；非法编码返回 ErrInvalidArgument。
func decodeCursor(cursor string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(cursor)
	if err != nil || len(raw) == 0 {
		return "", fmt.Errorf("%w: 非法游标 %q", storage.ErrInvalidArgument, cursor)
	}
	return string(raw), nil
}
