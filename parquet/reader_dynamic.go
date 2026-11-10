package parquet

import "reflect"

// isDynamicType 判断 T 是否是动态读取模式（map[string]any）。
//
// 动态模式的实现都在这个文件里：reader.go 只保留构造期一行分叉，
// 结构体读取的代码与热路径不受影响。
func isDynamicType(t reflect.Type) bool { return t.Kind() == reflect.Map }

// dynamicReadSchema 返回动态读取使用的目标 schema。
//
// 未指定 Schema 时直接使用文件 schema（上游的 Schema 实现了 ReaderOption，
// 可以原样传给 GenericReader）；指定时作为目标 schema，只允许相对文件增删列。
func dynamicReadSchema(file *Schema, opts ReadOptions) *Schema {
	if opts.Schema != nil {
		return opts.Schema
	}
	return file
}

// ensureDynamicMaps 在动态模式下为 nil 的 map 预分配。
//
// 上游 Schema.Reconstruct 对 nil map 直接 SetMapIndex 会 panic（只有 len>0
// 时才重建 map），所以这里先补上空 map；结构体模式直接返回，不做任何反射。
func ensureDynamicMaps[T any](rows []T) {
	t := reflect.TypeFor[T]()
	if t == nil || t.Kind() != reflect.Map {
		return
	}
	for i := range rows {
		v := reflect.ValueOf(&rows[i]).Elem()
		if v.IsNil() {
			v.Set(reflect.MakeMap(t))
		}
	}
}
