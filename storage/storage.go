// Package storage 定义对象存储的统一操作契约，并提供按名字构造客户端的工厂。
//
// 本包只包含契约、工厂与错误哨兵，不依赖任何厂商 SDK 或配置库：配置解码器由
// 调用方注入（见 NewFactory），存储实现见 storage/s3。同一份实现覆盖 AWS S3
// 以及 OSS、COS、OBS、MinIO 等 S3 兼容端点，厂商差异（endpoint、region、
// 签名细节）由实现内部消化。
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

	// MinPartSize 是分片上传允许的最小分片：取各厂商中最严格的 5MiB，
	// 保证同一份代码在 OSS/COS/OBS 上不会因子切片过小被拒绝。
	MinPartSize = 5 << 20

	// MaxPartSize 是分片上传允许的最大分片：5GiB。
	MaxPartSize = 5 << 30

	// MaxParts 是单次分片上传允许的最大分片数。
	MaxParts = 10000

	// DefaultConcurrency 是 Put 未指定 Concurrency 时并发的分片上传数。
	DefaultConcurrency = 3

	// DefaultListPageSize 是 List 未指定 MaxKeys 时的每页条数。
	DefaultListPageSize = 1000

	// MaxPresignExpiry 是预签名 URL 的最长有效期。
	MaxPresignExpiry = 7 * 24 * time.Hour
)

var (
	// ErrObjectNotFound 表示对象不存在。
	ErrObjectNotFound = errors.New("storage: object not found")

	// ErrBucketNotFound 表示服务端明确返回了 NoSuchBucket。基于 HEAD 的
	// Stat/Exists/Get 无法区分"桶不存在"与"对象不存在"（404 无错误体），
	// 此时会归为 ErrObjectNotFound；桶的存在性由调用方保证。
	ErrBucketNotFound = errors.New("storage: bucket not found")

	// ErrAccessDenied 表示凭据无效、签名错误或权限不足。
	ErrAccessDenied = errors.New("storage: access denied")

	// ErrPreconditionFailed 表示服务端返回 412（如桶策略或服务端强制的禁止覆盖），
	// 调用方主动发起的条件写不在契约内：所有上传都是覆盖写。
	ErrPreconditionFailed = errors.New("storage: precondition failed")

	// ErrInvalidRange 表示请求的字节区间不被服务端接受（通常是起始位置越界）。
	ErrInvalidRange = errors.New("storage: invalid range")

	// ErrUnsupported 表示服务端不支持该能力（HTTP 501 或 NotImplemented）。
	ErrUnsupported = errors.New("storage: unsupported operation")

	// ErrInvalidArgument 表示调用方传入了非法参数。
	ErrInvalidArgument = errors.New("storage: invalid argument")
)

// Client 是对象存储的统一操作契约。实现必须可被多个 goroutine 并发调用。
//
// 缺失语义：Stat、Get 与 Copy 的源对象不存在时返回包装了 ErrObjectNotFound
// 的错误；Delete 与 DeleteMany 幂等，对象不存在不报错；Exists 只在对象确实
// 不存在时返回 false，权限不足等错误必须原样返回。
//
// 桶语义：契约不做桶级存在性检查，桶必须由调用方预先保证存在。基于 HEAD 的
// Stat/Exists/Get 无法区分"桶不存在"与"对象不存在"（404 无错误体），统一归为
// ErrObjectNotFound；ErrBucketNotFound 只在服务端返回 NoSuchBucket 错误体时
// 出现（如 List、DeleteObjects）。
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

	// Put 返回流式写入器。Close 成功后才对读可见；写入器不可并发使用。
	// 结束时必须调用 Close（提交）或 Abort（放弃）之一。
	Put(ctx context.Context, bucket, key string, opts WriterOptions) (Writer, error)

	// PutBytes 写入内存中的字节。
	PutBytes(ctx context.Context, bucket, key string, data []byte, opts WriterOptions) error

	// Upload 把本地文件上传为对象。
	Upload(ctx context.Context, bucket, key, path string, opts WriterOptions) error

	// Download 把对象下载到本地文件，父目录不存在时创建，已存在则覆盖。
	Download(ctx context.Context, bucket, key, path string, opts ReaderOptions) error

	// Delete 删除单个对象；对象不存在视为成功。
	Delete(ctx context.Context, bucket, key string) error

	// DeleteMany 批量删除，实现负责按服务端上限分批；单个对象的失败以
	// *DeleteError 记录并用 errors.Join 聚合，调用方需检查返回错误。
	DeleteMany(ctx context.Context, bucket string, keys []string) error

	// DeletePrefix 删除前缀下的全部对象；prefix 为空返回 ErrInvalidArgument。
	DeletePrefix(ctx context.Context, bucket, prefix string) error

	// Copy 服务端复制，支持跨桶；大对象由实现自动转分片复制并保留源元信息。
	Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error

	// Move 由 Copy 与 Delete 组合实现，非原子：目标写入成功但源删除失败时返回错误。
	Move(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error

	// List 返回一页列举结果，Cursor 为空表示从头开始。
	List(ctx context.Context, bucket string, opts ListOptions) (ListPage, error)

	// ListAll 自动翻页，返回前缀下的全部对象。
	ListAll(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error)

	// Walk 按 key 字典序流式遍历前缀下的对象，逐条回调。fn 返回错误时立即
	// 停止并原样返回该错误；内存占用与对象总数无关。
	Walk(ctx context.Context, bucket, prefix string, fn func(ObjectInfo) error) error

	// ListDir 返回某一层级的目录视图：本层文件夹名与本层直接对象。prefix
	// 缺少尾部 "/" 时实现自动补齐（"sql" 视同 "sql/"）。
	ListDir(ctx context.Context, bucket, prefix string) (DirInfo, error)

	// PresignGet 生成下载预签名 URL。
	PresignGet(ctx context.Context, bucket, key string, opts PresignOptions) (PresignedURL, error)

	// PresignPut 生成上传预签名 URL。
	PresignPut(ctx context.Context, bucket, key string, opts PresignOptions) (PresignedURL, error)
}

// DeleteError 是批量删除中单个对象的失败。DeleteMany 用 errors.Join 聚合，
// 调用方可用 errors.As 取出 Key，用 errors.Is/As 检查底层原因。
type DeleteError struct {
	Key string
	Err error
}

// Error 实现 error。
func (e *DeleteError) Error() string {
	return fmt.Sprintf("storage: 删除 %s 失败: %v", e.Key, e.Err)
}

// Unwrap 暴露底层错误。
func (e *DeleteError) Unwrap() error { return e.Err }

// Reader 是对象内容流，调用方必须 Close。
type Reader struct {
	io.ReadCloser

	// Info 是服务端在响应中返回的元信息；区间请求时 Size 为对象总大小。
	Info ObjectInfo
}

// Writer 是流式写入器，由 Client.Put 返回。
//
// Close 提交本次写入：成功后才对读可见，失败时实现会尽力回收未完成的资源
// （如分片上传）。Abort 放弃本次写入并立即回收资源，不会删除已提交的对象。
// 两个方法都幂等：Close 之后再 Abort、Abort 之后再 Close 都不报错，Abort 之后
// 再 Write 返回包装 ErrInvalidArgument 的错误。
//
// 定义为接口以便各实现提供自己的写入器，storage 包不依赖任何厂商 SDK。
type Writer interface {
	io.Writer
	// Close 提交写入；失败时实现尽力回收未完成的上传。
	Close() error
	// Abort 放弃写入并回收未完成的上传；已提交的对象不受影响。
	Abort() error
}

// ObjectInfo 是对象的元信息。列举结果中 ContentType、ContentDisposition、
// CacheControl 与 Metadata 可能为零值（ListObjects 不返回这些头），需要完整
// 元信息时使用 Stat 或 Reader.Info；StorageClass 在两种来源下都可能为空。
type ObjectInfo struct {
	Key                string            `json:"key"`
	Size               int64             `json:"size"`
	LastModified       time.Time         `json:"lastModified"`
	ETag               string            `json:"etag"` // 不承诺是 MD5：OSS 与分片上传的算法与 S3 不同
	ContentType        string            `json:"contentType"`
	ContentDisposition string            `json:"contentDisposition"`
	CacheControl       string            `json:"cacheControl"`
	StorageClass       string            `json:"storageClass"`
	Metadata           map[string]string `json:"metadata"` // 键为小写，不含 Content-Type
}

// String 实现 fmt.Stringer，便于日志与测试输出。
func (o ObjectInfo) String() string {
	return fmt.Sprintf("ObjectInfo(key: %s, size: %d, lastModified: %s, etag: %s, contentType: %s, contentDisposition: %s, cacheControl: %s, storageClass: %s, metadata: %v)",
		o.Key, o.Size, o.LastModified.Format(time.RFC3339Nano), o.ETag, o.ContentType,
		o.ContentDisposition, o.CacheControl, o.StorageClass, o.Metadata)
}

// ByteRange 是前闭后开的字节区间 [Start, End)；End < 0 表示直到对象结尾。
// Start 为负数表示取末尾 |Start| 字节（此时 End 必须为 0 或负数），用于读取
// 日志尾部或 Parquet 文件脚注。
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
	CacheControl       string            // 空表示不设置
	Metadata           map[string]string // 键写入前统一转小写

	// PartSize 是分片阈值与单片大小，范围 [MinPartSize, MaxPartSize]，
	// 0 表示 DefaultPartSize。数据不超过 PartSize 时走单次 PutObject。
	PartSize int

	// Concurrency 是并发上传的分片数，0 表示 DefaultConcurrency；
	// 峰值内存约为 PartSize × Concurrency。
	Concurrency int
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
