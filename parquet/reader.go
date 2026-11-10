package parquet

import (
	"errors"
	"fmt"
	"io"
	"slices"

	pq "github.com/parquet-go/parquet-go"
)

// readBatchSize 是 Each/ReadFile 内部每次从上游读取的行数。
const readBatchSize = 1024

// Reader 是类型化的流式读取器。
//
// T 是结构体或结构体指针，schema 由 struct tag 推导；列名为点号路径，
// 支持嵌套结构体。它不是并发安全的：同一实例只应被一个 goroutine 使用。
type Reader[T any] struct {
	reader *pq.GenericReader[T]
	file   *File
	schema *Schema
	closed bool
}

// OpenReader 打开本地 parquet 文件并准备按 T 读取。
//
// 构造期依次做：打开并校验文件、投影校验（Columns 必须存在）、目标 schema 推导、
// 缺列校验（默认报 *MissingColumnsError）、类型兼容校验，最后定位到 SkipRows。
// 任何阶段失败都返回 error 而不是 panic。
func OpenReader[T any](path string, opts ReadOptions) (r *Reader[T], err error) {
	defer guard("OpenReader", &err)
	f, err := Open(path)
	if err != nil {
		return nil, err
	}
	r, err = openReader[T](f, opts)
	if err != nil {
		f.Close()
		return nil, err
	}
	return r, nil
}

// OpenReaderAt 从调用方提供的随机访问数据源读取，语义同 OpenReader。
//
// Close 不会关闭 r；数据源由调用方负责。
func OpenReaderAt[T any](r io.ReaderAt, size int64, opts ReadOptions) (reader *Reader[T], err error) {
	defer guard("OpenReaderAt", &err)
	f, err := OpenAt(r, size)
	if err != nil {
		return nil, err
	}
	reader, err = openReader[T](f, opts)
	if err != nil {
		f.Close()
		return nil, err
	}
	return reader, nil
}

// ReadFile 读取文件全部行，语义同 OpenReader。
//
// 结果一次性放入内存；大文件用 OpenReader + Read/Each 流式处理。
func ReadFile[T any](path string, opts ReadOptions) ([]T, error) {
	r, err := OpenReader[T](path, opts)
	if err != nil {
		return nil, err
	}
	rows, err := readAll(r)
	return rows, errors.Join(err, r.Close())
}

// openReader 在已打开的文件上构造 Reader。
func openReader[T any](f *File, opts ReadOptions) (r *Reader[T], err error) {
	defer guard("构造 Reader", &err)

	t, err := typeOf[T]()
	if err != nil {
		return nil, err
	}
	if opts.SkipRows < 0 {
		return nil, fmt.Errorf("%w: SkipRows 不能为负数", ErrInvalidOption)
	}
	fileSchema := f.Schema()
	if err := checkProjection(opts, fileSchema); err != nil {
		return nil, err
	}

	var target *Schema
	if isDynamicType(t) {
		target = dynamicReadSchema(fileSchema, opts)
	} else {
		if opts.Schema != nil {
			return nil, fmt.Errorf("%w: 结构体读取不接受 Schema", ErrInvalidOption)
		}
		target, err = modelSchema(t)
		if err != nil {
			return nil, err
		}
	}
	// 投影是显式裁剪：未被投影的列不参与缺列与类型兼容校验。
	selected := selectedPaths(target, opts.Columns)
	if err := checkMissingColumns(target, fileSchema, opts.AllowMissingColumns, selected); err != nil {
		return nil, err
	}
	if err := checkCompatible(target, fileSchema, selected); err != nil {
		return nil, err
	}
	projected, err := projectSchema(target, opts.Columns)
	if err != nil {
		return nil, err
	}

	reader := pq.NewGenericRowGroupReader[T](rowGroupOf(f), projected)
	if opts.SkipRows > 0 {
		if err := reader.SeekToRow(opts.SkipRows); err != nil {
			return nil, err
		}
	}
	return &Reader[T]{reader: reader, file: f, schema: target}, nil
}

// rowGroupOf 返回覆盖整个文件的行组视图。
//
// 上游的 fileRowGroupOf 未导出：单个行组直接使用，多个行组用 MultiRowGroup
// 拼接，0 个行组（0 行文件）用一个空 Buffer 占位。
func rowGroupOf(f *File) pq.RowGroup {
	groups := f.file.RowGroups()
	switch len(groups) {
	case 0:
		return pq.NewBuffer(f.file.Schema())
	case 1:
		return groups[0]
	default:
		return pq.MultiRowGroup(groups...)
	}
}

// readAll 把 Reader 的全部行读入切片。
func readAll[T any](r *Reader[T]) ([]T, error) {
	buf := make([]T, readBatchSize)
	var out []T
	for {
		n, err := r.Read(buf)
		out = append(out, buf[:n]...)
		if errors.Is(err, io.EOF) {
			return out, nil
		}
		if err != nil {
			return out, err
		}
	}
}

// Read 读取最多 len(rows) 行，返回读到的行数。
//
// 没有更多行时返回 0 与 io.EOF；关闭后返回 ErrClosed。已读出的值不与
// Reader 的内部缓冲区共享内存，可以安全保留。
func (r *Reader[T]) Read(rows []T) (n int, err error) {
	if r.closed {
		return 0, ErrClosed
	}
	defer guard("Read", &err)
	ensureDynamicMaps(rows)
	return r.reader.Read(rows)
}

// Each 按批读取并对每行调用 fn。
//
// fn 返回 nil 继续；返回 ErrStop（可用 errors.Is 判断）表示成功提前结束，
// Each 返回 nil；返回其他错误立即停止并原样返回。已读的行由 fn 自己保存。
func (r *Reader[T]) Each(fn func(T) error) error {
	return r.EachBatch(0, func(rows []T) error {
		for i := range rows {
			if err := fn(rows[i]); err != nil {
				return err
			}
		}
		return nil
	})
}

// EachBatch 按批读取并把整批交给 fn，适合批量入库、批量 RPC 这类场景。
//
// batchSize 是每批的最大行数，<= 0 时使用默认值 1024。fn 收到的切片是本批数据的
// 独立副本，归调用方所有，可以安全保留（例如 append 到结果集）。返回 nil 继续；
// 返回 ErrStop（可用 errors.Is 判断）表示成功提前结束，EachBatch 返回 nil；
// 返回其他错误立即停止并原样返回。
func (r *Reader[T]) EachBatch(batchSize int, fn func([]T) error) error {
	if r.closed {
		return ErrClosed
	}
	if batchSize <= 0 {
		batchSize = readBatchSize
	}
	buf := make([]T, batchSize)
	for {
		n, err := r.Read(buf)
		if n > 0 {
			// 每批克隆一份：调用方保留批次时不会与下一次读取互相覆盖。
			if fnErr := fn(slices.Clone(buf[:n])); fnErr != nil {
				if errors.Is(fnErr, ErrStop) {
					return nil
				}
				return fnErr
			}
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return err
		}
	}
}

// SeekToRow 把读取位置移到指定行（0-based），下一次 Read 从该行开始。
func (r *Reader[T]) SeekToRow(i int64) (err error) {
	if r.closed {
		return ErrClosed
	}
	defer guard("SeekToRow", &err)
	return r.reader.SeekToRow(i)
}

// NumRows 返回文件总行数，不受 SkipRows 影响。
func (r *Reader[T]) NumRows() int64 { return r.file.Meta().NumRows }

// Schema 返回本次读取使用的目标 schema。
func (r *Reader[T]) Schema() *Schema { return r.schema }

// Close 关闭由 OpenReader 打开的文件；对 OpenReaderAt 创建的 Reader 是空操作。
//
// 重复调用返回 nil。
func (r *Reader[T]) Close() error {
	if r.closed {
		return nil
	}
	r.closed = true
	return r.file.Close()
}
