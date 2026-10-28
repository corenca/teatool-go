package s3

import (
	"context"
	"fmt"

	"github.com/corenca/teatool-go/storage"
)

// Put 返回流式写入器：缓冲区累计到分片大小后自动转分片上传。
//
// PartSize 与 Concurrency 在发起任何请求前校验，非法值返回 ErrInvalidArgument。
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

// PutBytes 写入内存中的字节；写入失败时放弃本次上传，不留下可见对象。
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
