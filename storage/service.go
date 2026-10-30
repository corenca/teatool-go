package storage

import (
	"context"
	"fmt"
)

// Service 是把 Client 绑定到单个桶的适配器：所有方法的桶由构造时固定，其余参数、
// 返回值与错误语义和 Client 完全一致，因此下文只写每个方法做什么，重复语义见 Client。
//
// 必须通过 NewService 构造，零值不可用。是否可并发调用由底层 Client 决定。
type Service struct {
	client Client
	bucket string
}

// NewService 把 client 绑定到 bucket。client 为 nil 或 bucket 为空时返回包装
// ErrInvalidArgument 的错误。接口值指向 nil 指针（typed-nil，如未初始化的具体
// 客户端）不会被识别为 nil，需要调用方避免。
func NewService(client Client, bucket string) (*Service, error) {
	if client == nil {
		return nil, fmt.Errorf("%w: client 不能为空", ErrInvalidArgument)
	}
	if bucket == "" {
		return nil, fmt.Errorf("%w: bucket 不能为空", ErrInvalidArgument)
	}
	return &Service{client: client, bucket: bucket}, nil
}

// Bucket 返回绑定的桶名（构造时传入的原始值，不标准化）。
func (s *Service) Bucket() string { return s.bucket }

// Stat 返回对象元信息（head 语义）。
func (s *Service) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	return s.client.Stat(ctx, s.bucket, key)
}

// Exists 判断对象是否存在。
func (s *Service) Exists(ctx context.Context, key string) (bool, error) {
	return s.client.Exists(ctx, s.bucket, key)
}

// Get 返回对象内容流，调用方负责 Close。
func (s *Service) Get(ctx context.Context, key string, opts ReaderOptions) (*Reader, error) {
	return s.client.Get(ctx, s.bucket, key, opts)
}

// GetBytes 把对象内容读入内存。
func (s *Service) GetBytes(ctx context.Context, key string, opts ReaderOptions) ([]byte, error) {
	return s.client.GetBytes(ctx, s.bucket, key, opts)
}

// Put 返回流式写入器，结束时必须 Close 或 Abort。
func (s *Service) Put(ctx context.Context, key string, opts WriterOptions) (Writer, error) {
	return s.client.Put(ctx, s.bucket, key, opts)
}

// PutBytes 写入内存中的字节。
func (s *Service) PutBytes(ctx context.Context, key string, data []byte, opts WriterOptions) error {
	return s.client.PutBytes(ctx, s.bucket, key, data, opts)
}

// Upload 把本地文件上传为对象。
func (s *Service) Upload(ctx context.Context, key, path string, opts WriterOptions) error {
	return s.client.Upload(ctx, s.bucket, key, path, opts)
}

// Download 把对象下载到本地文件。
func (s *Service) Download(ctx context.Context, key, path string, opts ReaderOptions) error {
	return s.client.Download(ctx, s.bucket, key, path, opts)
}

// Delete 删除单个对象。
func (s *Service) Delete(ctx context.Context, key string) error {
	return s.client.Delete(ctx, s.bucket, key)
}

// DeleteMany 批量删除。
func (s *Service) DeleteMany(ctx context.Context, keys []string) error {
	return s.client.DeleteMany(ctx, s.bucket, keys)
}

// DeletePrefix 删除前缀下的全部对象。
func (s *Service) DeletePrefix(ctx context.Context, prefix string) error {
	return s.client.DeletePrefix(ctx, s.bucket, prefix)
}

// Copy 把源与目标都收窄到绑定的同一个桶，不支持跨桶。
func (s *Service) Copy(ctx context.Context, srcKey, dstKey string) error {
	return s.client.Copy(ctx, s.bucket, srcKey, s.bucket, dstKey)
}

// Move 在绑定的桶内移动，由 Copy 与 Delete 组合实现，非原子。
func (s *Service) Move(ctx context.Context, srcKey, dstKey string) error {
	return s.client.Move(ctx, s.bucket, srcKey, s.bucket, dstKey)
}

// List 返回一页列举结果。
func (s *Service) List(ctx context.Context, opts ListOptions) (ListPage, error) {
	return s.client.List(ctx, s.bucket, opts)
}

// ListAll 自动翻页，返回前缀下的全部对象。
func (s *Service) ListAll(ctx context.Context, prefix string) ([]ObjectInfo, error) {
	return s.client.ListAll(ctx, s.bucket, prefix)
}

// Walk 按 key 字典序流式遍历前缀下的对象。
func (s *Service) Walk(ctx context.Context, prefix string, fn func(ObjectInfo) error) error {
	return s.client.Walk(ctx, s.bucket, prefix, fn)
}

// ListDir 返回某一层级的目录视图。
func (s *Service) ListDir(ctx context.Context, prefix string) (DirInfo, error) {
	return s.client.ListDir(ctx, s.bucket, prefix)
}

// PresignGet 生成下载预签名 URL。
func (s *Service) PresignGet(ctx context.Context, key string, opts PresignOptions) (PresignedURL, error) {
	return s.client.PresignGet(ctx, s.bucket, key, opts)
}

// PresignPut 生成上传预签名 URL。
func (s *Service) PresignPut(ctx context.Context, key string, opts PresignOptions) (PresignedURL, error) {
	return s.client.PresignPut(ctx, s.bucket, key, opts)
}
