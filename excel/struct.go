package excel

import (
	"encoding"
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// 结构体反射用到的类型对象。
var (
	timeType            = reflect.TypeFor[time.Time]()
	textMarshalerType   = reflect.TypeFor[encoding.TextMarshaler]()
	textUnmarshalerType = reflect.TypeFor[encoding.TextUnmarshaler]()
)

// fieldSpec 是结构体字段解析后的列声明。
type fieldSpec struct {
	index    []int        // 从根结构体到叶子字段的下标路径
	header   string       // Excel 表头名
	typ      reflect.Type // 叶子字段类型；指针字段取元素类型
	layout   string       // time.Time 的布局
	required bool         // 空值是否报错
	width    float64      // 写出列宽
	pointer  bool         // 叶子字段是否为指针
}

// ReadInto 把当前工作表读成结构体切片。
//
// 字段与 tag 决定列；不接受 opts.Columns。T 只出现在返回值，调用时必须
// 显式写 s.ReadInto[User](opts)。列/tag 无效返回 ErrInvalidColumn；Reader
// 已关闭返回 ErrClosed；行级错误以 *RowError 返回。配置 OnRowError 后改为
// 容错读取：坏行跳过并交给处理器，只返回成功行。
func (s *SheetReader) ReadInto[T any](opts ReadOptions) ([]T, error) {
	var out []T
	err := eachIntoSheet[T](s, opts, func(_ int, value T) error {
		out = append(out, value)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// EachInto 逐行读取并转换到结构体，不把全部结果保留在内存。
//
// 字段与 tag 决定列；不接受 opts.Columns。rowNum 是 Excel 物理行号
// （1-based，含表头与跳过行）。回调返回 ErrStop 表示成功提前结束，其他
// 错误会包装成 *RowError；结构体转换/Validate 错误也以 *RowError 返回。
// Reader 已关闭返回 ErrClosed。fn 不能为 nil。配置 OnRowError 后，行级错误
// 会跳过该行并继续，详见 ReadOptions.OnRowError。
func (s *SheetReader) EachInto[T any](opts ReadOptions, fn func(rowNum int, value T) error) error {
	return eachIntoSheet[T](s, opts, fn)
}

// eachIntoSheet 是 ReadInto/EachInto 的公共实现。
func eachIntoSheet[T any](s *SheetReader, opts ReadOptions, fn func(rowNum int, value T) error) error {
	if err := s.reader.check(); err != nil {
		return err
	}
	typ, fields, hasTime, err := prepareStruct[T](opts.Columns, true)
	if err != nil {
		return err
	}
	cols := make([]Column, len(fields))
	for i, f := range fields {
		// 结构体层用表头作为内部键，行级错误里显示的就是用户看到的那一列。
		// Required 由 decodeStructValue 汇总，避免 readRows 提前短路导致其他
		// 字段的错误无法进入同一份报告。
		cols[i] = Column{Header: f.header, Key: f.header}
	}
	opts.Columns = cols

	return s.read(opts, hasTime, false, func(rowNum int, row map[string]string, date1904 bool) error {
		value, err := decodeStructValue[T](typ, fields, row, rowNum, date1904)
		if err != nil {
			return err
		}
		if vErr := validateRow(opts, rowNum, row); vErr != nil {
			return vErr
		}
		if err := validateStructValue(&value); err != nil {
			return &RowError{Row: rowNum, Err: err}
		}
		return fn(rowNum, value)
	})
}

// decodeStructValue 把一行 map 转换成结构体，汇总所有字段错误；不执行 Validate。
func decodeStructValue[T any](typ reflect.Type, fields []fieldSpec, row map[string]string, rowNum int, date1904 bool) (T, error) {
	value := reflect.New(typ).Elem()
	problems := make([]rowProblem, 0, len(fields))
	for _, f := range fields {
		if err := setStructField(value, f, row[f.header], date1904); err != nil {
			problems = append(problems, rowProblem{column: f.header, err: err})
		}
	}
	if err := joinRowErrors(rowNum, problems); err != nil {
		var zero T
		return zero, err
	}
	return value.Interface().(T), nil
}

// validateStructValue 调用结构体可选的 Validate 方法。
func validateStructValue[T any](value *T) error {
	if validator, ok := any(value).(interface{ Validate() error }); ok {
		return validator.Validate()
	}
	return nil
}

// StructSheetWriter 是绑定到具体结构体类型的 sheet 流式写出句柄。
//
// 由 Builder.AddSheetFrom 创建。Write 按字段 tag 格式化并追加一行；Close
// 只 Flush 当前 sheet，幂等。Builder.Close 会自动关闭它。关闭后 Write 返回
// ErrClosed；Builder 已失败且 sheet 未关闭时返回第一次失败的错误。
type StructSheetWriter[T any] struct {
	sheet  *SheetWriter
	fields []fieldSpec
}

// AddSheetFrom 添加一个由结构体类型 T 决定列的工作表，并返回流式写出句柄。
//
// 列顺序、表头与宽度来自 T 的字段 tag；不接受 opts.Columns（非空返回
// ErrInvalidColumn）。name 规则与 AddSheet 相同：非法名 ErrInvalidSheetName，
// 重名 ErrSheetExists。T 的字段无效时返回 ErrInvalidColumn，且不污染 Builder。
// 已有活动 sheet 时会先关闭它。
func (b *Builder) AddSheetFrom[T any](name string, opts WriteOptions) (*StructSheetWriter[T], error) {
	if err := b.check(); err != nil {
		return nil, err
	}
	_, fields, _, err := prepareStruct[T](opts.Columns, false)
	if err != nil {
		return nil, err
	}
	cols := make([]Column, len(fields))
	for i, f := range fields {
		cols[i] = Column{Header: f.header, Key: f.header, Width: f.width}
	}
	opts.Columns = cols

	sw, err := b.AddSheet(name, opts)
	if err != nil {
		return nil, err
	}
	return &StructSheetWriter[T]{sheet: sw, fields: fields}, nil
}

// Write 按字段 tag 格式化并追加一行。
//
// 单元格超长或底层 SetRow 失败返回 *RowError，并让 Builder 进入失败态。
// 关闭后返回 ErrClosed；Builder 已失败且 sheet 未关闭时返回原错误。
func (w *StructSheetWriter[T]) Write(value T) error {
	if w.sheet.closed {
		return ErrClosed
	}
	if err := w.sheet.builder.check(); err != nil {
		return err
	}

	row := make(map[string]string, len(w.fields))
	// 取地址让指针接收者的 TextMarshaler 也能被调用。
	rv := reflect.ValueOf(&value).Elem()
	for _, f := range w.fields {
		text, err := formatStructField(rv, f)
		if err != nil {
			re := &RowError{Row: w.sheet.rowNum, Column: f.header, Err: err}
			w.sheet.builder.fail(re)
			return re
		}
		row[f.header] = text
	}
	return w.sheet.Write(row)
}

// Close 只 Flush 当前 sheet，幂等；语义同 SheetWriter.Close。
func (w *StructSheetWriter[T]) Close() error {
	return w.sheet.Close()
}

// WriteFrom 把结构体切片写入一个 sheet，成功后关闭该 sheet。
//
// 等价于 AddSheetFrom + 逐行 Write + Close；字段声明顺序就是列顺序，
// 表头与选项来自 tag。time.Time 按 layout 格式化，指针为 nil 时写空单元格。
// 不接受 opts.Columns（非空返回 ErrInvalidColumn）。错误同 AddSheetFrom/
// StructSheetWriter.Write；失败后 Builder 进入失败态。
func (b *Builder) WriteFrom[T any](name string, rows []T, opts WriteOptions) error {
	w, err := b.AddSheetFrom[T](name, opts)
	if err != nil {
		return err
	}
	for i := range rows {
		if err := w.Write(rows[i]); err != nil {
			return err
		}
	}
	return w.Close()
}

// prepareStruct 校验目标类型与 tag，并返回结构体类型与字段声明。
//
// forRead 为 true 时按读取方向校验字段类型，false 时按写出方向校验。
func prepareStruct[T any](cols []Column, forRead bool) (reflect.Type, []fieldSpec, bool, error) {
	if len(cols) > 0 {
		return nil, nil, false, fmt.Errorf("%w: 结构体层不接受 Columns", ErrInvalidColumn)
	}
	typ := reflect.TypeFor[T]()
	if typ.Kind() != reflect.Struct {
		return nil, nil, false, fmt.Errorf("%w: 目标类型必须是结构体，实际是 %s", ErrInvalidColumn, typ)
	}
	fields, err := structFields(typ, forRead)
	if err != nil {
		return nil, nil, false, err
	}
	hasTime := false
	for _, f := range fields {
		if f.typ == timeType {
			hasTime = true
			break
		}
	}
	return typ, fields, hasTime, nil
}

// structFields 递归解析结构体的全部导出叶子字段。
func structFields(typ reflect.Type, forRead bool) ([]fieldSpec, error) {
	fields := make([]fieldSpec, 0, typ.NumField())
	headers := make(map[string]struct{}, typ.NumField())
	if err := collectFields(typ, nil, "", forRead, &fields, headers); err != nil {
		return nil, err
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: 结构体没有可导出的字段", ErrInvalidColumn)
	}
	return fields, nil
}

// collectFields 遍历一层结构体，并把嵌套结构体展平成叶子字段。
//
// prefix 是父级 tag 提供的列前缀；非空时最终表头为 prefix + "." + 叶子表头。
func collectFields(typ reflect.Type, path []int, prefix string, forRead bool, fields *[]fieldSpec, headers map[string]struct{}) error {
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" {
			continue // 未导出字段忽略
		}
		tag := f.Tag.Get("excel")
		name, opts, _ := strings.Cut(tag, ",")
		name = strings.TrimSpace(name)
		if name == "-" {
			continue
		}

		fieldPath := append(append([]int(nil), path...), i)
		fieldType, err := derefFieldType(f.Type)
		if err != nil {
			return fmt.Errorf("%w: 字段 %s %s", ErrInvalidColumn, f.Name, err)
		}
		if isNestedStruct(fieldType) {
			if strings.TrimSpace(opts) != "" {
				return fmt.Errorf("%w: 嵌套结构体字段 %s 不支持 tag 选项 %q", ErrInvalidColumn, f.Name, strings.TrimSpace(opts))
			}
			childPrefix := prefix
			if name != "" {
				if childPrefix != "" {
					childPrefix += "." + name
				} else {
					childPrefix = name
				}
			}
			before := len(*fields)
			if err := collectFields(fieldType, fieldPath, childPrefix, forRead, fields, headers); err != nil {
				return err
			}
			if len(*fields) == before {
				return fmt.Errorf("%w: 嵌套结构体字段 %s 没有可导出的叶子字段", ErrInvalidColumn, f.Name)
			}
			continue
		}

		spec, err := parseFieldTag(f, fieldPath, tag, forRead)
		if err != nil {
			return err
		}
		if prefix != "" {
			spec.header = prefix + "." + spec.header
		}
		if _, ok := headers[spec.header]; ok {
			return fmt.Errorf("%w: 字段 %s 的表头 %q 重复", ErrInvalidColumn, f.Name, spec.header)
		}
		headers[spec.header] = struct{}{}
		*fields = append(*fields, spec)
	}
	return nil
}

// derefFieldType 去掉一层指针；多层指针返回错误。
func derefFieldType(t reflect.Type) (reflect.Type, error) {
	if t.Kind() == reflect.Pointer {
		if t.Elem().Kind() == reflect.Pointer {
			return nil, errors.New("不支持多层指针")
		}
		t = t.Elem()
	}
	return t, nil
}

// isNestedStruct 判断字段是否应作为嵌套容器展平。
//
// time.Time 与实现文本接口的自定义类型保持为叶子列。
func isNestedStruct(t reflect.Type) bool {
	if t == timeType || t.Kind() != reflect.Struct {
		return false
	}
	return !implementsTextMarshaler(t) && !implementsTextUnmarshaler(t)
}

// parseFieldTag 解析叶子字段的 excel tag 与类型。
func parseFieldTag(f reflect.StructField, index []int, tag string, forRead bool) (fieldSpec, error) {
	spec := fieldSpec{index: index, header: f.Name, typ: f.Type}
	parts := strings.Split(tag, ",")
	if header := strings.TrimSpace(parts[0]); header != "" {
		spec.header = header
	}
	for _, raw := range parts[1:] {
		opt := strings.TrimSpace(raw)
		switch {
		case opt == "":
		case opt == "required":
			spec.required = true
		case strings.HasPrefix(opt, "layout:"):
			spec.layout = strings.TrimSpace(strings.TrimPrefix(opt, "layout:"))
			if spec.layout == "" {
				return spec, fmt.Errorf("%w: 字段 %s 的 layout 为空", ErrInvalidColumn, f.Name)
			}
		case strings.HasPrefix(opt, "width:"):
			width, err := strconv.ParseFloat(strings.TrimSpace(strings.TrimPrefix(opt, "width:")), 64)
			if err != nil || width < 0 || width > excelize.MaxColumnWidth {
				return spec, fmt.Errorf("%w: 字段 %s 的 width 无效", ErrInvalidColumn, f.Name)
			}
			spec.width = width
		default:
			return spec, fmt.Errorf("%w: 字段 %s 的 tag 选项 %q 未知", ErrInvalidColumn, f.Name, opt)
		}
	}

	if f.Type.Kind() == reflect.Pointer {
		if f.Type.Elem().Kind() == reflect.Pointer {
			return spec, fmt.Errorf("%w: 字段 %s 不支持多层指针", ErrInvalidColumn, f.Name)
		}
		spec.pointer = true
		spec.typ = f.Type.Elem()
	}
	if !supportedFieldType(spec.typ, forRead) {
		return spec, fmt.Errorf("%w: 字段 %s 的类型 %s 不支持", ErrInvalidColumn, f.Name, f.Type)
	}
	if spec.typ == timeType && spec.layout == "" {
		return spec, fmt.Errorf("%w: time.Time 字段 %s 必须提供 layout", ErrInvalidColumn, f.Name)
	}
	return spec, nil
}

// supportedFieldType 判断字段类型是否支持当前读/写方向。
//
// 标量类型直接支持；非标量类型必须实现对应方向的 encoding.Text 接口。
func supportedFieldType(t reflect.Type, forRead bool) bool {
	if t == timeType {
		return true
	}
	if forRead {
		if implementsTextUnmarshaler(t) {
			return true
		}
	} else if implementsTextMarshaler(t) {
		return true
	}
	switch t.Kind() {
	case reflect.String, reflect.Bool,
		reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64,
		reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64,
		reflect.Float32, reflect.Float64:
		return true
	default:
		return false
	}
}

// implementsTextMarshaler 判断类型或其指针是否实现 encoding.TextMarshaler。
func implementsTextMarshaler(t reflect.Type) bool {
	return t.Implements(textMarshalerType) || reflect.PointerTo(t).Implements(textMarshalerType)
}

// implementsTextUnmarshaler 判断类型或其指针是否实现 encoding.TextUnmarshaler。
func implementsTextUnmarshaler(t reflect.Type) bool {
	return t.Implements(textUnmarshalerType) || reflect.PointerTo(t).Implements(textUnmarshalerType)
}

// setStructField 把一列字符串写入根结构体下的叶子字段。
func setStructField(root reflect.Value, f fieldSpec, raw string, date1904 bool) error {
	if raw == "" {
		if f.required {
			return errors.New("必填字段为空")
		}
		return nil
	}
	dst, err := fieldByIndexAlloc(root, f.index)
	if err != nil {
		return err
	}
	holder := dst
	if f.pointer {
		holder = reflect.New(f.typ)
	}
	if err := parseFieldValue(holder, f, raw, date1904); err != nil {
		return err
	}
	if f.pointer {
		dst.Set(holder)
	}
	return nil
}

// parseFieldValue 把字符串解析到叶子字段。holder 可能是指针。
func parseFieldValue(holder reflect.Value, f fieldSpec, raw string, date1904 bool) error {
	value := holder
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	text := strings.TrimSpace(raw)

	if f.typ == timeType {
		t, err := parseTimeValue(text, f.layout, date1904)
		if err != nil {
			return fmt.Errorf("值 %q 无法解析为时间: %w", truncate(raw, 64), err)
		}
		value.Set(reflect.ValueOf(t))
		return nil
	}
	if value.CanAddr() {
		if unmarshaler, ok := value.Addr().Interface().(encoding.TextUnmarshaler); ok {
			if err := unmarshaler.UnmarshalText([]byte(text)); err != nil {
				return fmt.Errorf("值 %q 无法解析为 %s: %w", truncate(raw, 64), f.typ, err)
			}
			return nil
		}
	}

	switch f.typ.Kind() {
	case reflect.String:
		value.SetString(raw)
	case reflect.Bool:
		switch strings.ToUpper(text) {
		case "TRUE", "1":
			value.SetBool(true)
		case "FALSE", "0":
			value.SetBool(false)
		default:
			return fmt.Errorf("值 %q 无法解析为 bool", truncate(raw, 64))
		}
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		n, err := strconv.ParseInt(text, 10, f.typ.Bits())
		if err != nil {
			return fmt.Errorf("值 %q 无法解析为 %s", truncate(raw, 64), f.typ)
		}
		value.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		n, err := strconv.ParseUint(text, 10, f.typ.Bits())
		if err != nil {
			return fmt.Errorf("值 %q 无法解析为 %s", truncate(raw, 64), f.typ)
		}
		value.SetUint(n)
	case reflect.Float32, reflect.Float64:
		n, err := strconv.ParseFloat(text, f.typ.Bits())
		if err != nil {
			return fmt.Errorf("值 %q 无法解析为 %s", truncate(raw, 64), f.typ)
		}
		value.SetFloat(n)
	}
	return nil
}

// parseTimeValue 先按 layout 解析，失败再按 Excel 日期序列号解析。
// 先 layout 是为了避免纯数字文本日期（如 "20240130"）被误当成序列号。
func parseTimeValue(text, layout string, date1904 bool) (time.Time, error) {
	if t, err := time.Parse(layout, text); err == nil {
		return t, nil
	}
	serial, err := strconv.ParseFloat(text, 64)
	if err != nil {
		return time.Time{}, fmt.Errorf("既不符合 layout %q，也不是 Excel 日期序列号", layout)
	}
	return excelize.ExcelDateToTime(serial, date1904)
}

// formatStructField 从根结构体按路径取出叶子字段并格式化。
//
// 嵌套指针为 nil 时返回空字符串，表示整组子列都写空单元格。
func formatStructField(root reflect.Value, f fieldSpec) (string, error) {
	value, ok := fieldByIndex(root, f.index)
	if !ok {
		return "", nil
	}
	return formatField(value, f)
}

// formatField 把一个叶子字段值写成字符串。
func formatField(value reflect.Value, f fieldSpec) (string, error) {
	if f.pointer {
		if value.IsNil() {
			return "", nil
		}
		value = value.Elem()
	}
	if f.typ == timeType {
		return value.Interface().(time.Time).Format(f.layout), nil
	}
	if marshaler, ok := textMarshalerValue(value); ok {
		text, err := marshaler.MarshalText()
		if err != nil {
			return "", fmt.Errorf("值无法序列化为文本: %w", err)
		}
		return string(text), nil
	}
	switch f.typ.Kind() {
	case reflect.String:
		return value.String(), nil
	case reflect.Bool:
		return strconv.FormatBool(value.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(value.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(value.Uint(), 10), nil
	case reflect.Float32:
		return strconv.FormatFloat(value.Float(), 'f', -1, 32), nil
	case reflect.Float64:
		return strconv.FormatFloat(value.Float(), 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("不支持的字段类型 %s", f.typ)
	}
}

// textMarshalerValue 取出字段值上的 TextMarshaler，兼容值/指针接收者。
func textMarshalerValue(value reflect.Value) (encoding.TextMarshaler, bool) {
	if value.IsValid() && value.CanInterface() {
		if marshaler, ok := value.Interface().(encoding.TextMarshaler); ok {
			return marshaler, true
		}
	}
	if value.IsValid() && value.CanAddr() {
		if marshaler, ok := value.Addr().Interface().(encoding.TextMarshaler); ok {
			return marshaler, true
		}
	}
	if value.IsValid() && value.Kind() != reflect.Pointer {
		copyValue := reflect.New(value.Type()).Elem()
		copyValue.Set(value)
		if marshaler, ok := copyValue.Addr().Interface().(encoding.TextMarshaler); ok {
			return marshaler, true
		}
	}
	return nil, false
}

// fieldByIndex 按路径取字段；嵌套指针为 nil 时返回 false。
func fieldByIndex(root reflect.Value, path []int) (reflect.Value, bool) {
	value := root
	for _, index := range path {
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				return reflect.Value{}, false
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct || index < 0 || index >= value.NumField() {
			return reflect.Value{}, false
		}
		value = value.Field(index)
	}
	return value, true
}

// fieldByIndexAlloc 按路径取字段，并在遇到 nil 中间指针时分配。
func fieldByIndexAlloc(root reflect.Value, path []int) (reflect.Value, error) {
	value := root
	for _, index := range path {
		if value.Kind() == reflect.Pointer {
			if value.IsNil() {
				if value.Type().Elem().Kind() != reflect.Struct {
					return reflect.Value{}, errors.New("嵌套字段路径不是结构体")
				}
				value.Set(reflect.New(value.Type().Elem()))
			}
			value = value.Elem()
		}
		if value.Kind() != reflect.Struct || index < 0 || index >= value.NumField() {
			return reflect.Value{}, errors.New("嵌套字段路径无效")
		}
		value = value.Field(index)
	}
	return value, nil
}
