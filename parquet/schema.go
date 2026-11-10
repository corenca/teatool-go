package parquet

import (
	"fmt"
	"reflect"
	"sort"
	"strings"

	pq "github.com/parquet-go/parquet-go"
	"github.com/parquet-go/parquet-go/format"
)

// guard 把 op 执行期间发生的 panic 转成 error 写入 errp。
//
// 上游把"调用方写错了"当 panic 抛（打开失败、schema 转换失败、值类型不对），
// 本包的公开入口统一用它兜住，保证调用方只处理 error。errp 已有错误时不覆盖。
func guard(op string, errp *error) {
	r := recover()
	if r == nil {
		return
	}
	if *errp != nil {
		return
	}
	*errp = fmt.Errorf("parquet: %s 失败: %v", op, r)
}

// typeOf 返回 T 的反射类型并做形态校验。
//
// 支持结构体、结构体指针；其他形态返回 ErrInvalidSchema。
func typeOf[T any]() (reflect.Type, error) {
	t := reflect.TypeFor[T]()
	if t == nil {
		return nil, fmt.Errorf("%w: T 不能是 any", ErrInvalidSchema)
	}
	if t.Kind() == reflect.Pointer {
		t = t.Elem()
	}
	switch t.Kind() {
	case reflect.Struct:
		return t, nil
	case reflect.Map:
		if t.Key().Kind() == reflect.String && t.Elem().Kind() == reflect.Interface {
			return t, nil
		}
		return nil, fmt.Errorf("%w: 动态模式只支持 map[string]any，得到 %v", ErrInvalidSchema, t)
	default:
		return nil, fmt.Errorf("%w: 期望结构体、*结构体或 map[string]any，得到 %v", ErrInvalidSchema, t)
	}
}

// modelSchema 由结构体类型推导 schema。
//
// 上游的 SchemaOf 对非法 tag 会 panic，这里转成 ErrInvalidSchema。
func modelSchema(t reflect.Type) (schema *Schema, err error) {
	defer guard("推导结构体 schema", &err)
	schema = pq.SchemaOf(reflect.New(t).Interface())
	if len(schema.Columns()) == 0 {
		return nil, fmt.Errorf("%w: 结构体 %v 没有可读写的列", ErrInvalidSchema, t)
	}
	return schema, nil
}

// splitColumnPath 把点号分隔的列路径拆成上游 Lookup 需要的片段。
func splitColumnPath(path string) []string { return strings.Split(path, ".") }

// joinColumnPath 把上游的列路径片段拼成点号分隔的文本。
func joinColumnPath(path []string) string { return strings.Join(path, ".") }

// columnPaths 返回 schema 的全部叶子列路径（点号分隔）。
func columnPaths(schema *Schema) []string {
	paths := schema.Columns()
	out := make([]string, len(paths))
	for i, p := range paths {
		out[i] = joinColumnPath(p)
	}
	return out
}

// checkProjection 校验投影里的每条列路径都能在文件 schema 中查到。
//
// 既接受叶子列路径（如 "addr.city"），也接受顶层字段名（如 "tags"，对应
// LIST/MAP 这类逻辑类型的整列）。
func checkProjection(opts ReadOptions, file *Schema) error {
	if len(opts.Columns) == 0 {
		return nil
	}
	var unknown []string
	for _, path := range opts.Columns {
		parts := splitColumnPath(path)
		if path == "" {
			unknown = append(unknown, path)
			continue
		}
		if _, ok := file.Lookup(parts...); ok {
			continue
		}
		if len(parts) == 1 && fieldByName(file.Fields(), parts[0]) != nil {
			continue
		}
		unknown = append(unknown, path)
	}
	if len(unknown) == 0 {
		return nil
	}
	sort.Strings(unknown)
	return fmt.Errorf("%w: %s", ErrUnknownColumn, joinColumns(unknown))
}

// checkMissingColumns 比较目标 schema 与文件 schema 的叶子列路径。
//
// 目标有、文件没有的列收集成 *MissingColumnsError。selected 是投影命中的目标
// 列（nil 表示全选）：投影是"明确不要"，未被投影的列不参与缺列判断。
func checkMissingColumns(target, file *Schema, allowMissing bool, selected map[string]struct{}) error {
	if allowMissing {
		return nil
	}
	have := make(map[string]struct{}, len(file.Columns()))
	for _, p := range file.Columns() {
		have[joinColumnPath(p)] = struct{}{}
	}
	var missing []string
	for _, path := range columnPaths(target) {
		if selected != nil {
			if _, ok := selected[path]; !ok {
				continue
			}
		}
		if _, ok := have[path]; !ok {
			missing = append(missing, path)
		}
	}
	if len(missing) == 0 {
		return nil
	}
	sort.Strings(missing)
	return &MissingColumnsError{Columns: missing}
}

// selectedPaths 返回投影命中的目标叶子列路径集合。
//
// columns 为空时返回 nil，表示全选。投影支持叶子路径与顶层字段名，后者会展开成
// 该字段下的全部叶子（LIST/MAP 列因此可以整列读取）。
func selectedPaths(target *Schema, columns []string) map[string]struct{} {
	if len(columns) == 0 {
		return nil
	}
	selected := make(map[string]struct{}, len(columns))
	for _, column := range columns {
		for _, p := range target.Columns() {
			path := joinColumnPath(p)
			if path == column || strings.HasPrefix(path, column+".") {
				selected[path] = struct{}{}
			}
		}
	}
	return selected
}

// projectSchema 从文件 schema 中裁出只含指定列路径的 schema，供 Reader 使用。
//
// 整列投影直接复用文件 schema 的节点（保留 LIST/MAP 等逻辑类型）；嵌套结构体的
// 部分投影会重建 group，并保留原有的 optional/repeated 修饰。LIST/MAP 内部不做
// 部分投影——这类列要么整列读取，要么不读。
func projectSchema(file *Schema, columns []string) (schema *Schema, err error) {
	if len(columns) == 0 {
		return file, nil
	}
	defer guard("裁剪投影 schema", &err)

	paths := make([][]string, 0, len(columns))
	for _, c := range columns {
		paths = append(paths, splitColumnPath(c))
	}
	return pq.NewSchema(file.Name(), projectFields(file.Fields(), paths)), nil
}

// projectBucket 是投影路径按首段列名分组的结果。
//
// all 为 true 表示整列都要（含该列的嵌套路径）；rest 是子路径。
type projectBucket struct {
	name string
	all  bool
	rest [][]string
}

// projectFields 按 paths 裁剪 fields，返回新的 group 节点。
func projectFields(fields []pq.Field, paths [][]string) pq.Node {
	buckets := make([]*projectBucket, 0, len(paths))
	index := make(map[string]*projectBucket, len(paths))
	for _, p := range paths {
		b := index[p[0]]
		if b == nil {
			b = &projectBucket{name: p[0]}
			index[p[0]] = b
			buckets = append(buckets, b)
		}
		if len(p) == 1 {
			b.all = true
		} else {
			b.rest = append(b.rest, p[1:])
		}
	}

	group := make(pq.Group, len(buckets))
	for _, b := range buckets {
		field := fieldByName(fields, b.name)
		if field == nil {
			continue
		}
		// 整列、叶子列，或带逻辑类型的 group（LIST/MAP/VARIANT）都整列复用。
		if b.all || field.Leaf() || field.Type().LogicalType() != nil {
			group[b.name] = field
			continue
		}
		group[b.name] = wrapRepetition(field, projectFields(field.Fields(), b.rest))
	}
	return group
}

// fieldByName 在 fields 中按列名查找字段。
func fieldByName(fields []pq.Field, name string) pq.Field {
	for _, f := range fields {
		if f.Name() == name {
			return f
		}
	}
	return nil
}

// wrapRepetition 给重建的 group 节点补回原有的 optional/repeated 修饰。
func wrapRepetition(src pq.Field, node pq.Node) pq.Node {
	switch {
	case src.Optional():
		return pq.Optional(node)
	case src.Repeated():
		return pq.Repeated(node)
	default:
		return node
	}
}

// checkCompatible 逐叶子路径比对目标 schema 与文件 schema 的类型。
//
// 只比对两边都有的路径；缺失列由 checkMissingColumns 处理。不兼容的列汇总成
// *IncompatibleColumnsError，时间戳/日期会附上建议的 tag。
func checkCompatible(target, file *Schema, selected map[string]struct{}) error {
	fileLeaves := make(map[string]pq.LeafColumn, len(file.Columns()))
	for _, p := range file.Columns() {
		if leaf, ok := file.Lookup(p...); ok {
			fileLeaves[joinColumnPath(p)] = leaf
		}
	}

	var mismatches []ColumnMismatch
	for _, p := range target.Columns() {
		key := joinColumnPath(p)
		if selected != nil {
			if _, ok := selected[key]; !ok {
				continue
			}
		}
		src, ok := fileLeaves[key]
		if !ok {
			continue
		}
		leaf, ok := target.Lookup(p...)
		if !ok {
			continue
		}
		targetType, fileType := leaf.Node.Type(), src.Node.Type()
		if compatibleLeaf(targetType, fileType) {
			continue
		}
		mismatches = append(mismatches, ColumnMismatch{
			Path:   key,
			Target: targetType.String(),
			File:   fileType.String(),
			Hint:   tagHint(fileType),
		})
	}
	if len(mismatches) == 0 {
		return nil
	}
	sort.Slice(mismatches, func(i, j int) bool { return mismatches[i].Path < mismatches[j].Path })
	return &IncompatibleColumnsError{Columns: mismatches}
}

// compatibleLeaf 判断两列类型可以互读。
//
// 规则：物理 Kind 必须相同；FIXED_LEN_BYTE_ARRAY 还要求长度相同；逻辑类型要求
// 完全相等，只放行 BYTE_ARRAY 上"文本类逻辑类型"与"无逻辑类型"之间的组合
// （Go 侧都是 string/[]byte，语义由调用方负责）。
func compatibleLeaf(target, file pq.Type) bool {
	if target.Kind() != file.Kind() {
		return false
	}
	if target.Kind() == pq.FixedLenByteArray && target.Length() != file.Length() {
		return false
	}
	targetLogical, fileLogical := target.LogicalType(), file.LogicalType()
	if targetLogical != nil && fileLogical != nil {
		return reflect.DeepEqual(targetLogical, fileLogical)
	}
	if target.Kind() != pq.ByteArray {
		return targetLogical == nil && fileLogical == nil
	}
	// BYTE_ARRAY：一边无逻辑类型时，另一边是文本类即可。
	return targetLogical == nil || fileLogical == nil
}

// tagHint 为时间戳/日期给出建议的 tag 片段，其他类型返回空串。
func tagHint(file pq.Type) string {
	lt := file.LogicalType()
	if lt == nil {
		return ""
	}
	if _, ok := lt.Value.(*format.DateType); ok {
		return "date"
	}
	ts, ok := lt.Value.(*format.TimestampType)
	if !ok {
		return ""
	}
	unit, ok := timeUnitName(ts.Unit)
	if !ok {
		return ""
	}
	hint := "timestamp(" + unit
	if !ts.IsAdjustedToUTC {
		hint += ":local"
	}
	return hint + ")"
}

// timeUnitName 把 parquet 时间单位映射成 tag 里的写法。
func timeUnitName(u format.TimeUnit) (string, bool) {
	switch u.Value.(type) {
	case *format.MilliSeconds:
		return "millisecond", true
	case *format.MicroSeconds:
		return "microsecond", true
	case *format.NanoSeconds:
		return "nanosecond", true
	default:
		return "", false
	}
}
