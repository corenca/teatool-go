package storage

import (
	"errors"
	"fmt"
	"sync"
)

// Decoder 把某个后端的配置子树解码进 target。target 始终是该后端配置结构体
// 的指针（*C），由工厂通过反射创建。
//
// Decoder 由调用方提供，storage 不依赖任何配置库。viper 用户可以在 Decoder 里
// 对子树调用 Unmarshal（v.Sub("storage.<name>").Unmarshal(target)，Sub 可能返回
// nil，需先判空），也可以用 mapstructure.Decode 处理已经取出的 map。Decoder
// 必须可并发调用：工厂在锁外解码，多个 goroutine 可能同时进入。
type Decoder func(raw map[string]any, target any) error

// Bucketer 是可选接口：驱动实现后，工厂据此取出配置里的桶名构造 Service。
// 客户端自身的操作仍按调用传桶，桶名只用于 NewService。
type Bucketer interface {
	// Bucket 返回配置里的桶名，未配置时返回空串。
	Bucket() string
}

// ErrBackendNotFound 表示工厂未注册该名字的后端。
var ErrBackendNotFound = errors.New("storage: backend not found")

// Factory 持有解码器与已注册后端，按名字构造 Client 或 Service。
//
// 必须通过 NewFactory 构造，零值不可用：零值上的方法一律返回包装
// ErrInvalidArgument 的错误，不会 panic。注册表受读写锁保护，RegisterBackend
// 与 NewClient/NewService 可并发调用。
type Factory struct {
	decode Decoder
	mu     sync.RWMutex
	builds map[string]func(map[string]any) (Client, error)
}

// NewFactory 创建工厂。decode 为 nil 时返回包装 ErrInvalidArgument 的错误。
func NewFactory(decode Decoder) (*Factory, error) {
	if decode == nil {
		return nil, fmt.Errorf("%w: decoder 不能为空", ErrInvalidArgument)
	}
	return &Factory{
		decode: decode,
		builds: make(map[string]func(map[string]any) (Client, error)),
	}, nil
}

// RegisterBackend 把后端构造函数注册到工厂：name 是配置里的后端名，C 是该后端
// 的配置结构体，newClient 是驱动自己的构造函数（如 s3.New）。
//
// C 必须是结构体值类型（不是指针）。name 为空、重复注册、newClient 为 nil 或
// C 不是结构体时返回包装 ErrInvalidArgument 的错误。注册名同时是实例名，同一
// 驱动可以注册成多个名字（如 s3、s3Backup）。
//
// 注册点是泛型的：storage.RegisterBackend(f, "s3", s3.New) 由编译器推断 C 与 T，
// 并校验 T 实现了 Client；异构性在注册那一刻擦除。
func RegisterBackend[C any, T Client](f *Factory, name string, newClient func(C) (T, error)) error {
	if err := f.ready(); err != nil {
		return err
	}
	if name == "" {
		return fmt.Errorf("%w: 后端名不能为空", ErrInvalidArgument)
	}
	if newClient == nil {
		return fmt.Errorf("%w: 后端 %q 的构造函数不能为空", ErrInvalidArgument, name)
	}
	build, err := newConfigBuilder(f.decode, name, newClient)
	if err != nil {
		return err
	}

	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.builds[name]; ok {
		return fmt.Errorf("%w: 后端 %q 已注册", ErrInvalidArgument, name)
	}
	f.builds[name] = build
	return nil
}

// NewClient 按名字构造 Client。raw 是该后端的配置子树；为 nil 时按空 map
// 处理，由驱动自己报告缺失字段。
//
// 名字未注册时返回包装 ErrBackendNotFound 的错误（消息里带已注册名单）；
// 解码失败时返回包装了后端名与原错误的错误。
func (f *Factory) NewClient(name string, raw map[string]any) (Client, error) {
	if err := f.ready(); err != nil {
		return nil, err
	}
	build, ok := f.lookup(name)
	if !ok {
		return nil, fmt.Errorf("%w: %q（已注册：%s）", ErrBackendNotFound, name, f.names())
	}
	if raw == nil {
		raw = map[string]any{}
	}
	return build(raw)
}

// NewService 构造绑定桶的 Service：先按名字构造 Client，再通过可选的 Bucketer
// 接口取配置里的桶名。
//
// 客户端未实现 Bucketer 或桶名为空时返回包装 ErrInvalidArgument 的错误。
func (f *Factory) NewService(name string, raw map[string]any) (*Service, error) {
	client, err := f.NewClient(name, raw)
	if err != nil {
		return nil, err
	}
	bucketer, ok := client.(Bucketer)
	if !ok {
		return nil, fmt.Errorf("%w: 后端 %q 未提供桶名（未实现 Bucketer）", ErrInvalidArgument, name)
	}
	return NewService(client, bucketer.Bucket())
}
