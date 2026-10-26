package s3

import (
	"context"
	"fmt"
	"io"
	"os"

	"github.com/corenca/teatool-go/fsutil"
	"github.com/corenca/teatool-go/storage"
)

// Upload 把本地文件上传为对象。
func (c *Client) Upload(ctx context.Context, bucket, key, path string, opts storage.WriterOptions) error {
	file, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("storage: 打开本地文件 %s: %w", path, err)
	}
	defer file.Close()

	writer, err := c.Put(ctx, bucket, key, opts)
	if err != nil {
		return err
	}
	if _, err := io.Copy(writer, file); err != nil {
		_ = writer.Close()
		return err
	}
	return writer.Close()
}

// Download 把对象下载到本地文件；父目录不存在时创建，已存在则覆盖。
func (c *Client) Download(ctx context.Context, bucket, key, path string, opts storage.ReaderOptions) error {
	reader, err := c.Get(ctx, bucket, key, opts)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := fsutil.EnsureParent(path); err != nil {
		return err
	}
	file, err := os.Create(path)
	if err != nil {
		return fmt.Errorf("storage: 创建本地文件 %s: %w", path, err)
	}
	if _, err := io.Copy(file, reader); err != nil {
		_ = file.Close()
		return err
	}
	return file.Close()
}
