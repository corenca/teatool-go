package storage

import (
	"fmt"
	"reflect"
	"sort"
	"strings"
)

// ready 校验工厂已由 NewFactory 初始化；nil 受体返回错误而不是 panic。
func (f *Factory) ready() error {
	if f == nil || f.decode == nil || f.builds == nil {
		return fmt.Errorf("%w: factory 必须由 NewFactory 构造", ErrInvalidArgument)
	}
	return nil
}

// lookup 取某个后端的构造闭包。
func (f *Factory) lookup(name string) (func(map[string]any) (Client, error), bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	build, ok := f.builds[name]
	return build, ok
}

// names 返回排序后的已注册后端名，用于未注册后端的错误消息。
func (f *Factory) names() string {
	f.mu.RLock()
	defer f.mu.RUnlock()
	names := make([]string, 0, len(f.builds))
	for name := range f.builds {
		names = append(names, name)
	}
	sort.Strings(names)
	return strings.Join(names, ", ")
}

// newConfigBuilder 把"配置类型 C + 构造函数"擦除成"接收 map 的构造闭包"。
// 解码在调用时进行，不持有工厂锁。
func newConfigBuilder[C any, T Client](decode Decoder, name string, newClient func(C) (T, error)) (func(map[string]any) (Client, error), error) {
	configType := reflect.TypeFor[C]()
	if configType.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: 后端 %q 的配置类型必须是结构体值类型，实际为 %s",
			ErrInvalidArgument, name, configType.Kind())
	}

	return func(raw map[string]any) (Client, error) {
		config := reflect.New(configType)
		if err := decode(raw, config.Interface()); err != nil {
			return nil, fmt.Errorf("storage: 解码后端 %q 的配置: %w", name, err)
		}
		return newClient(config.Elem().Interface().(C))
	}, nil
}
