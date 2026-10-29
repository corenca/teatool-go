# storage

对象存储的统一操作契约与 S3 实现。同一份代码覆盖 AWS S3 与 OSS、COS、OBS、MinIO
等 S3 兼容端点；`storage` 包零第三方依赖，`storage/s3` 是唯一的厂商实现。

## 使用方式

### 直接用 Client

`Client` 的每个方法都带 `bucket` 参数，适合多桶与跨桶场景。

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/corenca/teatool-go/storage"
	"github.com/corenca/teatool-go/storage/s3"
)

func main() {
	client, err := s3.New(s3.Config{
		Endpoint:        "https://oss-cn-hangzhou.aliyuncs.com",
		Region:          "cn-hangzhou",
		AccessKeyID:     os.Getenv("OSS_AK"),
		SecretAccessKey: os.Getenv("OSS_SK"),
	})
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	// Client 的每个方法都要传 bucket，多桶与跨桶操作直接用它
	if err := client.PutBytes(ctx, "demo", "a/b.txt", []byte("hello"),
		storage.WriterOptions{ContentType: "text/plain"}); err != nil {
		log.Fatal(err)
	}
	if err := client.Copy(ctx, "demo", "a/b.txt", "backup", "a/b.txt"); err != nil {
		log.Fatal(err)
	}
	fmt.Println("demo/a/b.txt 已写入并复制到 backup/a/b.txt")
}
```

### 直接用 Service

只操作一个桶时，用 `storage.NewService(client, bucket)` 绑定桶名，方法签名会去掉
`bucket` 参数，其余语义与 `Client` 完全一致。跨桶操作仍用 `Client`（构造 `Service`
前保留引用即可）。

```go
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/corenca/teatool-go/storage"
	"github.com/corenca/teatool-go/storage/s3"
)

func main() {
	client, err := s3.New(s3.Config{
		Endpoint:        "https://oss-cn-hangzhou.aliyuncs.com",
		Region:          "cn-hangzhou",
		AccessKeyID:     os.Getenv("OSS_AK"),
		SecretAccessKey: os.Getenv("OSS_SK"),
	})
	if err != nil {
		log.Fatal(err)
	}

	// Service 绑定单个桶，方法签名里不再有 bucket 参数
	service, err := storage.NewService(client, "demo")
	if err != nil {
		log.Fatal(err)
	}

	ctx := context.Background()
	if err := service.PutBytes(ctx, "a/b.txt", []byte("hello"),
		storage.WriterOptions{ContentType: "text/plain"}); err != nil {
		log.Fatal(err)
	}
	info, err := service.Stat(ctx, "a/b.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s 中的 %s：%d 字节\n", service.Bucket(), info.Key, info.Size)
}
```

### 从配置文件加载

多实例（多桶、多厂商）通常写在配置文件里。装配由业务方在启动时完成：读出 `storage`
段，为每个实例 `s3.New` 一个客户端，需要单桶视图的再套 `storage.NewService`，最后
按名字取用。库不提供全局注册中心或工厂——`s3` 是唯一的实现，装配属于业务方。

`config.yaml`：

```yaml
storage:
  files:                    # 实例名，业务方自己的 map key
    endpoint: https://oss-cn-hangzhou.aliyuncs.com
    region: cn-hangzhou
    accessKeyId: your-ak
    secretAccessKey: your-sk
    bucket: demo
  backup:
    endpoint: https://cos.ap-guangzhou.myqcloud.com
    region: ap-guangzhou
    accessKeyId: your-ak
    secretAccessKey: your-sk
    bucket: backup
```

```go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/go-viper/mapstructure/v2"
	"github.com/spf13/viper"

	"github.com/corenca/teatool-go/storage"
	"github.com/corenca/teatool-go/storage/s3"
)

// store 是业务方自己的实例表：实例名 -> 客户端与单桶 Service。
type store struct {
	clients  map[string]storage.Client
	services map[string]*storage.Service
}

func main() {
	v := viper.New()
	v.SetConfigFile("config.yaml")
	if err := v.ReadInConfig(); err != nil {
		log.Fatalf("读取配置失败: %v", err)
	}
	// 密钥建议用 v.BindEnv("storage.files.secretAccessKey", "OSS_SK") 注入

	raws := v.GetStringMap("storage")
	if len(raws) == 0 {
		log.Fatal("配置缺少 storage 段")
	}

	st := &store{
		clients:  make(map[string]storage.Client, len(raws)),
		services: make(map[string]*storage.Service, len(raws)),
	}
	for name, value := range raws {
		var config s3.Config
		if err := mapstructure.Decode(value, &config); err != nil {
			log.Fatalf("解析 storage.%s: %v", name, err)
		}
		client, err := s3.New(config)
		if err != nil {
			log.Fatalf("初始化 storage.%s: %v", name, err)
		}
		st.clients[name] = client
		// bucket 非空才有单桶视图；示例假设配置里都填了
		if config.Bucket != "" {
			service, err := storage.NewService(client, config.Bucket)
			if err != nil {
				log.Fatalf("初始化 storage.%s 的 Service: %v", name, err)
			}
			st.services[name] = service
		}
	}
	fmt.Printf("已初始化 %d 个客户端\n", len(st.clients))

	// 用起来和前面一致：单桶走 Service，跨桶走 Client
	ctx := context.Background()
	files := st.services["files"]
	if err := files.PutBytes(ctx, "a/hello.txt", []byte("hello"),
		storage.WriterOptions{ContentType: "text/plain"}); err != nil {
		log.Fatal(err)
	}
	if err := st.clients["files"].Copy(ctx, files.Bucket(), "a/hello.txt",
		st.services["backup"].Bucket(), "a/copy.txt"); err != nil {
		log.Fatal(err)
	}
}
```

要点：

- 每个实例都是 `s3.New(s3.Config)`：OSS/COS/OBS/MinIO 用同一份实现，只改
  `endpoint` / `region`（MinIO 与私有化部署再打开 `usePathStyle`）。
- `bucket` 非空的实例顺手 `storage.NewService`，单桶操作就不必反复传桶名；
  跨桶操作保留 `clients[name]`。
- viper 会把键名小写化（`accesskeyid`），`mapstructure` 匹配大小写不敏感，
  `s3.Config` 的小驼峰 tag 直接可用。
- 装配代码留在业务方：密钥来源、失败是中断启动还是降级、是否预热，都由业务决定。
- 将来若出现非 S3 协议的独立实现、且需要运行时按配置选实现，再按 `database/sql`
  的驱动模式引入注册中心；现在引入只会多一层没有多实现的抽象。

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
| `Bucket` | 可为空；业务方据此调 `storage.NewService` 绑定单桶 |
| `HTTPClient` | 自定义超时、代理、TLS 或埋点传输层 |

配置可用任一配置库解码：`s3.Config` 每个字段都带 `json` / `yaml` / `toml` /
`mapstructure` 四种 tag，键名统一为小驼峰（`accessKeyId`、`usePathStyle`），JSON、
YAML、TOML 文件与 viper、koanf 这类库都能直接解码。`HTTPClient` 与 `Credentials`
标 `-`，只能代码注入。

装配方式：`s3.New(config)` 直接构造客户端，`storage.NewService(client, bucket)`
把客户端绑到单个桶。库不提供全局注册中心或按名字构造的工厂，多实例的装配由业务方
在启动时完成（见上文「从配置文件加载」）。

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
