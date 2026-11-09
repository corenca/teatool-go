package dsv

import (
	"encoding"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strconv"
	"strings"
	"time"
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
	header   string       // 表头文本
	typ      reflect.Type // 叶子字段类型；指针字段取元素类型
	layout   string       // time.Time 的布局
	required bool         // 空值是否报错
	pointer  bool         // 叶子字段是否为指针
}

// ReadInto 读取 r 并映射成结构体切片。
//
// 字段与 tag 决定列；不接受 opts.Columns（非空返回 ErrInvalidColumn）。
// 配置 OnRowError 后改为容错读取：坏行跳过并交给处理器，只返回成功行。
// 大文件请用 EachInto，避免把所有记录留在内存。
func ReadInto[T any](r io.Reader, opts ReadOptions) ([]T, error) {
	var out []T
	err := EachInto[T](r, opts, func(_ int, value T) error {
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
// 字段与 tag 决定列；不接受 opts.Columns（非空返回 ErrInvalidColumn）。
// line 是物理行号（1-based，含表头与跳过行）。fn 返回 ErrStop 表示成功提前
// 结束，其他错误会被包装成 *RowError。fn 不能为 nil。配置 OnRowError 后，
// 行级错误会跳过该行并继续，详见 ReadOptions.OnRowError。
func EachInto[T any](r io.Reader, opts ReadOptions, fn func(line int, value T) error) error {
	if err := opts.checkBase(); err != nil {
		return err
	}
	fields, err := prepareStruct[T](opts.Columns, true)
	if err != nil {
		return err
	}
	if fn == nil {
		return fmt.Errorf("%w: 回调为 nil", ErrInvalidOption)
	}
	cols := make([]Column, len(fields))
	for i, f := range fields {
		cols[i] = Column{Header: f.header, Key: f.header, Required: f.required}
	}
	return eachRecord(r, opts, cols, func(line int, values []string) error {
		value, err := decodeStruct[T](fields, values, line)
		if err != nil {
			return err
		}
		if opts.Validate != nil {
			row := make(map[string]string, len(fields))
			for i, f := range fields {
				row[f.header] = values[i]
			}
			if err := opts.Validate(line, row); err != nil {
				if errors.Is(err, ErrStop) {
					return ErrStop
				}
				if _, ok := errors.AsType[*RowError](err); ok {
					return err
				}
				return &RowError{Line: line, Err: err}
			}
		}
		if err := validateStruct(&value); err != nil {
			return &RowError{Line: line, Err: err}
		}
		return fn(line, value)
	})
}

// ReadFileInto 打开 path 并把记录映射成结构体切片，文件由本函数负责关闭。
//
// 语义与 ReadInto 相同；打开、读取与关闭错误会用 errors.Join 合并返回。
func ReadFileInto[T any](path string, opts ReadOptions) ([]T, error) {
	var out []T
	err := EachFileInto[T](path, opts, func(_ int, value T) error {
		out = append(out, value)
		return nil
	})
	if err != nil {
		return nil, err
	}
	return out, nil
}

// EachFileInto 打开 path 并逐行映射成结构体回调，文件由本函数负责关闭。
//
// 语义与 EachInto 相同；打开、读取与关闭错误会用 errors.Join 合并返回。
func EachFileInto[T any](path string, opts ReadOptions, fn func(line int, value T) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	return errors.Join(EachInto(f, opts, fn), f.Close())
}

// prepareStruct 校验目标类型与 tag，返回按字段顺序排列的列声明。
//
// forRead 为 true 时按读取方向要求 TextUnmarshaler，false 时要求 TextMarshaler。
func prepareStruct[T any](cols []Column, forRead bool) ([]fieldSpec, error) {
	if len(cols) > 0 {
		return nil, fmt.Errorf("%w: 结构体层不接受 Columns", ErrInvalidColumn)
	}
	typ := reflect.TypeFor[T]()
	if typ.Kind() != reflect.Struct {
		return nil, fmt.Errorf("%w: 目标类型必须是结构体，实际是 %s", ErrInvalidColumn, typ)
	}
	return structFields(typ, forRead)
}

// structFields 解析结构体的全部导出字段，生成列声明。
func structFields(typ reflect.Type, forRead bool) ([]fieldSpec, error) {
	fields := make([]fieldSpec, 0, typ.NumField())
	headers := make(map[string]struct{}, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" {
			continue // 未导出字段忽略
		}
		name, opts, _ := strings.Cut(f.Tag.Get("dsv"), ",")
		name = strings.TrimSpace(name)
		if name == "-" {
			continue
		}
		if name == "" {
			name = f.Name
		}
		spec := fieldSpec{index: []int{i}, header: name, typ: f.Type}
		for raw := range strings.SplitSeq(opts, ",") {
			opt := strings.TrimSpace(raw)
			switch {
			case opt == "":
			case opt == "required":
				spec.required = true
			case strings.HasPrefix(opt, "layout:"):
				spec.layout = strings.TrimSpace(strings.TrimPrefix(opt, "layout:"))
				if spec.layout == "" {
					return nil, fmt.Errorf("%w: 字段 %s 的 layout 为空", ErrInvalidColumn, f.Name)
				}
			default:
				return nil, fmt.Errorf("%w: 字段 %s 的 tag 选项 %q 未知", ErrInvalidColumn, f.Name, opt)
			}
		}

		if f.Type.Kind() == reflect.Pointer {
			if f.Type.Elem().Kind() == reflect.Pointer {
				return nil, fmt.Errorf("%w: 字段 %s 不支持多层指针", ErrInvalidColumn, f.Name)
			}
			spec.pointer = true
			spec.typ = f.Type.Elem()
		}
		if isNestedStruct(spec.typ) {
			return nil, fmt.Errorf("%w: 字段 %s 是嵌套结构体，文本文件不支持展平", ErrInvalidColumn, f.Name)
		}
		if !supportedFieldType(spec.typ, forRead) {
			return nil, fmt.Errorf("%w: 字段 %s 的类型 %s 不支持", ErrInvalidColumn, f.Name, f.Type)
		}
		if spec.typ == timeType && spec.layout == "" {
			return nil, fmt.Errorf("%w: time.Time 字段 %s 必须提供 layout", ErrInvalidColumn, f.Name)
		}
		if _, ok := headers[spec.header]; ok {
			return nil, fmt.Errorf("%w: 字段 %s 的表头 %q 重复", ErrDuplicateColumn, f.Name, spec.header)
		}
		headers[spec.header] = struct{}{}
		fields = append(fields, spec)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: 结构体没有可导出的字段", ErrInvalidColumn)
	}
	return fields, nil
}

// isNestedStruct 判断字段是否是不支持的嵌套容器。
//
// time.Time 与实现文本接口的自定义类型保持为叶子列。
func isNestedStruct(t reflect.Type) bool {
	if t == timeType || t.Kind() != reflect.Struct {
		return false
	}
	return !implementsTextMarshaler(t) && !implementsTextUnmarshaler(t)
}

// supportedFieldType 判断字段类型是否支持当前读/写方向。
func supportedFieldType(t reflect.Type, forRead bool) bool {
	if t == timeType {
		return true
	}
	if t.Kind() == reflect.Interface {
		return false // 接口没有具体类型可供分配，读写都无法可靠工作
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

// decodeStruct 把一行字段转换成结构体，汇总所有字段错误；不执行 Validate。
func decodeStruct[T any](fields []fieldSpec, values []string, line int) (T, error) {
	var out T
	root := reflect.ValueOf(&out).Elem()
	problems := make([]fieldProblem, 0, len(fields))
	for i, f := range fields {
		if err := setStructField(root, f, values[i]); err != nil {
			problems = append(problems, fieldProblem{column: f.header, err: err})
		}
	}
	if err := joinFieldErrors(line, problems); err != nil {
		var zero T
		return zero, err
	}
	return out, nil
}

// validateStruct 调用结构体可选的 Validate 方法。
func validateStruct[T any](value *T) error {
	if validator, ok := any(value).(interface{ Validate() error }); ok {
		return validator.Validate()
	}
	return nil
}

// setStructField 把一列字符串写入根结构体下的叶子字段。
//
// 错误不带行号与列名，由 decodeStruct 统一汇总。
func setStructField(root reflect.Value, f fieldSpec, raw string) error {
	if raw == "" {
		if f.required {
			return errors.New("必填字段为空")
		}
		return nil // 非指针保持零值；指针保持 nil
	}
	dst := root.FieldByIndex(f.index)
	holder := dst
	if f.pointer {
		holder = reflect.New(f.typ)
	}
	if err := parseFieldValue(holder, f, raw); err != nil {
		return err
	}
	if f.pointer {
		dst.Set(holder)
	}
	return nil
}

// parseFieldValue 把字符串解析到叶子字段。
//
// holder 是指针字段时为 *T，否则为可寻址的 T。string 字段保留原值，
// 其他类型去空白后解析。
func parseFieldValue(holder reflect.Value, f fieldSpec, raw string) error {
	value := holder
	if value.Kind() == reflect.Pointer {
		value = value.Elem()
	}
	text := strings.TrimSpace(raw)

	if f.typ == timeType {
		t, err := time.Parse(f.layout, text)
		if err != nil {
			return fmt.Errorf("值 %q 无法解析为时间: %w", truncate(raw, 64), err)
		}
		value.Set(reflect.ValueOf(t))
		return nil
	}
	if implementsTextUnmarshaler(f.typ) {
		u, ok := value.Addr().Interface().(encoding.TextUnmarshaler)
		if !ok {
			return fmt.Errorf("类型 %s 无法解引用为 TextUnmarshaler", f.typ)
		}
		if err := u.UnmarshalText([]byte(text)); err != nil {
			return fmt.Errorf("值 %q 无法解析为 %s: %w", truncate(raw, 64), f.typ, err)
		}
		return nil
	}

	var err error
	switch f.typ.Kind() {
	case reflect.String:
		value.SetString(raw)
		return nil
	case reflect.Bool:
		var b bool
		b, err = parseBool(text)
		value.SetBool(b)
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		var n int64
		n, err = strconv.ParseInt(text, 10, f.typ.Bits())
		value.SetInt(n)
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		var n uint64
		n, err = strconv.ParseUint(text, 10, f.typ.Bits())
		value.SetUint(n)
	case reflect.Float32, reflect.Float64:
		var n float64
		n, err = strconv.ParseFloat(text, f.typ.Bits())
		value.SetFloat(n)
	default:
		return fmt.Errorf("类型 %s 不支持解析", f.typ)
	}
	if err != nil {
		return fmt.Errorf("值 %q 无法解析为 %s: %w", truncate(raw, 64), f.typ, err)
	}
	return nil
}

// StructWriter 是结构体层的文本写出器。
//
// 用 NewStructWriter/NewFileStructWriter 构建；列顺序、表头与格式化方式来自
// T 的字段与 tag，构造期写出表头。Write 逐条追加，Close 提交，Abort 放弃。
// 与 Writer 一致，任一次失败后进入失败态。StructWriter 不是并发安全的。
type StructWriter[T any] struct {
	w      *Writer
	fields []fieldSpec
}

// NewStructWriter 创建写到 out 的结构体层写出器。
//
// opts.Columns 必须为空（列来自 T），否则返回 ErrInvalidColumn；其余选项语义
// 与 NewWriter 相同。T 的字段/tag 无效时返回 ErrInvalidColumn，且不产生输出。
func NewStructWriter[T any](out io.Writer, opts WriteOptions) (*StructWriter[T], error) {
	fields, err := prepareStruct[T](opts.Columns, false)
	if err != nil {
		return nil, err
	}
	if err := opts.checkBase(); err != nil {
		return nil, err
	}
	w, err := newWriter(out, opts, columnsFromFields(fields))
	if err != nil {
		return nil, err
	}
	return &StructWriter[T]{w: w, fields: fields}, nil
}

// NewFileStructWriter 创建原子写 path 的结构体层写出器。
//
// 语义与 NewStructWriter 相同；落盘规则同 NewFileWriter。
func NewFileStructWriter[T any](path string, opts WriteOptions) (*StructWriter[T], error) {
	fields, err := prepareStruct[T](opts.Columns, false)
	if err != nil {
		return nil, err
	}
	if err := opts.checkBase(); err != nil {
		return nil, err
	}
	w, err := newFileWriter(path, opts, columnsFromFields(fields))
	if err != nil {
		return nil, err
	}
	return &StructWriter[T]{w: w, fields: fields}, nil
}

// columnsFromFields 把字段声明转换成 map 层需要的列声明。
func columnsFromFields(fields []fieldSpec) []Column {
	cols := make([]Column, len(fields))
	for i, f := range fields {
		cols[i] = Column{Header: f.header, Key: f.header}
	}
	return cols
}

// Write 追加一条结构体记录。
//
// 字段按 tag 格式化：string 原样、bool 为 true/false、整数与浮点用十进制、
// time.Time 按 layout、TextMarshaler 调用 MarshalText、nil 指针写空字段。
// 格式化后的文本仍要过 Sanitize 与非法字符检查。Close/Abort 后返回 ErrClosed。
func (s *StructWriter[T]) Write(value T) error {
	if s.w.closed {
		return ErrClosed
	}
	if s.w.failed != nil {
		return s.w.failed
	}
	root := reflect.ValueOf(&value).Elem()
	fields := make([]string, len(s.fields))
	labels := make([]string, len(s.fields))
	for i, f := range s.fields {
		text, err := formatFieldValue(root, f)
		if err != nil {
			err = &RowError{Line: s.w.line, Column: f.header, Err: err}
			s.w.failed = err
			return err
		}
		fields[i] = text
		labels[i] = f.header
	}
	if err := s.w.writeFields(fields, labels); err != nil {
		s.w.failed = err
		return err
	}
	s.w.line++
	return nil
}

// Close 提交写出，语义与 Writer.Close 相同。
func (s *StructWriter[T]) Close() error {
	return s.w.Close()
}

// Abort 放弃写出，语义与 Writer.Abort 相同。
func (s *StructWriter[T]) Abort() error {
	return s.w.Abort()
}

// WriteFileFrom 把结构体切片原子写入 path。
//
// 语义与 NewFileStructWriter + Write + Close 相同；任一行失败时放弃临时文件
// 并返回错误，path 原内容保持不变。
func WriteFileFrom[T any](path string, rows []T, opts WriteOptions) error {
	w, err := NewFileStructWriter[T](path, opts)
	if err != nil {
		return err
	}
	for _, row := range rows {
		if err := w.Write(row); err != nil {
			return errors.Join(err, w.Abort())
		}
	}
	return w.Close()
}

// formatFieldValue 把一个结构体字段格式化成文本。
//
// root 必须是可寻址的结构体值，以便调用指针接收者的 TextMarshaler。
func formatFieldValue(root reflect.Value, f fieldSpec) (string, error) {
	dst := root.FieldByIndex(f.index)
	if f.pointer {
		if dst.IsNil() {
			return "", nil
		}
		dst = dst.Elem()
	}
	if f.typ == timeType {
		return dst.Interface().(time.Time).Format(f.layout), nil
	}
	if implementsTextMarshaler(f.typ) {
		var m encoding.TextMarshaler
		if dst.CanAddr() {
			m, _ = dst.Addr().Interface().(encoding.TextMarshaler)
		}
		if m == nil {
			m, _ = dst.Interface().(encoding.TextMarshaler)
		}
		if m == nil {
			return "", fmt.Errorf("类型 %s 无法转换为 TextMarshaler", f.typ)
		}
		b, err := m.MarshalText()
		if err != nil {
			return "", fmt.Errorf("值无法序列化为文本: %w", err)
		}
		return string(b), nil
	}
	switch dst.Kind() {
	case reflect.String:
		return dst.String(), nil
	case reflect.Bool:
		return strconv.FormatBool(dst.Bool()), nil
	case reflect.Int, reflect.Int8, reflect.Int16, reflect.Int32, reflect.Int64:
		return strconv.FormatInt(dst.Int(), 10), nil
	case reflect.Uint, reflect.Uint8, reflect.Uint16, reflect.Uint32, reflect.Uint64:
		return strconv.FormatUint(dst.Uint(), 10), nil
	case reflect.Float32:
		return strconv.FormatFloat(dst.Float(), 'f', -1, 32), nil
	case reflect.Float64:
		return strconv.FormatFloat(dst.Float(), 'f', -1, 64), nil
	default:
		return "", fmt.Errorf("类型 %s 不支持写出", f.typ)
	}
}
