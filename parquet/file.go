package parquet

import (
	"fmt"
	"io"
	"os"

	pq "github.com/parquet-go/parquet-go"
)

// magic 是 parquet 文件的首尾魔数。
const magic = "PAR1"

// File 是只读的文件句柄，用于读取 footer 元信息与 schema。
//
// 它不持有数据页，也不参与读取行；读取行用 OpenReader。Close 之后
// Meta/Schema 的返回值仍然可用（已复制），但不应再调用 Close。
type File struct {
	file   *pq.File
	closer io.Closer
}

// Open 打开本地 parquet 文件并读取 footer。
//
// path 是文件路径。返回的 File 独占一个文件描述符，用完必须 Close。
// 文件不存在等 IO 错误原样返回；首尾魔数不对、长度不足或使用了不支持的
// 加密 footer 时返回包装 ErrNotParquet 的错误。
func Open(path string) (*File, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	file, err := openReaderAt(f, info.Size(), path)
	if err != nil {
		f.Close()
		return nil, err
	}
	file.closer = f
	return file, nil
}

// OpenAt 从调用方提供的随机访问数据源读取 footer。
//
// r 必须支持从任意偏移读取；size 是数据源的总长度。Close 不会关闭 r，
// 关闭数据源由调用方负责。魔数/长度问题返回包装 ErrNotParquet 的错误。
//
// 典型用法是把内存中的文件内容包装成 bytes.Reader，或自己实现基于区间读取的
// io.ReaderAt（例如对象存储）。
func OpenAt(r io.ReaderAt, size int64) (*File, error) {
	return openReaderAt(r, size, "")
}

// openReaderAt 是 Open/OpenAt 的共同实现，path 只用于错误信息。
//
// 上游 OpenFile 在读 footer 时理论上可能 panic（例如极端损坏的元数据），
// 这里用 guard 兜住，保证 Open/OpenAt 只返回 error。
func openReaderAt(r io.ReaderAt, size int64, path string) (f *File, err error) {
	defer guard("Open", &err)
	if err := checkMagic(r, size); err != nil {
		return nil, locateError(path, err)
	}
	pf, err := pq.OpenFile(r, size)
	if err != nil {
		return nil, locateError(path, err)
	}
	return &File{file: pf}, nil
}

// Meta 返回文件元信息。
func (f *File) Meta() Meta {
	m := f.file.Metadata()
	meta := Meta{
		NumRows:   f.file.NumRows(),
		RowGroups: len(f.file.RowGroups()),
		Size:      f.file.Size(),
		CreatedBy: m.CreatedBy,
	}
	if n := len(m.KeyValueMetadata); n > 0 {
		meta.KeyValue = make(map[string]string, n)
		for _, kv := range m.KeyValueMetadata {
			meta.KeyValue[kv.Key] = kv.Value
		}
	}
	return meta
}

// Schema 返回文件 schema，可用于查看列路径与类型、或作为动态读取的目标 schema。
func (f *File) Schema() *Schema { return f.file.Schema() }

// Close 关闭由 Open 打开的文件。重复调用返回 nil。
//
// 对 OpenAt 返回的 File 是空操作，不会关闭调用方的 io.ReaderAt。
func (f *File) Close() error {
	if f.closer == nil {
		return nil
	}
	closer := f.closer
	f.closer = nil
	return closer.Close()
}

// checkMagic 校验首尾魔数。长度不足 8 字节、头部不是 PAR1、尾部是加密 footer
// 或不是 PAR1 都返回 ErrNotParquet。
func checkMagic(r io.ReaderAt, size int64) error {
	if size < 8 {
		return fmt.Errorf("%w: 文件长度 %d 字节，不足 8 字节", ErrNotParquet, size)
	}
	head := make([]byte, 4)
	tail := make([]byte, 4)
	if _, err := r.ReadAt(head, 0); err != nil {
		return fmt.Errorf("%w: 读取文件头失败: %v", ErrNotParquet, err)
	}
	if _, err := r.ReadAt(tail, size-4); err != nil {
		return fmt.Errorf("%w: 读取文件尾失败: %v", ErrNotParquet, err)
	}
	if string(head) != magic {
		return fmt.Errorf("%w: 文件头不是 PAR1", ErrNotParquet)
	}
	if string(tail) == "PARE" {
		return fmt.Errorf("%w: 不支持加密 footer", ErrNotParquet)
	}
	if string(tail) != magic {
		return fmt.Errorf("%w: 文件尾不是 PAR1", ErrNotParquet)
	}
	return nil
}

// locateError 在错误信息前补上文件路径，方便定位是哪个文件出了问题。
func locateError(path string, err error) error {
	if path == "" {
		return err
	}
	return fmt.Errorf("%s: %w", path, err)
}
