// Package storage 定义对象存储的统一操作契约。
//
// 本包只有契约：接口、数据类型与错误哨兵，不依赖任何厂商 SDK。实现见
// storage/s3；同一份实现覆盖 AWS S3 以及 OSS、COS、OBS、MinIO 等 S3
// 兼容端点，厂商差异（endpoint、region、签名细节）由实现内部消化。
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"
)

const (
	// DefaultPartSize 是 Put 的默认分片大小：8MiB。
	DefaultPartSize = 8 << 20

	// DefaultListPageSize 是 List 未指定 MaxKeys 时的每页条数。
	DefaultListPageSize = 1000

	// MaxPresignExpiry 是预签名 URL 的最长有效期。
	MaxPresignExpiry = 7 * 24 * time.Hour
)

var (
	// ErrObjectNotFound 表示对象不存在。
	ErrObjectNotFound = errors.New("storage: object not found")

	// ErrInvalidArgument 表示调用方传入了非法参数。
	ErrInvalidArgument = errors.New("storage: invalid argument")
)

// Client 是对象存储的统一操作契约。实现必须可被多个 goroutine 并发调用。
//
// 缺失语义：Stat、Get 与 Copy 的源对象不存在时返回包装了 ErrObjectNotFound
// 的错误；Delete 与 DeleteMany 幂等，对象不存在不报错；Exists 只在对象确实
// 不存在时返回 false，权限不足等错误必须原样返回。
type Client interface {
	// ListBuckets 列举当前凭据可见的桶名，按字典序。
	ListBuckets(ctx context.Context) ([]string, error)

	// Stat 返回对象元信息（head 语义，不下载内容）。
	Stat(ctx context.Context, bucket, key string) (ObjectInfo, error)

	// Exists 判断对象是否存在。
	Exists(ctx context.Context, bucket, key string) (bool, error)

	// Get 返回对象内容流，Info 由响应头填充；区间请求时 Info.Size 是对象
	// 总大小。调用方负责 Close 返回的 Reader。
	Get(ctx context.Context, bucket, key string, opts ReaderOptions) (*Reader, error)

	// GetBytes 把对象内容读入内存。
	GetBytes(ctx context.Context, bucket, key string, opts ReaderOptions) ([]byte, error)

	// Put 返回流式写入器。Close 成功后才对读可见；写入器不可并发使用，调用方
	// 必须调用 Close，即使放弃本次写入。
	Put(ctx context.Context, bucket, key string, opts WriterOptions) (Writer, error)

	// PutBytes 写入内存中的字节。
	PutBytes(ctx context.Context, bucket, key string, data []byte, opts WriterOptions) error

	// Upload 把本地文件上传为对象。
	Upload(ctx context.Context, bucket, key, path string, opts WriterOptions) error

	// Download 把对象下载到本地文件，父目录不存在时创建，已存在则覆盖。
	Download(ctx context.Context, bucket, key, path string, opts ReaderOptions) error

	// Delete 删除单个对象；对象不存在视为成功。
	Delete(ctx context.Context, bucket, key string) error

	// DeleteMany 批量删除，实现负责按服务端上限分批；部分失败用 errors.Join
	// 聚合，调用方需检查返回错误。
	DeleteMany(ctx context.Context, bucket string, keys []string) error

	// DeletePrefix 删除前缀下的全部对象；prefix 为空返回 ErrInvalidArgument。
	DeletePrefix(ctx context.Context, bucket, prefix string) error

	// Copy 服务端复制，支持跨桶。
	Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error

	// Move 由 Copy 与 Delete 组合实现，非原子：目标写入成功但源删除失败时返回错误。
	Move(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error

	// List 返回一页列举结果，Cursor 为空表示从头开始。
	List(ctx context.Context, bucket string, opts ListOptions) (ListPage, error)

	// ListAll 自动翻页，返回前缀下的全部对象。
	ListAll(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error)

	// ListDir 返回某一层级的目录视图：本层文件夹名与本层直接对象。
	ListDir(ctx context.Context, bucket, prefix string) (DirInfo, error)

	// PresignGet 生成下载预签名 URL。
	PresignGet(ctx context.Context, bucket, key string, opts PresignOptions) (PresignedURL, error)

	// PresignPut 生成上传预签名 URL。
	PresignPut(ctx context.Context, bucket, key string, opts PresignOptions) (PresignedURL, error)
}

// Reader 是对象内容流，调用方必须 Close。
type Reader struct {
	io.ReadCloser

	// Info 是服务端在响应中返回的元信息；区间请求时 Size 为对象总大小。
	Info ObjectInfo
}

// Writer 是流式写入器，由 Client.Put 返回。
//
// Close 成功后才对读可见；Close 失败时实现尽力 Abort 未完成的分片。定义为
// 接口以便各实现提供自己的写入器，storage 包不依赖任何厂商 SDK。
type Writer interface {
	io.WriteCloser
}

// ObjectInfo 是对象的元信息。列举结果中 ContentType 与 Metadata 可能为零值，
// 需要完整元信息时使用 Stat 或 Reader.Info。
type ObjectInfo struct {
	Key          string            `json:"key"`
	Size         int64             `json:"size"`
	LastModified time.Time         `json:"lastModified"`
	ETag         string            `json:"etag"` // 不承诺是 MD5：OSS 与分片上传的算法与 S3 不同
	ContentType  string            `json:"contentType"`
	Metadata     map[string]string `json:"metadata"` // 键为小写，不含 Content-Type
}

// String 实现 fmt.Stringer，便于日志与测试输出。
func (o ObjectInfo) String() string {
	return fmt.Sprintf("ObjectInfo(key: %s, size: %d, lastModified: %s, etag: %s, contentType: %s, metadata: %v)",
		o.Key, o.Size, o.LastModified.Format(time.RFC3339Nano), o.ETag, o.ContentType, o.Metadata)
}

// ByteRange 是前闭后开的字节区间 [Start, End)；End < 0 表示直到对象结尾。
//
// 越界宽容：End 超出对象大小时截断，Start 不小于对象大小时返回空流，均不报错。
type ByteRange struct {
	Start int64
	End   int64
}

// String 实现 fmt.Stringer。
func (r ByteRange) String() string {
	return fmt.Sprintf("ByteRange(start: %d, end: %d)", r.Start, r.End)
}

// ReaderOptions 是读取参数。
type ReaderOptions struct {
	// Range 为 nil 表示读取整个对象。
	Range *ByteRange
}

// WriterOptions 是写入参数。
type WriterOptions struct {
	ContentType        string            // 空表示交给服务端默认
	ContentDisposition string            // 空表示不设置
	Metadata           map[string]string // 键写入前统一转小写
	PartSize           int               // 0 表示使用 DefaultPartSize
}

// ListOptions 是列举参数。
type ListOptions struct {
	Prefix    string // 对象键前缀
	Delimiter string // 为空表示不分组；"/" 表示目录视图
	Cursor    string // 上一页返回的 NextCursor，空表示从头开始
	MaxKeys   int    // <= 0 使用 DefaultListPageSize；计数含对象与公共前缀
}

// ListPage 是一页列举结果，Objects 与 CommonPrefixes 均按 key 字典序。
type ListPage struct {
	Objects        []ObjectInfo `json:"objects"`
	CommonPrefixes []string     `json:"commonPrefixes"` // 仅在 Delimiter 非空时出现，元素是完整前缀
	NextCursor     string       `json:"nextCursor"`     // 空表示已到末尾；非空时必须原样回传
	PageSize       int          `json:"pageSize"`       // 本页生效的每页条数（含对象与公共前缀）
}

// String 实现 fmt.Stringer；元素复用各自的 String。
func (p ListPage) String() string {
	return fmt.Sprintf("ListPage(objects: %v, commonPrefixes: %v, nextCursor: %s, pageSize: %d)",
		p.Objects, p.CommonPrefixes, p.NextCursor, p.PageSize)
}

// PresignOptions 是预签名参数。
type PresignOptions struct {
	Expires  time.Duration // 必须 > 0 且不超过 MaxPresignExpiry
	FileName string        // 仅 PresignGet 有效，通过响应头覆写下载文件名
}

// PresignedURL 是预签名结果。Headers 非空时，请求该 URL 必须原样携带。
type PresignedURL struct {
	URL     string            `json:"url"`
	Headers map[string]string `json:"headers"`
}

// String 实现 fmt.Stringer。
func (p PresignedURL) String() string {
	return fmt.Sprintf("PresignedURL(url: %s, headers: %v)", p.URL, p.Headers)
}

// DirInfo 是某一个层级的目录视图，供浏览器式界面使用。
type DirInfo struct {
	Prefix  string       `json:"prefix"`  // 查询前缀
	Dirs    []string     `json:"dirs"`    // 本层文件夹名（不含 Prefix，不以 "/" 结尾），按字典序
	Objects []ObjectInfo `json:"objects"` // 本层直接对象，按字典序
}

// String 实现 fmt.Stringer；元素复用各自的 String。
func (d DirInfo) String() string {
	return fmt.Sprintf("DirInfo(prefix: %s, dirs: %v, objects: %v)", d.Prefix, d.Dirs, d.Objects)
}
