// Package s3 用 AWS S3 SDK 实现对象存储契约，同一份实现覆盖 AWS S3 以及
// OSS、COS、OBS、MinIO 等 S3 兼容端点。
package s3

import (
	"fmt"
	"net/http"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"

	"github.com/corenca/teatool-go/storage"
)

// ChecksumMode 控制上传时是否计算校验值。
//
// 取值必须与端点的能力匹配：AWS S3 与新版兼容端点支持对全部上传计算 CRC 类
// 校验；部分旧版或私有化端点不支持 aws-chunked 编码，强行开启会导致上传失败。
type ChecksumMode string

const (
	// ChecksumWhenRequired 只在调用方显式要求时计算校验值，兼容旧版端点。
	//
	// 零值（空字符串）即等同于此模式，是本包默认策略。
	ChecksumWhenRequired ChecksumMode = "when_required"

	// ChecksumAlways 对所有上传计算校验值，要求端点支持 aws-chunked。
	//
	// 开启后能及早发现传输损坏，但与不兼容的端点搭配时上传会失败，开启前
	// 先用一个小对象验证。
	ChecksumAlways ChecksumMode = "always"
)

// Config 是 S3 客户端配置。字段统一用小驼峰 tag，json / yaml / toml / mapstructure
// 四种键名一致，json、yaml、toml 配置文件与 viper、koanf 这类库都能直接解码。
// 只能代码注入的 HTTPClient 与 Credentials 标 "-"，不会从配置里读取。
//
// 最小可用配置：Endpoint、Region、AccessKeyID、SecretAccessKey（或 Credentials）。
// 四个厂商的差异（endpoint 形态、region 前缀、path style）在 New 里归一化。
type Config struct {
	Endpoint        string       `json:"endpoint" yaml:"endpoint" toml:"endpoint" mapstructure:"endpoint"`                             // 为空时使用 AWS 默认端点；可带桶前缀（COS 常见形态）
	Region          string       `json:"region" yaml:"region" toml:"region" mapstructure:"region"`                                     // 为空时回退 us-east-1；OSS 的 oss- 前缀会被去掉
	AccessKeyID     string       `json:"accessKeyId" yaml:"accessKeyId" toml:"accessKeyId" mapstructure:"accessKeyId"`                 // 访问密钥 ID
	SecretAccessKey string       `json:"secretAccessKey" yaml:"secretAccessKey" toml:"secretAccessKey" mapstructure:"secretAccessKey"` // 访问密钥
	SessionToken    string       `json:"sessionToken" yaml:"sessionToken" toml:"sessionToken" mapstructure:"sessionToken"`             // STS 临时凭据的会话令牌，可空
	UsePathStyle    bool         `json:"usePathStyle" yaml:"usePathStyle" toml:"usePathStyle" mapstructure:"usePathStyle"`             // MinIO 与私有化部署常需要
	Checksum        ChecksumMode `json:"checksum" yaml:"checksum" toml:"checksum" mapstructure:"checksum"`                             // 为空时使用 ChecksumWhenRequired
	Bucket          string       `json:"bucket" yaml:"bucket" toml:"bucket" mapstructure:"bucket"`                                     // 可为空，仅在构造 storage.Service 时使用
	HTTPClient      *http.Client `json:"-" yaml:"-" toml:"-" mapstructure:"-"`                                                         // 为空时使用 SDK 默认

	// Credentials 不为空时优先于静态密钥，用于对接 IAM Role / AssumeRole 等
	// 凭据链；此时 AccessKeyID/SecretAccessKey/SessionToken 可以留空。
	Credentials aws.CredentialsProvider `json:"-" yaml:"-" toml:"-" mapstructure:"-"`
}

// Client 是 S3 客户端，实现 storage.Client。
//
// 必须通过 New 构造，零值不可用。可被多个 goroutine 并发调用，内部只持有一个
// SDK 客户端与不可变配置，没有可变的共享状态。
type Client struct {
	config Config
	sdk    *awss3.Client
}

// New 创建 S3 客户端。endpoint 与 region 在此完成归一化，整个客户端只构造
// 一个 SDK 实例；桶前缀差异由 bucketEndpointResolver 在解析端点时处理。
func New(config Config) (*Client, error) {
	if config.Credentials == nil && (config.AccessKeyID == "" || config.SecretAccessKey == "") {
		return nil, fmt.Errorf("%w: access key 与 secret key 不能为空（或注入 Credentials）", storage.ErrInvalidArgument)
	}
	switch config.Checksum {
	case "":
		config.Checksum = ChecksumWhenRequired
	case ChecksumWhenRequired, ChecksumAlways:
	default:
		return nil, fmt.Errorf("%w: 未知的 checksum 模式 %q", storage.ErrInvalidArgument, config.Checksum)
	}
	config.Endpoint = normalizeEndpoint(config.Endpoint)
	config.Region = normalizeRegion(config.Region)

	credentialsProvider := config.Credentials
	if credentialsProvider == nil {
		credentialsProvider = credentials.NewStaticCredentialsProvider(
			config.AccessKeyID, config.SecretAccessKey, config.SessionToken)
	}
	awsConfig := aws.Config{
		Region:      config.Region,
		Credentials: credentialsProvider,
	}
	if config.HTTPClient != nil {
		awsConfig.HTTPClient = config.HTTPClient
	}
	if config.Checksum == ChecksumWhenRequired {
		awsConfig.RequestChecksumCalculation = aws.RequestChecksumCalculationWhenRequired
		awsConfig.ResponseChecksumValidation = aws.ResponseChecksumValidationWhenRequired
	}

	sdkClient := awss3.NewFromConfig(awsConfig, func(o *awss3.Options) {
		o.UsePathStyle = config.UsePathStyle
		if config.Endpoint != "" {
			o.BaseEndpoint = aws.String(config.Endpoint)
			o.EndpointResolverV2 = bucketEndpointResolver{
				endpoint: config.Endpoint,
				base:     awss3.NewDefaultEndpointResolverV2(),
			}
		}
	})
	return &Client{config: config, sdk: sdkClient}, nil
}

// 编译期断言：S3 客户端完整实现契约。
var _ storage.Client = (*Client)(nil)
