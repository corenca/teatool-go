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
type ChecksumMode string

const (
	// ChecksumWhenRequired 只在调用方显式要求时计算校验值，兼容旧版端点。
	ChecksumWhenRequired ChecksumMode = "when_required"

	// ChecksumAlways 对所有上传计算校验值，要求端点支持 aws-chunked。
	ChecksumAlways ChecksumMode = "always"
)

// Config 是 S3 客户端配置。
type Config struct {
	Endpoint        string       `json:"endpoint" toml:"endpoint"`               // 为空时使用 AWS 默认端点；可带桶前缀（COS 常见形态）
	Region          string       `json:"region" toml:"region"`                   // 为空时回退 us-east-1；OSS 的 oss- 前缀会被去掉
	AccessKeyID     string       `json:"accessKeyId" toml:"accessKeyId"`         // 访问密钥 ID
	SecretAccessKey string       `json:"secretAccessKey" toml:"secretAccessKey"` // 访问密钥
	UsePathStyle    bool         `json:"usePathStyle" toml:"usePathStyle"`       // MinIO 与私有化部署常需要
	Checksum        ChecksumMode `json:"checksum" toml:"checksum"`               // 为空时使用 ChecksumWhenRequired
	HTTPClient      *http.Client `json:"-" toml:"-"`                             // 为空时使用 SDK 默认
}

// Client 是 S3 客户端，实现 storage.Client。
type Client struct {
	config Config
	sdk    *awss3.Client
}

// New 创建 S3 客户端。endpoint 与 region 在此完成归一化，整个客户端只构造
// 一个 SDK 实例；桶前缀差异由 bucketEndpointResolver 在解析端点时处理。
func New(config Config) (*Client, error) {
	if config.AccessKeyID == "" || config.SecretAccessKey == "" {
		return nil, fmt.Errorf("%w: access key 与 secret key 不能为空", storage.ErrInvalidArgument)
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

	awsConfig := aws.Config{
		Region:      config.Region,
		Credentials: credentials.NewStaticCredentialsProvider(config.AccessKeyID, config.SecretAccessKey, ""),
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
