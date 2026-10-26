package s3

import (
	"context"
	"fmt"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
)

// Copy 服务端复制，支持跨桶。
func (c *Client) Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	_, err := c.sdk.CopyObject(ctx, &awss3.CopyObjectInput{
		Bucket:     aws.String(dstBucket),
		Key:        aws.String(dstKey),
		CopySource: aws.String(copySource(srcBucket, srcKey)),
	})
	return mapError(err)
}

// Move 由复制与删除组合实现，非原子。
func (c *Client) Move(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	if err := c.Copy(ctx, srcBucket, srcKey, dstBucket, dstKey); err != nil {
		return err
	}
	if err := c.Delete(ctx, srcBucket, srcKey); err != nil {
		return fmt.Errorf("storage: 复制成功但删除源对象 %s 失败: %w", srcKey, err)
	}
	return nil
}

// copySource 生成 CopySource 头：保留 "/" 分隔，只转义特殊字符。
func copySource(bucket, key string) string {
	source := &url.URL{Path: "/" + bucket + "/" + key}
	return source.EscapedPath()
}
