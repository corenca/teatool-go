// Package storage 定义对象存储的统一操作契约。
//
// 本包只包含契约、错误哨兵与单桶适配器，不依赖任何厂商 SDK 或配置库；存储实现见
// storage/s3。同一份实现覆盖 AWS S3 以及 OSS、COS、OBS、MinIO 等 S3 兼容端点，
// 厂商差异（endpoint、region、签名细节）由实现内部消化。多实例的装配属于调用方：
// 用 s3.New 逐个构造客户端，需要单桶视图时再套 storage.NewService。
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
	//
	// 小于该值的数据走单次 PutObject；大于等于时转分片上传。取 8MiB 是在
	// 请求次数与失败重传成本之间的折中，可通过 WriterOptions.PartSize 调整。
	DefaultPartSize = 8 << 20

	// MinPartSize 是分片上传允许的最小分片：取各厂商中最严格的 5MiB，
	// 保证同一份代码在 OSS/COS/OBS 上不会因子切片过小被拒绝。
	//
	// WriterOptions.PartSize 小于该值时会被实现拒绝，返回 ErrInvalidArgument。
	MinPartSize = 5 << 20

	// MaxPartSize 是分片上传允许的最大分片：5GiB。
	//
	// 超出时返回 ErrInvalidArgument；单片过大意味着失败重传成本高。
	MaxPartSize = 5 << 30

	// MaxParts 是单次分片上传允许的最大分片数。
	//
	// 由 S3 协议规定（10000）。因此单次上传上限约为 MaxPartSize × MaxParts，
	// 超出时应由业务分卷后再上传。
	MaxParts = 10000

	// DefaultConcurrency 是 Put 未指定 Concurrency 时并发的分片上传数。
	//
	// 默认 3 兼顾吞吐与内存：峰值内存约为 PartSize × Concurrency。
	DefaultConcurrency = 3

	// DefaultListPageSize 是 List 未指定 MaxKeys 时的每页条数。
	//
	// 计数包含对象与公共前缀（目录视图下的子目录也算一条）。
	DefaultListPageSize = 1000

	// MaxPresignExpiry 是预签名 URL 的最长有效期。
	//
	// 取 7 天，与 S3 协议上限一致；PresignOptions.Expires 超过它时返回
	// ErrInvalidArgument，而不是静默截断。
	MaxPresignExpiry = 7 * 24 * time.Hour
)

var (
	// ErrObjectNotFound 表示对象不存在。
	//
	// Stat、Get、Copy（源对象）在这时返回包装了它的错误；Exists 返回
	// (false, nil)。实现可能处于分片上传中的对象视为不存在。
	ErrObjectNotFound = errors.New("storage: object not found")

	// ErrBucketNotFound 表示服务端明确返回了 NoSuchBucket。基于 HEAD 的
	// Stat/Exists/Get 无法区分"桶不存在"与"对象不存在"（404 无错误体），
	// 此时会归为 ErrObjectNotFound；桶的存在性由调用方保证。
	ErrBucketNotFound = errors.New("storage: bucket not found")

	// ErrAccessDenied 表示凭据无效、签名错误或权限不足。
	//
	// 对应 HTTP 403。常见原因：AK/SK 错误、子账号缺少该前缀权限、桶策略拒绝、
	// 系统时间偏差过大导致签名过期。遇到它不要重试，先查凭据与权限。
	ErrAccessDenied = errors.New("storage: access denied")

	// ErrPreconditionFailed 表示服务端返回 412（如桶策略或服务端强制的禁止覆盖），
	// 调用方主动发起的条件写不在契约内：所有上传都是覆盖写。
	ErrPreconditionFailed = errors.New("storage: precondition failed")

	// ErrInvalidRange 表示请求的字节区间不被服务端接受（通常是起始位置越界）。
	//
	// 只有服务端明确返回 416 时才会出现。契约对越界区间是宽容的（End 超长
	// 截断、Start 超长返回空流），所以正常调用下不应看到它。
	ErrInvalidRange = errors.New("storage: invalid range")

	// ErrUnsupported 表示服务端不支持该能力（HTTP 501 或 NotImplemented）。
	//
	// 典型场景：兼容端点不支持分片复制、预签名参数或某些校验头。遇到它应
	// 降级为普通实现（如复制改用读+写）或调整配置。
	ErrUnsupported = errors.New("storage: unsupported operation")

	// ErrInvalidArgument 表示调用方传入了非法参数。
	//
	// 典型场景：bucket/key 为空、PartSize 越界、预签名有效期非法、prefix 为空
	// （DeletePrefix）。属于编码或配置错误，不应重试。
	ErrInvalidArgument = errors.New("storage: invalid argument")
)

// Client 是对象存储的统一操作契约。实现必须可被多个 goroutine 并发调用。
//
// 本类型是行为的权威说明：各方法只写自身语义，具体实现（storage/s3）只补充实现
// 特有的阈值、编码方式与厂商兼容点，不重复这里的内容。
//
// 错误约定：实现返回的错误都可用 errors.Is 判断下列哨兵——ErrObjectNotFound、
// ErrBucketNotFound、ErrAccessDenied、ErrPreconditionFailed、ErrInvalidRange、
// ErrUnsupported、ErrInvalidArgument；批量删除的单项失败是 *DeleteError。
//
// 缺失语义：Stat、Get 与 Copy 的源对象不存在时返回包装了 ErrObjectNotFound
// 的错误；Delete 与 DeleteMany 幂等，对象不存在不报错；Exists 只在对象确实
// 不存在时返回 false，权限不足等错误必须原样返回。
//
// 桶语义：契约不做桶级存在性检查，桶必须由调用方预先保证存在。基于 HEAD 的
// Stat/Exists/Get 无法区分"桶不存在"与"对象不存在"（404 无错误体），统一归为
// ErrObjectNotFound；ErrBucketNotFound 只在服务端返回 NoSuchBucket 错误体时
// 出现（如 List、DeleteObjects）。
//
// 写入语义：所有上传都是覆盖写，不做条件写；同名对象被整体替换，元数据也一并
// 替换，不会与前一次上传合并。
type Client interface {
	// ListBuckets 列举当前凭据可见的桶名，按字典序返回；无桶时返回空切片。
	ListBuckets(ctx context.Context) ([]string, error)

	// Stat 只发 HEAD 不取内容，返回对象元信息；Metadata 的键统一小写。
	// 对象不存在返回包装 ErrObjectNotFound 的错误。
	Stat(ctx context.Context, bucket, key string) (ObjectInfo, error)

	// Exists 报告对象是否存在：确定不存在返回 (false, nil)；权限不足、网络错误
	// 等无法判断的情况返回 (false, err)，不得把错误伪装成"不存在"。
	Exists(ctx context.Context, bucket, key string) (bool, error)

	// Get 返回对象内容流，调用方负责 Close。Info 由响应头填充，区间请求时 Size
	// 为对象总大小；opts.Range 为 nil 表示读取整个对象。
	Get(ctx context.Context, bucket, key string, opts ReaderOptions) (*Reader, error)

	// GetBytes 把整个对象（或 opts.Range 指定区间）读入内存，返回的字节已脱离
	// 连接，调用方无需再关闭任何东西。
	GetBytes(ctx context.Context, bucket, key string, opts ReaderOptions) ([]byte, error)

	// Put 返回流式写入器：数据缓冲到 PartSize 后转分片上传。Close 提交、Abort
	// 放弃，两者必须择一调用；写入器不可并发使用。
	Put(ctx context.Context, bucket, key string, opts WriterOptions) (Writer, error)

	// PutBytes 一次写入内存中的字节；超过 PartSize 时内部转分片上传。
	PutBytes(ctx context.Context, bucket, key string, data []byte, opts WriterOptions) error

	// Upload 把本地文件 path 上传为对象；读取本地文件失败时不得提交截断的对象。
	Upload(ctx context.Context, bucket, key, path string, opts WriterOptions) error

	// Download 把对象写到本地 path：父目录不存在时创建，已存在则覆盖；失败时
	// 不得破坏已有文件，也不得留下半个文件。
	Download(ctx context.Context, bucket, key, path string, opts ReaderOptions) error

	// Delete 删除单个对象，对象不存在视为成功（幂等）。
	Delete(ctx context.Context, bucket, key string) error

	// DeleteMany 批量删除，由实现按服务端上限分批；单项失败以 *DeleteError 记录，
	// 用 errors.Join 聚合返回，调用方必须检查返回值。
	DeleteMany(ctx context.Context, bucket string, keys []string) error

	// DeletePrefix 删除前缀下的全部对象，逐页列举以免整批入内存；prefix 为空
	// 返回 ErrInvalidArgument，避免误删整个桶。
	DeletePrefix(ctx context.Context, bucket, prefix string) error

	// Copy 在服务端复制（支持跨桶），数据不经过本进程；大对象由实现自动转分片
	// 复制并保留源元信息。源对象不存在时返回包装 ErrObjectNotFound 的错误。
	Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error

	// Move 由 Copy 与 Delete 组合实现，非原子：复制成功而删除源失败时返回错误，
	// 此时两个对象同时存在。
	Move(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error

	// List 返回一页列举结果：opts 控制前缀、分隔符、游标与每页条数；返回的
	// NextCursor 非空时原样回传给下一次调用，为空表示已到末尾。
	List(ctx context.Context, bucket string, opts ListOptions) (ListPage, error)

	// ListAll 自动翻页，返回前缀下的全部对象（按 key 字典序），结果一次性进内存。
	ListAll(ctx context.Context, bucket, prefix string) ([]ObjectInfo, error)

	// Walk 按 key 字典序流式遍历前缀下的对象，逐条回调；fn 返回错误时立即停止
	// 并原样返回该错误，内存占用与对象总数无关。
	Walk(ctx context.Context, bucket, prefix string, fn func(ObjectInfo) error) error

	// ListDir 返回某一层级的目录视图：Dirs 为本层文件夹名（不含前缀、不以 "/"
	// 结尾），Objects 为本层直接对象。prefix 缺少尾部 "/" 时由实现自动补齐。
	ListDir(ctx context.Context, bucket, prefix string) (DirInfo, error)

	// PresignGet 生成下载预签名 URL：opts.Expires 必须大于 0 且不超过
	// MaxPresignExpiry，否则返回 ErrInvalidArgument；opts.FileName 非空时通过
	// 响应头覆写下载文件名。返回的 URL 是短期凭据。
	PresignGet(ctx context.Context, bucket, key string, opts PresignOptions) (PresignedURL, error)

	// PresignPut 生成上传预签名 URL：opts.Expires 同上，opts.FileName 被忽略；
	// 上传不约束 Content-Type，且会覆盖同名对象。
	PresignPut(ctx context.Context, bucket, key string, opts PresignOptions) (PresignedURL, error)
}

// DeleteError 是批量删除中单个对象的失败。DeleteMany 用 errors.Join 聚合，
// 调用方可用 errors.As 取出 Key，用 errors.Is/As 检查底层原因。
//
// 批量删除是尽力而为的：部分失败时已验证删除成功的对象不会回滚，调用方应
// 根据逐个 DeleteError 决定是否重试。
type DeleteError struct {
	// Key 是删除失败的对象键。
	Key string
	// Err 是服务端返回的底层错误，通常是包装后的 ErrAccessDenied 等哨兵。
	Err error
}

// Error 实现 error。
func (e *DeleteError) Error() string {
	return fmt.Sprintf("storage: 删除 %s 失败: %v", e.Key, e.Err)
}

// Unwrap 暴露底层错误，供 errors.Is / errors.As 穿透。
func (e *DeleteError) Unwrap() error { return e.Err }

// Reader 是对象内容流，调用方必须 Close。
//
// 不 Close 会泄漏连接，使连接池无法复用。读取失败（网络中断、区间失效）会在
// Read 时以错误返回，不会返回部分内容。
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
	// Key 是对象键（完整路径，不含桶名）。
	Key string `json:"key"`
	// Size 是对象字节数。
	Size int64 `json:"size"`
	// LastModified 是服务端记录的最后修改时间。
	LastModified time.Time `json:"lastModified"`
	// ETag 是不透明的实体标识，不承诺是 MD5：OSS 与分片上传的算法与 S3 不同。
	ETag string `json:"etag"`
	// ContentType 是对象的内容类型；列举结果中可能为空。
	ContentType string `json:"contentType"`
	// ContentDisposition 是下载时的响应头建议；列举结果中可能为空。
	ContentDisposition string `json:"contentDisposition"`
	// CacheControl 是缓存指令；列举结果中可能为空。
	CacheControl string `json:"cacheControl"`
	// StorageClass 是存储类型（如 STANDARD、IA）；兼容端点可能为空。
	StorageClass string `json:"storageClass"`
	// Metadata 是自定义元数据，键已转为小写，不含 Content-Type 等标准头。
	Metadata map[string]string `json:"metadata"`
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
// 字段零值的含义：{0, 0} 是空区间（不是整个对象），要读全部请把 ReaderOptions.Range
// 留为 nil。
type ByteRange struct {
	// Start 是区间起点（含）；为负数时表示取末尾 |Start| 字节。
	Start int64
	// End 是区间终点（不含）；小于 0 表示直到对象结尾。
	End int64
}

// String 实现 fmt.Stringer。
func (r ByteRange) String() string {
	return fmt.Sprintf("ByteRange(start: %d, end: %d)", r.Start, r.End)
}

// ReaderOptions 是读取参数，零值表示读取整个对象。
//
// 目前只有区间读取一项；把 Range 留为 nil 即全量读取，不要用 {0, 0} 代替。
type ReaderOptions struct {
	// Range 为 nil 表示读取整个对象；非 nil 时按区间读取，越界宽容（见 ByteRange）。
	Range *ByteRange
}

// WriterOptions 是写入参数，零值表示默认分片大小与并发度、不附加元数据。
//
// 元数据类字段（ContentType 等）只影响本次对象的响应头，不参与内容编码。
type WriterOptions struct {
	// ContentType 是对象的内容类型，如 "text/csv"；空表示交给服务端默认。
	ContentType string
	// ContentDisposition 是下载时的响应头建议，如 `attachment; filename="a.csv"`；空表示不设置。
	ContentDisposition string
	// CacheControl 是缓存指令，如 "max-age=3600"；空表示不设置。
	CacheControl string
	// Metadata 是自定义元数据，键写入前统一转小写。
	Metadata map[string]string

	// PartSize 是分片阈值与单片大小，范围 [MinPartSize, MaxPartSize]，
	// 0 表示 DefaultPartSize。数据不超过 PartSize 时走单次 PutObject。
	// 非 0 且越界时返回 ErrInvalidArgument。
	PartSize int

	// Concurrency 是并发上传的分片数，0 表示 DefaultConcurrency；
	// 负数返回 ErrInvalidArgument。峰值内存约为 PartSize × Concurrency。
	Concurrency int
}

// ListOptions 是列举参数，零值表示从头列举、不分组、使用默认每页条数。
//
// 要浏览"目录"，把 Delimiter 设为 "/"，子目录会出现在 CommonPrefixes；要拿
// 全部对象则留空。
type ListOptions struct {
	// Prefix 是对象键前缀，空表示从头列举。
	Prefix string
	// Delimiter 为空表示不分组；"/" 表示按目录视图分组，子目录出现在 CommonPrefixes。
	Delimiter string
	// Cursor 是上一页返回的 NextCursor，空表示从头开始；游标不透明，不要自行构造。
	Cursor string
	// MaxKeys 是每页条数上限，<= 0 使用 DefaultListPageSize；计数含对象与公共前缀。
	MaxKeys int
}

// ListPage 是一页列举结果，Objects 与 CommonPrefixes 均按 key 字典序。
//
// 翻页用 NextCursor：为空表示已到末尾，非空时原样回传给下一次 List。
// MaxKeys 同时限制两者数量，所以一页可能只有几个对象。
type ListPage struct {
	// Objects 是本页对象，按 key 字典序。
	Objects []ObjectInfo `json:"objects"`
	// CommonPrefixes 是目录视图下的子目录前缀，仅在 Delimiter 非空时出现，元素是完整前缀。
	CommonPrefixes []string `json:"commonPrefixes"`
	// NextCursor 为空表示已到末尾；非空时必须原样回传给 ListOptions.Cursor。
	NextCursor string `json:"nextCursor"`
	// PageSize 是本页生效的每页条数（含对象与公共前缀）。
	PageSize int `json:"pageSize"`
}

// String 实现 fmt.Stringer；元素复用各自的 String。
func (p ListPage) String() string {
	return fmt.Sprintf("ListPage(objects: %v, commonPrefixes: %v, nextCursor: %s, pageSize: %d)",
		p.Objects, p.CommonPrefixes, p.NextCursor, p.PageSize)
}

// PresignOptions 是预签名参数。
//
// 零值不可用：Expires 必须显式给出且不超过 MaxPresignExpiry，否则返回
// ErrInvalidArgument。
type PresignOptions struct {
	// Expires 是链接有效期，必须 > 0 且不超过 MaxPresignExpiry，否则返回 ErrInvalidArgument。
	Expires time.Duration
	// FileName 仅 PresignGet 有效，通过响应头覆写下载文件名；PresignPut 忽略。
	FileName string
}

// PresignedURL 是预签名结果。Headers 非空时，请求该 URL 必须原样携带。
//
// 上传预签名一般不需要额外头，下载预签名在指定 FileName 时会带上响应头参数。
type PresignedURL struct {
	// URL 是已签名的完整地址，含查询参数。
	URL string `json:"url"`
	// Headers 是签名时固化的请求头；非空时使用方必须原样携带，否则签名失效。
	Headers map[string]string `json:"headers"`
}

// String 实现 fmt.Stringer。
func (p PresignedURL) String() string {
	return fmt.Sprintf("PresignedURL(url: %s, headers: %v)", p.URL, p.Headers)
}

// DirInfo 是某一个层级的目录视图，供浏览器式界面使用。
//
// 与 List 的区别：List 返回完整前缀（含本层目录），DirInfo 把子目录折叠成名字，
// 前端可以直接渲染成"文件夹 + 文件"列表。
type DirInfo struct {
	// Prefix 是查询前缀（已补齐尾部 "/"）。
	Prefix string `json:"prefix"`
	// Dirs 是本层文件夹名，不含 Prefix、不以 "/" 结尾，按字典序。
	Dirs []string `json:"dirs"`
	// Objects 是本层直接对象，按字典序。
	Objects []ObjectInfo `json:"objects"`
}

// String 实现 fmt.Stringer；元素复用各自的 String。
func (d DirInfo) String() string {
	return fmt.Sprintf("DirInfo(prefix: %s, dirs: %v, objects: %v)", d.Prefix, d.Dirs, d.Objects)
}
