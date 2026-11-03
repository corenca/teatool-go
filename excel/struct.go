package excel

import (
	"errors"
	"fmt"
	"reflect"
	"strconv"
	"strings"
	"time"

	"github.com/xuri/excelize/v2"
)

// timeType 是 time.Time 的类型对象，用于和字段类型比较。
var timeType = reflect.TypeFor[time.Time]()

// fieldSpec 是结构体字段解析后的列声明。
type fieldSpec struct {
	index    int          // 字段在结构体中的下标
	header   string       // Excel 表头名
	typ      reflect.Type // 字段类型；指针字段取元素类型
	layout   string       // time.Time 的布局
	required bool         // 空值是否报错
	width    float64      // 写出列宽
	pointer  bool         // 是否为指针字段
}

// ReadInto 把当前工作表读成结构体切片。
//
// 字段与 tag 决定列；不接受 opts.Columns。T 只出现在返回值，调用时必须
// 显式写 s.ReadInto[User](opts)。列/tag 无效返回 ErrInvalidColumn；Reader
// 已关闭返回 ErrClosed；行级错误以 *RowError 返回。
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
// Reader 已关闭返回 ErrClosed。fn 不能为 nil。
func (s *SheetReader) EachInto[T any](opts ReadOptions, fn func(rowNum int, value T) error) error {
	return eachIntoSheet[T](s, opts, fn)
}

// eachIntoSheet 是 ReadInto/EachInto 的公共实现。
func eachIntoSheet[T any](s *SheetReader, opts ReadOptions, fn func(rowNum int, value T) error) error {
	if err := s.reader.check(); err != nil {
		return err
	}
	typ, fields, hasTime, err := prepareStruct[T](opts.Columns)
	if err != nil {
		return err
	}
	cols := make([]Column, len(fields))
	for i, f := range fields {
		// 结构体层用表头作为内部键，行级错误里显示的就是用户看到的那一列。
		cols[i] = Column{Header: f.header, Key: f.header, Required: f.required}
	}
	opts.Columns = cols

	return s.read(opts, hasTime, func(rowNum int, row map[string]string, date1904 bool) error {
		value, err := decodeStructValue[T](typ, fields, row, rowNum, date1904)
		if err != nil {
			return err
		}
		return fn(rowNum, value)
	})
}

// decodeStructValue 把一行 map 转换成结构体并执行 Validate。
func decodeStructValue[T any](typ reflect.Type, fields []fieldSpec, row map[string]string, rowNum int, date1904 bool) (T, error) {
	value := reflect.New(typ).Elem()
	for _, f := range fields {
		if err := setStructField(value.Field(f.index), f, row[f.header], rowNum, date1904); err != nil {
			var zero T
			return zero, err
		}
	}
	if validator, ok := value.Addr().Interface().(interface{ Validate() error }); ok {
		if err := validator.Validate(); err != nil {
			var zero T
			return zero, &RowError{Row: rowNum, Err: err}
		}
	}
	return value.Interface().(T), nil
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
	_, fields, _, err := prepareStruct[T](opts.Columns)
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
	rv := reflect.ValueOf(value)
	for _, f := range w.fields {
		text, err := formatField(rv.Field(f.index), f)
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
func prepareStruct[T any](cols []Column) (reflect.Type, []fieldSpec, bool, error) {
	if len(cols) > 0 {
		return nil, nil, false, fmt.Errorf("%w: 结构体层不接受 Columns", ErrInvalidColumn)
	}
	typ := reflect.TypeFor[T]()
	if typ.Kind() != reflect.Struct {
		return nil, nil, false, fmt.Errorf("%w: 目标类型必须是结构体，实际是 %s", ErrInvalidColumn, typ)
	}
	fields, err := structFields(typ)
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

// structFields 解析结构体的全部导出字段。
func structFields(typ reflect.Type) ([]fieldSpec, error) {
	fields := make([]fieldSpec, 0, typ.NumField())
	headers := make(map[string]struct{}, typ.NumField())
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		if f.PkgPath != "" {
			continue // 未导出字段忽略
		}
		if f.Anonymous {
			return nil, fmt.Errorf("%w: 不支持匿名字段 %s", ErrInvalidColumn, f.Name)
		}
		tag := f.Tag.Get("excel")
		if name, _, _ := strings.Cut(tag, ","); strings.TrimSpace(name) == "-" {
			continue
		}
		spec, err := parseFieldTag(f, i, tag)
		if err != nil {
			return nil, err
		}
		if _, ok := headers[spec.header]; ok {
			return nil, fmt.Errorf("%w: 字段 %s 的表头 %q 重复", ErrInvalidColumn, f.Name, spec.header)
		}
		headers[spec.header] = struct{}{}
		fields = append(fields, spec)
	}
	if len(fields) == 0 {
		return nil, fmt.Errorf("%w: 结构体没有可导出的字段", ErrInvalidColumn)
	}
	return fields, nil
}

// parseFieldTag 解析 excel tag 与字段类型。
func parseFieldTag(f reflect.StructField, index int, tag string) (fieldSpec, error) {
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
	if !supportedFieldType(spec.typ) {
		return spec, fmt.Errorf("%w: 字段 %s 的类型 %s 不支持", ErrInvalidColumn, f.Name, f.Type)
	}
	if spec.typ == timeType && spec.layout == "" {
		return spec, fmt.Errorf("%w: time.Time 字段 %s 必须提供 layout", ErrInvalidColumn, f.Name)
	}
	return spec, nil
}

// supportedFieldType 判断字段类型是否在白名单内。
func supportedFieldType(t reflect.Type) bool {
	if t == timeType {
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

// setStructField 把一列字符串写入字段。
func setStructField(dst reflect.Value, f fieldSpec, raw string, rowNum int, date1904 bool) error {
	if raw == "" {
		if f.required {
			return &RowError{Row: rowNum, Column: f.header, Err: errors.New("必填字段为空")}
		}
		return nil
	}
	holder := dst
	if f.pointer {
		holder = reflect.New(f.typ)
	}
	if err := parseFieldValue(holder, f, raw, date1904); err != nil {
		return &RowError{Row: rowNum, Column: f.header, Err: err}
	}
	if f.pointer {
		dst.Set(holder)
	}
	return nil
}

// parseFieldValue 把字符串解析到字段值。holder 可能是指针。
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

// formatField 把一个字段值写成字符串。
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
