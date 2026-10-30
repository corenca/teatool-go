package s3

import (
	"context"
	"errors"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/corenca/teatool-go/storage"
)

// deleteBatchSize 是单次批量删除请求的 key 上限，S3 与各兼容端点一致。
const deleteBatchSize = 1000

// Delete 把服务端返回的 404 视为成功，其余错误按契约映射。
func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	_, err := c.sdk.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		mapped := mapError(err)
		if errors.Is(mapped, storage.ErrObjectNotFound) {
			return nil
		}
		return mapped
	}
	return nil
}

// DeleteMany 每批 1000 个，并固定附加 Content-MD5（OSS/COS 强制要求）；某批请求
// 失败不会阻止后续批次，全部错误最后聚合。
func (c *Client) DeleteMany(ctx context.Context, bucket string, keys []string) error {
	var errs []error
	for _, batch := range splitKeys(keys, deleteBatchSize) {
		out, err := c.sdk.DeleteObjects(ctx, &awss3.DeleteObjectsInput{
			Bucket: aws.String(bucket),
			Delete: &types.Delete{
				Objects: objectIdentifiers(batch),
				Quiet:   aws.Bool(true),
			},
		}, withContentMD5)
		if err != nil {
			errs = append(errs, fmt.Errorf("storage: 批量删除 %d 个对象: %w", len(batch), err))
			continue
		}
		for _, item := range out.Errors {
			errs = append(errs, &storage.DeleteError{
				Key: aws.ToString(item.Key),
				Err: errors.New(aws.ToString(item.Message)),
			})
		}
	}
	return errors.Join(errs...)
}

// DeletePrefix 逐页列举后再删除该页，内存占用与对象总数无关；中途失败仍继续
// 删除后续页面，最后一并返回聚合错误。
func (c *Client) DeletePrefix(ctx context.Context, bucket, prefix string) error {
	if prefix == "" {
		return fmt.Errorf("%w: 删除前缀不能为空", storage.ErrInvalidArgument)
	}
	var errs []error
	cursor := ""
	for {
		page, err := c.List(ctx, bucket, storage.ListOptions{Prefix: prefix, Cursor: cursor})
		if err != nil {
			return errors.Join(append(errs, err)...)
		}
		if len(page.Objects) > 0 {
			keys := make([]string, 0, len(page.Objects))
			for _, object := range page.Objects {
				keys = append(keys, object.Key)
			}
			if err := c.DeleteMany(ctx, bucket, keys); err != nil {
				errs = append(errs, err)
			}
		}
		if page.NextCursor == "" {
			return errors.Join(errs...)
		}
		cursor = page.NextCursor
	}
}

// splitKeys 按批大小切分 key 列表。
func splitKeys(keys []string, size int) [][]string {
	var batches [][]string
	for start := 0; start < len(keys); start += size {
		batches = append(batches, keys[start:min(start+size, len(keys))])
	}
	return batches
}

// objectIdentifiers 把 key 列表转成批量删除元素。
func objectIdentifiers(keys []string) []types.ObjectIdentifier {
	identifiers := make([]types.ObjectIdentifier, 0, len(keys))
	for _, key := range keys {
		identifiers = append(identifiers, types.ObjectIdentifier{Key: aws.String(key)})
	}
	return identifiers
}
