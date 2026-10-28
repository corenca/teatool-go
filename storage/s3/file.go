package s3

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/corenca/teatool-go/fsutil"
	"github.com/corenca/teatool-go/storage"
)

// Upload 把本地文件上传为对象。读取失败时放弃上传，不会提交截断的对象。
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
		_ = writer.Abort()
		return err
	}
	return writer.Close()
}

// Download 把对象下载到本地文件；父目录不存在时创建，已存在则覆盖。
//
// 先写同目录临时文件再 rename：中途失败不会破坏已有文件，也不会留下半个文件。
func (c *Client) Download(ctx context.Context, bucket, key, path string, opts storage.ReaderOptions) error {
	reader, err := c.Get(ctx, bucket, key, opts)
	if err != nil {
		return err
	}
	defer reader.Close()

	if err := fsutil.EnsureParent(path); err != nil {
		return err
	}
	file, err := createTemp(filepath.Dir(path))
	if err != nil {
		return fmt.Errorf("storage: 创建临时文件失败: %w", err)
	}
	tempPath := file.Name()
	committed := false
	defer func() {
		if !committed {
			_ = file.Close()
			_ = os.Remove(tempPath)
		}
	}()

	if _, err := io.Copy(file, reader); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("storage: 替换本地文件 %s: %w", path, err)
	}
	committed = true
	return nil
}

// createTemp 在 dir 下创建权限 0666（受 umask 约束）的临时文件。
// 不用 os.CreateTemp：它固定 0600，改回 0644 又会绕过用户的 umask。
func createTemp(dir string) (*os.File, error) {
	for range 100 {
		path := filepath.Join(dir, ".teatool-download-"+rand.Text())
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o666)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
	}
	return nil, errors.New("storage: 无法创建唯一的临时文件")
}
