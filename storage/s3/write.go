package s3

import (
	"context"

	"github.com/corenca/teatool-go/storage"
)

// Put 返回流式写入器：缓冲区累计到分片大小后自动转分片上传。
func (c *Client) Put(ctx context.Context, bucket, key string, opts storage.WriterOptions) (storage.Writer, error) {
	partSize := opts.PartSize
	if partSize <= 0 {
		partSize = storage.DefaultPartSize
	}
	return &writer{
		client:   c,
		ctx:      ctx,
		bucket:   bucket,
		key:      key,
		opts:     opts,
		partSize: partSize,
	}, nil
}

// PutBytes 写入内存中的字节。
func (c *Client) PutBytes(ctx context.Context, bucket, key string, data []byte, opts storage.WriterOptions) error {
	writer, err := c.Put(ctx, bucket, key, opts)
	if err != nil {
		return err
	}
	if _, err := writer.Write(data); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}
