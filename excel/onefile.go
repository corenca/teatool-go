package excel

import (
	"errors"
	"fmt"
)

// ReadFile 读取 path 中指定工作表的全部行。
//
// sheet 为空时取第一个工作表；读取与关闭错误会合并返回。格式不支持返回
// ErrUnsupportedFormat，sheet 不存在返回 ErrSheetNotFound，行级错误是
// *RowError。这是单 sheet 文件的便捷入口；多 sheet 或流式读取用 OpenFile + Reader。
func ReadFile(path, sheet string, opts ReadOptions) ([]map[string]string, error) {
	rd, err := OpenFile(path)
	if err != nil {
		return nil, err
	}
	sh, err := rd.Sheet(sheet)
	if err != nil {
		return nil, errors.Join(err, rd.Close())
	}
	rows, err := sh.Read(opts)
	return rows, errors.Join(err, rd.Close())
}

// ReadFileInto 读取 path 中指定工作表并映射成结构体切片。
//
// sheet 为空时取第一个工作表；与 ReadInto 一样不接受 opts.Columns（非空返回
// ErrInvalidColumn）。列/tag 无效返回 ErrInvalidColumn，行级错误是 *RowError。
func ReadFileInto[T any](path, sheet string, opts ReadOptions) ([]T, error) {
	if _, _, _, err := prepareStruct[T](opts.Columns, true); err != nil {
		return nil, err
	}
	rd, err := OpenFile(path)
	if err != nil {
		return nil, err
	}
	sh, err := rd.Sheet(sheet)
	if err != nil {
		return nil, errors.Join(err, rd.Close())
	}
	out, err := sh.ReadInto[T](opts)
	return out, errors.Join(err, rd.Close())
}

// WriteFile 把单 sheet 的 map 行写入 path。
//
// sheet 必须非空，否则返回 ErrInvalidSheetName；缺 Key 返回 *RowError；
// 文件原子写出（临时文件 + rename，权限 0644）。多 sheet 用 NewFileBuilder + Builder。
func WriteFile(path, sheet string, rows []map[string]string, opts WriteOptions) error {
	if sheet == "" {
		return fmt.Errorf("%w: 名称为空", ErrInvalidSheetName)
	}
	b, err := NewFileBuilder(path)
	if err != nil {
		return err
	}
	if err := b.AddSheetRows(sheet, rows, opts); err != nil {
		b.Abort()
		return err
	}
	return b.Close()
}

// WriteFileFrom 把单 sheet 的结构体行写入 path。
//
// sheet 必须非空，否则返回 ErrInvalidSheetName；与 WriteFrom 一样不接受
// opts.Columns（非空返回 ErrInvalidColumn）。
func WriteFileFrom[T any](path, sheet string, rows []T, opts WriteOptions) error {
	if sheet == "" {
		return fmt.Errorf("%w: 名称为空", ErrInvalidSheetName)
	}
	if _, _, _, err := prepareStruct[T](opts.Columns, false); err != nil {
		return err
	}
	b, err := NewFileBuilder(path)
	if err != nil {
		return err
	}
	if err := b.WriteFrom[T](sheet, rows, opts); err != nil {
		b.Abort()
		return err
	}
	return b.Close()
}
