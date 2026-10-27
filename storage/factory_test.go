package storage_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"testing"

	"github.com/corenca/teatool-go/storage"
)

// jsonDecoder 用标准库 JSON 往返把配置 map 解进目标结构体，避免为工厂测试
// 引入 mapstructure/viper 依赖。
func jsonDecoder(raw map[string]any, target any) error {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, target)
}

// factoryConfig 是假后端的配置结构体。
type factoryConfig struct {
	Endpoint string `json:"endpoint" mapstructure:"endpoint"`
	Bucket   string `json:"bucket" mapstructure:"bucket"`
}

// factoryClient 用内嵌接口满足 storage.Client 的 19 个方法；测试只读取字段。
type factoryClient struct {
	storage.Client
	endpoint string
	bucket   string
}

// factoryBucketClient 在 factoryClient 之外实现 storage.Bucketer。
type factoryBucketClient struct {
	storage.Client
	bucket string
}

// Bucket 返回配置里的桶名。
func (c *factoryBucketClient) Bucket() string { return c.bucket }

// newFactoryClient 是不带桶能力的假后端构造函数。
func newFactoryClient(config factoryConfig) (*factoryClient, error) {
	return &factoryClient{endpoint: config.Endpoint, bucket: config.Bucket}, nil
}

// newFactoryBucketClient 是带桶能力的假后端构造函数。
func newFactoryBucketClient(config factoryConfig) (*factoryBucketClient, error) {
	return &factoryBucketClient{bucket: config.Bucket}, nil
}

// errDecode 是解码失败的哨兵错误。
var errDecode = errors.New("decode boom")

// TestNewFactoryRejectsNilDecoder 校验解码器缺失时构造失败。
func TestNewFactoryRejectsNilDecoder(t *testing.T) {
	factory, err := storage.NewFactory(nil)
	t.Logf("NewFactory(nil) factory=%v err=%v", factory, err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}

// TestFactoryZeroValue 校验零值与 nil 工厂的入口都返回错误而不是 panic。
func TestFactoryZeroValue(t *testing.T) {
	var zero storage.Factory
	nilFactory := (*storage.Factory)(nil)

	cases := []struct {
		name string
		call func() error
	}{
		{"零值注册", func() error { return storage.RegisterBackend(&zero, "fake", newFactoryClient) }},
		{"零值 NewClient", func() error {
			_, err := zero.NewClient("fake", nil)
			return err
		}},
		{"零值 NewService", func() error {
			_, err := zero.NewService("fake", nil)
			return err
		}},
		{"nil 注册", func() error { return storage.RegisterBackend(nilFactory, "fake", newFactoryClient) }},
		{"nil NewClient", func() error {
			_, err := nilFactory.NewClient("fake", nil)
			return err
		}},
		{"nil NewService", func() error {
			_, err := nilFactory.NewService("fake", nil)
			return err
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			t.Logf("%s error = %v", tc.name, err)
			if !errors.Is(err, storage.ErrInvalidArgument) {
				t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
			}
		})
	}
}

// TestRegisterBackendValidation 校验注册期的参数校验。
func TestRegisterBackendValidation(t *testing.T) {
	factory, err := storage.NewFactory(jsonDecoder)
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}

	var nilCtor func(factoryConfig) (*factoryClient, error)
	cases := []struct {
		name string
		call func() error
	}{
		{"空名字", func() error { return storage.RegisterBackend(factory, "", newFactoryClient) }},
		{"nil 构造函数", func() error { return storage.RegisterBackend(factory, "nilCtor", nilCtor) }},
		{"非结构体配置", func() error {
			return storage.RegisterBackend(factory, "intCfg", func(int) (*factoryClient, error) { return nil, nil })
		}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.call()
			t.Logf("%s error = %v", tc.name, err)
			if !errors.Is(err, storage.ErrInvalidArgument) {
				t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
			}
		})
	}

	if err := storage.RegisterBackend(factory, "fake", newFactoryClient); err != nil {
		t.Fatalf("首次注册失败: %v", err)
	}
	err = storage.RegisterBackend(factory, "fake", newFactoryClient)
	t.Logf("重复注册 fake error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("重复注册期望 ErrInvalidArgument，实际 %v", err)
	}
}

// TestNewClientUnknownBackend 校验未注册名字的错误与已注册名单。
func TestNewClientUnknownBackend(t *testing.T) {
	factory, err := storage.NewFactory(jsonDecoder)
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	for _, name := range []string{"s3", "s3Backup"} {
		if err := storage.RegisterBackend(factory, name, newFactoryClient); err != nil {
			t.Fatalf("注册 %s 失败: %v", name, err)
		}
	}

	_, err = factory.NewClient("gcs", nil)
	t.Logf("未注册 gcs 的 error = %v", err)
	if !errors.Is(err, storage.ErrBackendNotFound) {
		t.Fatalf("期望 ErrBackendNotFound，实际 %v", err)
	}
	for _, name := range []string{"s3", "s3Backup"} {
		if !strings.Contains(err.Error(), name) {
			t.Errorf("错误消息缺少已注册后端 %q: %v", name, err)
		}
	}
}

// TestNewClientDecodesAndConstructs 校验配置被解码进后端自己的结构体。
func TestNewClientDecodesAndConstructs(t *testing.T) {
	factory, err := storage.NewFactory(jsonDecoder)
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	if err := storage.RegisterBackend(factory, "fake", newFactoryClient); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	raw := map[string]any{"endpoint": "https://example.com", "bucket": "demo"}
	client, err := factory.NewClient("fake", raw)
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}
	fake, ok := client.(*factoryClient)
	if !ok {
		t.Fatalf("客户端类型 = %T，期望 *factoryClient", client)
	}
	t.Logf("raw=%v -> endpoint=%q bucket=%q", raw, fake.endpoint, fake.bucket)
	if fake.endpoint != "https://example.com" || fake.bucket != "demo" {
		t.Fatalf("解码结果 = %+v", fake)
	}
}

// TestNewClientNilRawIsEmptyMap 校验 nil 配置被归一化成空 map。
func TestNewClientNilRawIsEmptyMap(t *testing.T) {
	var got map[string]any
	factory, err := storage.NewFactory(func(raw map[string]any, target any) error {
		got = raw
		return jsonDecoder(raw, target)
	})
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	if err := storage.RegisterBackend(factory, "fake", newFactoryClient); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	if _, err := factory.NewClient("fake", nil); err != nil {
		t.Fatalf("NewClient(nil) error = %v", err)
	}
	t.Logf("Decoder 收到 raw=%v（len=%d，nil=%v）", got, len(got), got == nil)
	if got == nil {
		t.Fatal("Decoder 收到 nil map，期望空 map")
	}
}

// TestNewClientDecodeError 校验解码错误保留 %w 链并带上后端名。
func TestNewClientDecodeError(t *testing.T) {
	factory, err := storage.NewFactory(func(map[string]any, any) error { return errDecode })
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	if err := storage.RegisterBackend(factory, "fake", newFactoryClient); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	_, err = factory.NewClient("fake", nil)
	t.Logf("解码失败 error = %v", err)
	if !errors.Is(err, errDecode) {
		t.Fatalf("期望能追到 errDecode，实际 %v", err)
	}
	if !strings.Contains(err.Error(), "fake") {
		t.Errorf("错误消息缺少后端名: %v", err)
	}
}

// TestNewServiceUsesBucketer 校验工厂从客户端取桶名构造 Service。
func TestNewServiceUsesBucketer(t *testing.T) {
	factory, err := storage.NewFactory(jsonDecoder)
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	if err := storage.RegisterBackend(factory, "fake", newFactoryBucketClient); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	svc, err := factory.NewService("fake", map[string]any{"bucket": "demo"})
	if err != nil {
		t.Fatalf("NewService() error = %v", err)
	}
	t.Logf("NewService().Bucket() = %q", svc.Bucket())
	if svc.Bucket() != "demo" {
		t.Fatalf("Bucket() = %q，期望 demo", svc.Bucket())
	}
}

// TestNewServiceRequiresBucketer 校验未实现 Bucketer 的客户端不能构造 Service。
func TestNewServiceRequiresBucketer(t *testing.T) {
	factory, err := storage.NewFactory(jsonDecoder)
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	if err := storage.RegisterBackend(factory, "fake", newFactoryClient); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	_, err = factory.NewService("fake", map[string]any{"bucket": "demo"})
	t.Logf("客户端未实现 Bucketer 的 error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}

// TestNewServiceRequiresBucket 校验桶名为空时不能构造 Service。
func TestNewServiceRequiresBucket(t *testing.T) {
	factory, err := storage.NewFactory(jsonDecoder)
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	if err := storage.RegisterBackend(factory, "fake", newFactoryBucketClient); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	_, err = factory.NewService("fake", map[string]any{})
	t.Logf("空桶的 error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}

// TestFactoryConcurrentNewClient 校验并发构造在 -race 下稳定。
func TestFactoryConcurrentNewClient(t *testing.T) {
	factory, err := storage.NewFactory(jsonDecoder)
	if err != nil {
		t.Fatalf("NewFactory() error = %v", err)
	}
	if err := storage.RegisterBackend(factory, "fake", newFactoryClient); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	const workers = 32
	var wg sync.WaitGroup
	errs := make(chan error, workers)
	for i := range workers {
		wg.Go(func() {
			endpoint := fmt.Sprintf("e%d", i)
			client, err := factory.NewClient("fake", map[string]any{"endpoint": endpoint})
			if err != nil {
				errs <- err
				return
			}
			if got := client.(*factoryClient).endpoint; got != endpoint {
				errs <- fmt.Errorf("endpoint = %q，期望 %q", got, endpoint)
			}
		})
	}
	wg.Wait()
	close(errs)

	failed := 0
	for err := range errs {
		failed++
		t.Error(err)
	}
	t.Logf("并发构造 %d 个客户端，失败 %d 个", workers, failed)
}
