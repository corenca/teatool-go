package s3

import (
	"context"
	"encoding/base64"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/corenca/teatool-go/storage"
)

// List 返回一页列举结果；游标为 continuation-token 的不透明编码。
func (c *Client) List(ctx context.Context, bucket string, opts storage.ListOptions) (storage.ListPage, error) {
	pageSize := opts.MaxKeys
	if pageSize <= 0 {
		pageSize = storage.DefaultListPageSize
	}

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

// ListAll 自动翻页，返回前缀下的全部对象。
func (c *Client) ListAll(ctx context.Context, bucket, prefix string) ([]storage.ObjectInfo, error) {
	var objects []storage.ObjectInfo
	cursor := ""
	for {
		page, err := c.List(ctx, bucket, storage.ListOptions{Prefix: prefix, Cursor: cursor})
		if err != nil {
			return nil, err
		}
		objects = append(objects, page.Objects...)
		if page.NextCursor == "" {
			return objects, nil
		}
		cursor = page.NextCursor
	}
}

// ListDir 返回某一层级的目录视图：Dirs 只含本层文件夹名。
func (c *Client) ListDir(ctx context.Context, bucket, prefix string) (storage.DirInfo, error) {
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

// ListBuckets 列举当前凭据可见的桶名。
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
