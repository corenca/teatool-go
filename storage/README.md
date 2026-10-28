# storage

对象存储的统一操作契约与 S3 实现。同一份代码覆盖 AWS S3 与 OSS、COS、OBS、MinIO
等 S3 兼容端点；`storage` 包零第三方依赖，`storage/s3` 是唯一的厂商实现。

## 快速开始

```go
import (
	"github.com/corenca/teatool-go/storage"
	"github.com/corenca/teatool-go/storage/s3"
)

client, err := s3.New(s3.Config{
	Endpoint:        "https://oss-cn-hangzhou.aliyuncs.com",
	Region:          "cn-hangzhou",
	AccessKeyID:     os.Getenv("OSS_AK"),
	SecretAccessKey: os.Getenv("OSS_SK"),
})
if err != nil {
	return err
}

err = client.PutBytes(ctx, "bucket", "a/b.txt", []byte("hello"), storage.WriterOptions{
	ContentType: "text/plain",
})
```

只需要操作单个桶时，用 `storage.NewService(client, bucket)` 固定桶名，方法签名会
去掉 `bucket` 参数。跨桶操作直接使用 `Client`（构造 `Service` 前保留引用即可）。

## 契约语义

- **缺失**：`Stat` / `Get` / `Copy` 的源对象不存在返回 `ErrObjectNotFound`；
  `Delete` / `DeleteMany` 幂等，不存在的对象不报错；`Exists` 只在确实不存在时
  返回 `false`，权限错误原样返回。
- **桶**：契约不做桶级存在性检查，桶由调用方保证存在。`Stat`/`Exists`/`Get` 走
  HEAD，无法区分"桶不存在"和"对象不存在"（404 无错误体），统一返回
  `ErrObjectNotFound`；只有服务端返回 `NoSuchBucket` 错误体的接口才会给出
  `ErrBucketNotFound`。
- **错误分类**：`ErrObjectNotFound`、`ErrBucketNotFound`、`ErrAccessDenied`、
  `ErrPreconditionFailed`（服务端 412）、`ErrInvalidRange`、`ErrUnsupported`、
  `ErrInvalidArgument`，全部可用 `errors.Is` 判断。批量删除的单个失败是
  `*storage.DeleteError`，可用 `errors.As` 取出 `Key`。
- **区间读取**：`ByteRange` 前闭后开；`End < 0` 表示到结尾；`Start < 0` 表示
  末尾 `|Start|` 字节。起点越界返回空流而不是错误。
- **写入**：`Put` 返回 `Writer`，`Close` 提交、`Abort` 放弃。`Close` 失败或
  调用方放弃时必须 `Abort`，否则可能留下未完成的分片上传。数据不超过
  `PartSize` 时走单次 `PutObject`，超过才转分片上传（`PartSize` 取值
  5MiB–5GiB，`Concurrency` 默认 3，峰值内存约为两者之积）。**所有上传都是
  覆盖写**：同名 key 直接覆盖，不做条件写。
- **列举**：`List` 是一页，`ListAll` 全量入内存，`Walk` 流式逐条回调（大地
  前缀优先用 `Walk`）。`ListDir` 返回单层目录视图，前缀缺少尾部 `/` 时自动补齐。
- **删除**：`DeleteMany` 自动按服务端上限分批，部分失败用 `errors.Join` 聚合；
  `DeletePrefix` 逐页列举再删除，不需把整批 key 读入内存。
- **下载**：`Download` 先写同目录临时文件再 `rename`，失败不会破坏已有文件。
- **复制**：`Copy` 超过 5GiB 自动转分片复制，并保留源对象的元信息；`Move` 是
  `Copy` + `Delete`，非原子。

## 配置

| 字段 | 说明 |
| --- | --- |
| `Endpoint` | 为空走 AWS 默认端点；可带桶前缀（COS 常见形态），解析时自动剥离 |
| `Region` | 为空回退 `us-east-1`；OSS 的 `oss-` 前缀自动去掉 |
| `AccessKeyID` / `SecretAccessKey` | 静态凭据 |
| `SessionToken` | STS 临时凭据的会话令牌 |
| `Credentials` | 注入凭据链（IAM Role / AssumeRole），优先于静态凭据 |
| `UsePathStyle` | MinIO 与私有化部署通常需要 |
| `Checksum` | `when_required`（默认）或 `always`，后者要求端点支持 `aws-chunked` |
| `Bucket` | 可为空，仅在构造 `storage.Service` 时使用 |
| `HTTPClient` | 自定义超时、代理、TLS 或埋点传输层 |

配置可用任一配置库解码；字段同时带 `json` 与小驼峰 `mapstructure` tag。

### 配置驱动的工厂

```go
factory, err := storage.NewFactory(func(raw map[string]any, target any) error {
	return mapstructure.Decode(raw, target) // 也可用 viper.Sub(name).Unmarshal(target)
})
if err != nil {
	return err
}
if err := storage.RegisterBackend(factory, "s3", s3.New); err != nil {
	return err
}

client, err := factory.NewClient("s3", rawConfig)   // Client
service, err := factory.NewService("s3", rawConfig) // 绑定 Config.Bucket 的 Service
```

同一驱动可注册成多个名字（如 `s3`、`s3Backup`）；未注册的名字返回
`ErrBackendNotFound`。

## 厂商差异

- 批量删除固定带 `Content-MD5`（OSS/COS 强制要求，S3 与 MinIO 同样接受）。
- `PresignPut` 无法约束对象的 `Content-Type`：SDK 会剥离该请求头，对象类型由
  上传请求或服务端默认值决定。
- `PresignGet` 的 `FileName` 按 RFC 6266 生成 `Content-Disposition`，中文文件名
  同时带 ASCII 回退与 `filename*=UTF-8''`。
- `ETag` 不承诺是 MD5：OSS 与分片上传的算法与 S3 不同，需要校验请自行比对内容。
- 服务端不支持某项能力时返回 `ErrUnsupported`（HTTP 501 / `NotImplemented`），
  例如旧端点拒绝 `aws-chunked` 校验。

## 明确的非目标

桶管理（创建/删除/ACL/生命周期/版本控制/标签）、断点续传、重试策略、限速、
厂商专有能力（OSS 图片处理、COS CI 等）、条件写（`If-None-Match`/禁止覆盖）。
`Client` 必须可并发调用；`Writer` 本身不可并发使用。
