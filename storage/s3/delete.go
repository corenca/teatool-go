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

// Delete 删除单个对象；对象不存在视为成功。
func (c *Client) Delete(ctx context.Context, bucket, key string) error {
	_, err := c.sdk.DeleteObject(ctx, &awss3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		if errors.Is(mapError(err), storage.ErrObjectNotFound) {
			return nil
		}
		return err
	}
	return nil
}

// DeleteMany 分批批量删除；部分失败用 errors.Join 聚合。
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
			errs = append(errs, fmt.Errorf("storage: 删除 %s 失败: %s", aws.ToString(item.Key), aws.ToString(item.Message)))
		}
	}
	return errors.Join(errs...)
}

// DeletePrefix 删除前缀下的全部对象；空前缀会被拒绝以免清空整个桶。
func (c *Client) DeletePrefix(ctx context.Context, bucket, prefix string) error {
	if prefix == "" {
		return fmt.Errorf("%w: 删除前缀不能为空", storage.ErrInvalidArgument)
	}
	objects, err := c.ListAll(ctx, bucket, prefix)
	if err != nil {
		return err
	}
	if len(objects) == 0 {
		return nil
	}
	keys := make([]string, 0, len(objects))
	for _, object := range objects {
		keys = append(keys, object.Key)
	}
	return c.DeleteMany(ctx, bucket, keys)
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
