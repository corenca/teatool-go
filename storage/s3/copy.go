package s3

import (
	"context"
	"fmt"
	"net/url"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/corenca/teatool-go/storage"
)

const (
	// singleCopyLimit 是 CopyObject 单次请求的大小上限（S3 为 5GiB）。
	singleCopyLimit = 5 << 30
	// copyPartSize 是分片复制时每片的大小。
	copyPartSize = 1 << 30
)

// Copy 超过 5GiB 时改用 UploadPartCopy 分片复制（分片 1GiB），并保留源元信息；
// 未超过时走单次 CopyObject。数据不经过本进程。
func (c *Client) Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	// 先取源对象大小：CopyObject 单次上限 5GiB，超出必须分片。
	info, err := c.Stat(ctx, srcBucket, srcKey)
	if err != nil {
		return err
	}
	if info.Size <= singleCopyLimit {
		_, err := c.sdk.CopyObject(ctx, &awss3.CopyObjectInput{
			Bucket:     aws.String(dstBucket),
			Key:        aws.String(dstKey),
			CopySource: aws.String(copySource(srcBucket, srcKey)),
		})
		return mapError(err)
	}
	return c.multipartCopy(ctx, info, srcBucket, srcKey, dstBucket, dstKey, copyPartSize)
}

// Move 由 Copy 与 Delete 组合实现；删除源失败时错误信息会标明是复制成功后的事务
// 失败，此时两个对象同时存在。
func (c *Client) Move(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	if err := c.Copy(ctx, srcBucket, srcKey, dstBucket, dstKey); err != nil {
		return err
	}
	if err := c.Delete(ctx, srcBucket, srcKey); err != nil {
		return fmt.Errorf("storage: 复制成功但删除源对象 %s 失败: %w", srcKey, err)
	}
	return nil
}

// multipartCopy 用 UploadPartCopy 复制大对象；分片大小由调用方给出，便于测试。
func (c *Client) multipartCopy(ctx context.Context, info storage.ObjectInfo,
	srcBucket, srcKey, dstBucket, dstKey string, partSize int64) error {
	create := &awss3.CreateMultipartUploadInput{
		Bucket: aws.String(dstBucket),
		Key:    aws.String(dstKey),
	}
	applyCopyMetadata(create, info)
	created, err := c.sdk.CreateMultipartUpload(ctx, create)
	if err != nil {
		return mapError(err)
	}
	uploadID := created.UploadId
	abort := func() {
		// 用不带取消的 ctx：调用方取消后仍要能清理未完成的分片。
		_, _ = c.sdk.AbortMultipartUpload(context.WithoutCancel(ctx), &awss3.AbortMultipartUploadInput{
			Bucket:   aws.String(dstBucket),
			Key:      aws.String(dstKey),
			UploadId: uploadID,
		})
	}

	parts := make([]types.CompletedPart, 0, info.Size/partSize+1)
	for number, start := int32(1), int64(0); start < info.Size; number, start = number+1, start+partSize {
		end := min(start+partSize, info.Size) - 1
		out, err := c.sdk.UploadPartCopy(ctx, &awss3.UploadPartCopyInput{
			Bucket:          aws.String(dstBucket),
			Key:             aws.String(dstKey),
			UploadId:        uploadID,
			PartNumber:      aws.Int32(number),
			CopySource:      aws.String(copySource(srcBucket, srcKey)),
			CopySourceRange: aws.String(fmt.Sprintf("bytes=%d-%d", start, end)),
		})
		if err != nil {
			abort()
			return mapError(err)
		}
		parts = append(parts, types.CompletedPart{
			ETag:       out.CopyPartResult.ETag,
			PartNumber: aws.Int32(number),
		})
	}

	if _, err := c.sdk.CompleteMultipartUpload(ctx, &awss3.CompleteMultipartUploadInput{
		Bucket:          aws.String(dstBucket),
		Key:             aws.String(dstKey),
		UploadId:        uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: parts},
	}); err != nil {
		abort()
		return mapError(err)
	}
	return nil
}

// applyCopyMetadata 把源对象的内容头带到目标的分片上传请求：分片复制只搬数据，
// 元信息必须在 CreateMultipartUpload 时显式指定。
func applyCopyMetadata(input *awss3.CreateMultipartUploadInput, info storage.ObjectInfo) {
	if info.ContentType != "" {
		input.ContentType = aws.String(info.ContentType)
	}
	if info.ContentDisposition != "" {
		input.ContentDisposition = aws.String(info.ContentDisposition)
	}
	if info.CacheControl != "" {
		input.CacheControl = aws.String(info.CacheControl)
	}
	if len(info.Metadata) > 0 {
		input.Metadata = lowerMetadata(info.Metadata)
	}
}

// copySource 生成 CopySource 头：保留 "/" 分隔，只转义特殊字符。
func copySource(bucket, key string) string {
	source := &url.URL{Path: "/" + bucket + "/" + key}
	return source.EscapedPath()
}
