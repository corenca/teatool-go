package s3

import (
	"context"
	"fmt"

	"github.com/corenca/teatool-go/storage"
)

// Put 在发起任何请求前校验 PartSize 与 Concurrency，非法值直接返回
// ErrInvalidArgument；首次缓冲超过 PartSize 时才创建分片上传任务。
func (c *Client) Put(ctx context.Context, bucket, key string, opts storage.WriterOptions) (storage.Writer, error) {
	partSize := opts.PartSize
	if partSize == 0 {
		partSize = storage.DefaultPartSize
	}
	if partSize < storage.MinPartSize || partSize > storage.MaxPartSize {
		return nil, fmt.Errorf("%w: PartSize %d 必须在 [%d, %d] 内",
			storage.ErrInvalidArgument, partSize, storage.MinPartSize, storage.MaxPartSize)
	}

	concurrency := opts.Concurrency
	if concurrency == 0 {
		concurrency = storage.DefaultConcurrency
	}
	if concurrency < 0 {
		return nil, fmt.Errorf("%w: Concurrency %d 不能为负", storage.ErrInvalidArgument, concurrency)
	}

	return &writer{
		client:      c,
		ctx:         ctx,
		bucket:      bucket,
		key:         key,
		opts:        opts,
		partSize:    partSize,
		concurrency: concurrency,
		sem:         make(chan struct{}, concurrency),
	}, nil
}

// PutBytes 在 Put 之上写一次再 Close；写入失败时调用 Abort。
func (c *Client) PutBytes(ctx context.Context, bucket, key string, data []byte, opts storage.WriterOptions) error {
	writer, err := c.Put(ctx, bucket, key, opts)
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		_ = writer.Abort()
		return err
	}
	return writer.Close()
}
