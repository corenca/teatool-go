package parquet

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	pq "github.com/parquet-go/parquet-go"
)

// Writer 是类型化的 parquet 写出器。
//
// T 是结构体时 schema 由 struct tag 推导；T 是 map[string]any 时必须提供
// WriteOptions.Schema。写出是流式的：Write 把行交给上游编码，Close 写 footer
// 并提交。它不是并发安全的：同一实例只应被一个 goroutine 使用。
type Writer[T any] struct {
	writer *pq.GenericWriter[T]
	schema *Schema

	file   *os.File // NewWriter 时为 nil
	tmp    string
	target string

	failed error
	closed bool
}

// NewWriter 把 parquet 写入调用方提供的 io.Writer。
//
// 只写不负责提交语义：Close 之后数据才完整（footer 在 Close 时写出）；
// Abort 只是丢弃后续写入，已经写出的字节无法回收。需要原子落盘用 NewFileWriter。
func NewWriter[T any](w io.Writer, opts WriteOptions) (writer *Writer[T], err error) {
	defer guard("NewWriter", &err)
	gw, schema, err := newGenericWriter[T](w, opts)
	if err != nil {
		return nil, err
	}
	return &Writer[T]{writer: gw, schema: schema}, nil
}

// NewFileWriter 在 path 同目录创建临时文件，Close 成功后原子替换到 path。
//
// 目标文件权限固定为 0644，已存在的同名文件会被覆盖；Close 之前失败（或调用
// Abort）不会留下目标文件与临时文件。构造失败时不会创建任何文件。
func NewFileWriter[T any](path string, opts WriteOptions) (w *Writer[T], err error) {
	defer guard("NewFileWriter", &err)
	file, err := os.CreateTemp(filepath.Dir(path), ".parquet-*")
	if err != nil {
		return nil, err
	}
	w, err = NewWriter[T](file, opts)
	if err != nil {
		file.Close()
		os.Remove(file.Name())
		return nil, err
	}
	w.file = file
	w.tmp = file.Name()
	w.target = path
	return w, nil
}

// WriteFile 一次性把 rows 写成文件，等价于 NewFileWriter + Write + Close。
//
// 任一步失败都会删除临时文件并返回原错误；大文件或分批产出用 NewFileWriter。
func WriteFile[T any](path string, rows []T, opts WriteOptions) error {
	w, err := NewFileWriter[T](path, opts)
	if err != nil {
		return err
	}
	if err := w.Write(rows); err != nil {
		w.Abort()
		return err
	}
	return w.Close()
}

// newGenericWriter 校验选项并构造上游 writer。
func newGenericWriter[T any](out io.Writer, opts WriteOptions) (*pq.GenericWriter[T], *Schema, error) {
	t, err := typeOf[T]()
	if err != nil {
		return nil, nil, err
	}
	codec, err := compressionCodec(opts.Compression)
	if err != nil {
		return nil, nil, err
	}
	if opts.MaxRowsPerRowGroup < 0 || opts.PageBufferSize < 0 || opts.WriteBufferSize < 0 {
		return nil, nil, fmt.Errorf("%w: 行组与缓冲参数不能为负数", ErrInvalidOption)
	}

	var schema *Schema
	if isDynamicType(t) {
		if opts.Schema == nil {
			return nil, nil, fmt.Errorf("%w: 动态写出必须提供 Schema", ErrInvalidSchema)
		}
		schema = opts.Schema
	} else {
		if opts.Schema != nil {
			return nil, nil, fmt.Errorf("%w: 结构体写出不接受 Schema", ErrInvalidOption)
		}
		schema, err = modelSchema(t)
		if err != nil {
			return nil, nil, err
		}
	}

	options := make([]pq.WriterOption, 0, 6)
	options = append(options, pq.Compression(codec), schema)
	if opts.MaxRowsPerRowGroup > 0 {
		options = append(options, pq.MaxRowsPerRowGroup(opts.MaxRowsPerRowGroup))
	}
	if opts.PageBufferSize > 0 {
		options = append(options, pq.PageBufferSize(opts.PageBufferSize))
	}
	if opts.WriteBufferSize > 0 {
		options = append(options, pq.WriteBufferSize(opts.WriteBufferSize))
	}
	for k, v := range opts.KeyValueMetadata {
		options = append(options, pq.KeyValueMetadata(k, v))
	}
	return pq.NewGenericWriter[T](out, options...), schema, nil
}

// Write 写入一批行。
//
// 任一次调用失败后 Writer 进入失败态：后续 Write 与 Close 都返回同一个原错误，
// 且不会产出文件，只能调用 Abort 清理。上游对值类型不匹配会 panic，这里转成 error。
func (w *Writer[T]) Write(rows []T) (err error) {
	if w.closed {
		return ErrClosed
	}
	if w.failed != nil {
		return w.failed
	}
	defer func() {
		if err != nil {
			w.failed = err
		}
	}()
	defer guard("Write", &err)
	_, err = w.writer.Write(rows)
	return err
}

// Schema 返回本次写出使用的 schema。
func (w *Writer[T]) Schema() *Schema { return w.schema }

// Close 写出 footer 并提交。
//
// NewFileWriter 创建的 Writer 会 chmod 0644、关闭临时文件并 rename 到目标路径；
// rename 失败时删除临时文件。已经关闭时重复调用返回 nil。失败态下返回原错误，
// 不落盘。
func (w *Writer[T]) Close() (err error) {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.failed != nil {
		w.removeTemp()
		return w.failed
	}
	defer guard("Close", &err)
	if err = w.writer.Close(); err != nil {
		w.removeTemp()
		return err
	}
	if w.file == nil {
		return nil
	}
	if err = w.file.Chmod(0o644); err != nil {
		w.removeTemp()
		return err
	}
	if err = w.file.Close(); err != nil {
		w.removeTemp()
		return err
	}
	if err = os.Rename(w.tmp, w.target); err != nil {
		w.removeTemp()
		return err
	}
	return nil
}

// Abort 放弃写出并删除临时文件。
//
// 对 NewFileWriter 创建的 Writer，Abort 之后目标路径不存在；对 NewWriter 创建的
// Writer 只是丢弃后续写入（已写出的字节无法回收）。重复调用返回 nil。
func (w *Writer[T]) Abort() error {
	if w.closed {
		return nil
	}
	w.closed = true
	if w.file == nil {
		return nil
	}
	err := w.file.Close()
	os.Remove(w.tmp)
	return err
}

// removeTemp 关闭并删除临时文件，忽略清理过程中的错误。
func (w *Writer[T]) removeTemp() {
	if w.file == nil {
		return
	}
	w.file.Close()
	os.Remove(w.tmp)
}
