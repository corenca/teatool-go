# parquet

parquet 列式文件的类型化读写。底层是
[`parquet-go/parquet-go`](https://github.com/parquet-go/parquet-go)，本包在此之上补齐：

- **构造与读写入口不 panic**：打开失败、schema 转换失败、值类型不合法，全部返回 error。
- **读取前校验**：投影列是否存在、目标类型缺不缺列、同名列的类型/时间单位对不对，
  默认拒绝静默零值与静默错时间。
- **原子写出**：先写同目录临时文件，`Close` 成功后 `rename`，失败不留半成品。
- **配置友好的选项**：压缩用字符串（`snappy`/`zstd`/`gzip`/`lz4`/`none`）。

结构体 + struct tag 是主路径；`map[string]any` 动态模式只用于探查、格式转换这类
编译期 schema 未知的场景。

## 安装

```bash
go get github.com/corenca/teatool-go/parquet
```

包名与上游同名，同时使用时给上游起别名：

```go
import (
    "github.com/corenca/teatool-go/parquet"
    pq "github.com/parquet-go/parquet-go"
)
```

`parquet.Schema` / `Field` / `Value` / `Row` 是上游类型的**别名**，两边可以互相传递。

## 快速开始

```go
type User struct {
    ID    int64     `parquet:"id"`
    Name  string    `parquet:"name"`
    Score float64   `parquet:"score"`
    At    time.Time `parquet:"at,timestamp(microsecond)"`
    Tags  []string  `parquet:"tags,list"`
}

// 写：原子落盘，失败不留文件
err := parquet.WriteFile("users.parquet", users, parquet.WriteOptions{})

// 读：整文件读入内存
got, err := parquet.ReadFile[User]("users.parquet", parquet.ReadOptions{})

// 读：流式，不整文件入内存
r, err := parquet.OpenReader[User]("users.parquet", parquet.ReadOptions{})
if err != nil {
    return err
}
defer r.Close()
for {
    var buf [1024]User
    n, err := r.Read(buf[:])
    // 处理 buf[:n]……
    if err == io.EOF {
        break
    }
    if err != nil {
        return err
    }
}
```

## 只读部分列

```go
users, err := parquet.ReadFile[User]("users.parquet", parquet.ReadOptions{
    Columns: []string{"id", "name"}, // 顶层列名或点号嵌套路径
})
// users[i].Score 是零值，未投影的列不会读取
```

投影是显式裁剪：没被投影的字段读出来是零值，**不会**因此报缺列，也不会参与
类型兼容校验——只有被投影的列会被读、被校验。列名在文件里不存在才报
`ErrUnknownColumn`。既可以用叶子路径（`addr.city`），也可以只写顶层列名
（`tags` 整列读 LIST/MAP）。嵌套结构体支持部分投影；LIST/MAP 内部不能拆开读。

跳过前 N 行（例如增量文件里已处理的部分）：

```go
r, err := parquet.OpenReader[User]("users.parquet", parquet.ReadOptions{SkipRows: 1000})
```

`SkipRows` 用上游 `SeekToRow` 定位，不扫描被跳过的数据；超过总行数时 `Read`
直接返回 `io.EOF`。也可以随时调用 `r.SeekToRow(i)`。

## 批量读取

逐行回调用 `Each`；要凑批（批量入库、批量 RPC）用 `EachBatch`：

```go
var kept [][]User
err := r.EachBatch(500, func(rows []User) error {
    // rows 是本批数据的独立副本，归你所有，可以直接保留
    kept = append(kept, rows)
    return db.BulkInsert(ctx, rows)
})
```

- `batchSize <= 0` 使用默认值 1024。
- 每批会克隆一份交给回调，因此可以安全保留；代价是每批一次 memcpy。
- 回调返回 `ErrStop` → 停止并返回 nil；返回其他错误 → 原样返回。
- 不提供对象池：复用行切片会让“回调里 append 保留”变成静默数据覆盖；
  上游的页缓冲与读缓冲已在库内管理，这里只多一次批次拷贝。

## 查看文件结构

```go
f, err := parquet.Open("users.parquet")
if err != nil {
    return err
}
defer f.Close()

fmt.Println(f.Meta().NumRows, f.Meta().RowGroups, f.Meta().CreatedBy)
fmt.Println(f.Schema().String())   // 列路径、物理类型、逻辑类型
fmt.Println(f.Schema().Columns())  // 全部叶子列路径
```

`Open` 只读 footer，不读数据页；`Meta().KeyValue` 是文件级元信息。
内存里的内容用 `parquet.OpenAt(bytes.NewReader(b), int64(len(b)))`，或自己实现
`io.ReaderAt`（例如基于区间读取的对象存储）。

## 类型映射与 tag

列声明直接用上游的 `parquet:"列名,选项"`：

| Go 类型 | parquet 类型 | 说明 |
| --- | --- | --- |
| `string` | BYTE_ARRAY + UTF8 | |
| `bool` | BOOLEAN | |
| `int32` / `int64` | INT32 / INT64 | `int` 会映射成 INT64，但宽度随平台，不推荐 |
| `float32` / `float64` | FLOAT / DOUBLE | |
| `time.Time` | TIMESTAMP | **默认纳秒**，读外部文件通常要显式写单位 |
| `time.Time` + `date` | DATE | |
| `[]byte` | BYTE_ARRAY | 无逻辑类型 |
| `[]T` | LIST | 需要 `,list` 才是标准三层 LIST |
| `map[K]V` | MAP | 键值用 `parquet-key` / `parquet-value` |
| 嵌套结构体 | GROUP | 点号路径投影 |
| `*T` | optional | 用指针表达"可能为空" |

### 时间戳 tag

```go
At    time.Time `parquet:"at,timestamp(microsecond)"`        // UTC 归一，对应 Spark timestamp
At    time.Time `parquet:"at,timestamp(microsecond:local)"`  // 不归一，对应 Spark timestamp_ntz
Day   time.Time `parquet:"day,date"`
```

可用单位：`millisecond`、`microsecond`、`nanosecond`。**单位必须与文件一致**：
本包会比对文件列与目标列的逻辑类型，不一致直接返回
`*IncompatibleColumnsError`，并在错误里给出建议 tag：

```
parquet: 列类型不兼容: 列 "ts" 文件 TIMESTAMP(isAdjustedToUTC=true,unit=MICROS)，
目标 TIMESTAMP(isAdjustedToUTC=true,unit=NANOS)；请改为 parquet:"ts,timestamp(microsecond)";
```

### 可空字段

parquet 的 optional 列读到非指针字段时会填零值，与"真的写入了零"无法区分。
需要区分时用指针：

```go
Note *string `parquet:"note"` // 文件为 null → nil
```

## 写出

```go
w, err := parquet.NewFileWriter[User]("users.parquet", parquet.WriteOptions{
    Compression:        "zstd",
    MaxRowsPerRowGroup: 100_000,
    KeyValueMetadata:   map[string]string{"source": "crm"},
})
if err != nil {
    return err
}
for _, batch := range batches {
    if err := w.Write(batch); err != nil {
        w.Abort()
        return err
    }
}
return w.Close()
```

- `Close` 之后才写出 footer 并 `rename` 到目标路径（权限 0644，覆盖同名文件）。
- 目标文件在 `Close` 成功之前**不存在**，`Abort` 会删掉临时文件。
- 失败态：`Write` 因值类型不合法失败后，`Writer` 进入失败态，后续 `Write`/`Close`
  返回同一个原错误，只能 `Abort`。
- **磁盘/写入端 IO 错误通常从 `Close` 返回**，不是 `Write`：上游把页缓冲在内存，
  提交时才 flush。
- 只想写到任意 `io.Writer`（比如 HTTP 响应）用 `NewWriter`，它没有原子性。

`Compression` 取值：`""`（等同 `snappy`）、`snappy`、`zstd`、`gzip`、`lz4`、`none`。
struct tag 里单独写了压缩的列（`parquet:"name,zstd"`）以 tag 为准。

## 提前结束遍历

```go
n := 0
err := r.Each(func(u User) error {
    n++
    if n == 10 {
        return parquet.ErrStop // 停止遍历，Each 返回 nil
    }
    return nil
})
```

`EachBatch` 的 `ErrStop` 语义相同，只是在整批处理完之后停止。回调返回其他错误时
立即停止并原样返回该错误；已读的行由回调自己保存。

## 动态模式（非主路径）

schema 在编译期未知时用 `map[string]any`。读取不传 `Schema` 就用文件 schema，
不需要先定义结构体：

```go
rows, err := parquet.ReadFile[map[string]any]("unknown.parquet", parquet.ReadOptions{})
```

值模型是 **parquet 物理值**，不是逻辑类型对应的 Go 类型：

| parquet | map 里的值 |
| --- | --- |
| BOOLEAN | `bool` |
| INT32 | `int32` |
| INT64 | `int64` |
| FLOAT / DOUBLE | `float32` / `float64` |
| BYTE_ARRAY / FIXED_LEN_BYTE_ARRAY | `string`（二进制也是 string） |
| date / time / timestamp | 底层物理值（`int32`/`int64`），时间戳按列单位解释 |
| decimal | `*big.Float`（已按 scale 还原） |
| LIST | `[]any` |
| MAP | `map[K]any`，值可能是指针（如 `*int32`） |
| GROUP | `map[string]any` |
| null | `nil` |

值是上游 `AssignValue` 的结果，与物理存储不完全对应：需要 decimal 的原始字节时，
用结构体字段 `[]byte` + `decimal(2:10)` 读未缩放的补码整数。

写出必须显式给 `Schema`，否则返回 `ErrInvalidSchema`：

```go
schema := pq.SchemaOf(new(User))
err := parquet.WriteFile[map[string]any]("out.parquet", rows, parquet.WriteOptions{Schema: schema})
```

动态写出不做类型校验：值的 Go 类型与列语义不符时（例如把字符串写进 INT32 列），
上游会 panic，本包把它转成 error 并让 `Writer` 进入失败态。业务读写请用结构体。

## API 一览

包级函数：

| 函数 | 用途 |
| --- | --- |
| `ReadFile[T](path, opts) ([]T, error)` | 一次读完整文件 |
| `WriteFile[T](path, rows, opts) error` | 一次写出全部行（原子落盘） |

文件元信息 `*File`：

| API | 用途 |
| --- | --- |
| `Open(path) (*File, error)` | 打开本地文件并读 footer |
| `OpenAt(r io.ReaderAt, size int64) (*File, error)` | 从随机访问数据源读 footer（不接管 `r`） |
| `(*File).Meta() Meta` | 行数、行组数、大小、创建者、key/value |
| `(*File).Schema() *Schema` | 文件 schema（`String()`/`Columns()` 查看列） |
| `(*File).Close() error` | 关闭由 `Open` 打开的文件；重复调用返回 nil |

读取 `*Reader[T]`：

| API | 用途 |
| --- | --- |
| `OpenReader[T](path, opts) (*Reader[T], error)` | 打开文件并校验 schema |
| `OpenReaderAt[T](r io.ReaderAt, size int64, opts) (*Reader[T], error)` | 从随机访问数据源读 |
| `(*Reader[T]).Read(rows []T) (int, error)` | 读一批，`io.EOF` 结束，行可安全保留 |
| `(*Reader[T]).Each(fn func(T) error) error` | 逐行回调 |
| `(*Reader[T]).EachBatch(size int, fn func([]T) error) error` | 批量回调，批次是独立副本、可保留 |
| `(*Reader[T]).SeekToRow(i int64) error` | 定位到第 i 行（0-based） |
| `(*Reader[T]).NumRows() int64` | 文件总行数 |
| `(*Reader[T]).Schema() *Schema` | 本次读取实际使用的 schema（已裁剪投影） |
| `(*Reader[T]).Close() error` | 关闭（`OpenReaderAt` 创建的为空操作） |

写出 `*Writer[T]`：

| API | 用途 |
| --- | --- |
| `NewWriter[T](w io.Writer, opts) (*Writer[T], error)` | 写到任意 `io.Writer`，无原子性 |
| `NewFileWriter[T](path, opts) (*Writer[T], error)` | 写临时文件，`Close` 时 rename |
| `(*Writer[T]).Write(rows []T) error` | 追加一批行 |
| `(*Writer[T]).Schema() *Schema` | 本次写出使用的 schema |
| `(*Writer[T]).Close() error` | 写 footer、提交并落盘 |
| `(*Writer[T]).Abort() error` | 放弃并删除临时文件 |

类型与选项：

| 名称 | 说明 |
| --- | --- |
| `Schema` / `Field` / `Value` / `Row` | 上游类型的别名，可直接传给上游 API |
| `Meta` | `NumRows`/`RowGroups`/`Size`/`CreatedBy`/`KeyValue` |
| `ReadOptions` | `Columns`/`Schema`/`AllowMissingColumns`/`SkipRows` |
| `WriteOptions` | `Schema`/`Compression`/`MaxRowsPerRowGroup`/`PageBufferSize`/`WriteBufferSize`/`KeyValueMetadata` |
| `MissingColumnsError` | `Columns []string`，`Unwrap() → ErrMissingColumn` |
| `IncompatibleColumnsError` | `Columns []ColumnMismatch`，`Unwrap() → ErrIncompatibleColumn` |
| `ColumnMismatch` | `Path`/`Target`/`File`/`Hint` |

## 选项速查

### ReadOptions

| 字段 | 含义 | 默认 |
| --- | --- | --- |
| `Columns` | 列投影，点号路径；空表示全部 | 空 |
| `Schema` | 动态模式的目标 schema | 文件 schema |
| `AllowMissingColumns` | 允许目标类型有、文件没有的列，读成零值 | `false` |
| `SkipRows` | 读取前跳过的行数，负数报错 | `0` |

### WriteOptions

| 字段 | 含义 | 默认 |
| --- | --- | --- |
| `Schema` | 动态模式必填；结构体模式不允许 | 由 tag 推导 |
| `Compression` | 列默认压缩 | `snappy` |
| `MaxRowsPerRowGroup` | 每个行组最大行数，0 不限制 | `0` |
| `PageBufferSize` | 页缓冲字节数，0 用上游默认 | 256 KiB |
| `WriteBufferSize` | 写缓冲字节数，0 用上游默认 | 32 KiB |
| `KeyValueMetadata` | 文件级元信息 | 空 |

## 错误处理

全部用 `errors.Is` 判断：

| 哨兵 | 含义 |
| --- | --- |
| `ErrNotParquet` | 长度不足、首尾魔数不对、加密 footer |
| `ErrInvalidOption` | 未知压缩名、负数参数、结构体模式传了 `Schema` |
| `ErrInvalidSchema` | T 形态不支持、结构体没有导出列、动态写缺 `Schema` |
| `ErrUnknownColumn` | 投影里的列在文件中不存在 |
| `ErrMissingColumn` | 目标类型需要的列文件里没有，见 `*MissingColumnsError` |
| `ErrIncompatibleColumn` | 同名列类型不兼容，见 `*IncompatibleColumnsError` |
| `ErrClosed` | `Reader`/`Writer` 已关闭 |
| `ErrStop` | 只在 `Each` 回调里表示成功提前结束 |

```go
var missing *parquet.MissingColumnsError
if errors.As(err, &missing) {
    log.Printf("文件缺少列: %v", missing.Columns) // []string{"age"}
}
```

## 常见错误

| 现象 | 原因 | 处理 |
| --- | --- | --- |
| `*IncompatibleColumnsError`，提示 `timestamp` | 文件时间单位与 tag 不一致 | 按提示改 tag，如 `timestamp(microsecond)` |
| `*MissingColumnsError` | 结构体有文件没有的列 | 删字段、改 tag，或确认后开 `AllowMissingColumns` |
| `ErrUnknownColumn` | 投影列名拼错，或嵌套路径层级不对 | 用 `File.Schema().Columns()` 查看真实路径 |
| `ErrNotParquet` | 不是 parquet 文件、文件被截断、`.parquet` 只是后缀 | 确认来源；csv/xlsx 用 `dsv`/`excel` 包 |
| 时间相差 1000 倍 | 单位不匹配（旧版本或直接调上游） | 本包会拦；如手工关掉校验，按文件 `unit` 写 tag |
| `Write` 成功但 `Close` 报错 | 磁盘满、管道断开等 IO 错误 | 检查目标可写；`Writer` 已失败，重新写新文件 |

## 并发

`Reader`/`Writer` 都不是并发安全的：每个实例只由一个 goroutine 使用。
同一进程内可以有多个互不相干的 `Reader`/`Writer` 并行读写不同文件。
`Writer` 不支持并发 `Write`；需要并行产出时按列或按文件拆分。

## 非目标

- 追加写、断点续写、多文件/数据集/分区目录扫描与合并。
- 谓词下推、行组过滤、自定义 schema 演进规则（缺列报错，但不做类型转换）。
- 值的自动类型转换（时间戳不还原成 `time.Time`，字符串不转数字）。
- 对象存储直读直写、Arrow 互操作、列加密。
