# filesplit 使用说明

按行切分大文件：把一份超大业务文件拆成多份，分发给多台机器或多个 goroutine 并行
处理。

适用场景：

- 亿级行的 CSV / 日志 / 导出文件，单机处理太慢，需要切成 N 份分发。
- 上传受单文件大小限制，需要按行数或份数拆分后分别入对象存储。
- 数据导入批次控制，每批固定行数。

关键特性：

- **整文件不进内存**，按 256KB 块流式读写，内存占用与文件大小无关。
- 行内容与行尾（`\n` / `\r\n`）**按字节原样拷贝**，不重排、不转码、不改编码。
- 中途失败会删除已写出的子文件，不留下"看起来成功了一半"的残缺集合。
- 返回子文件的**绝对路径**，按切分顺序排列。

不做的事情：

- 不解析 CSV 引号、JSON、多行记录——切分只认换行符。带引号内换行的 CSV 需
  先确认业务上允许在任意行边界切开。
- 不做压缩、加密、编码转换。
- 不保证线程安全地并发写同一输出目录：两次切分若落到同一目录且源文件同名，
  会互相覆盖。

```bash
go get github.com/corenca/teatool-go
```

## 快速上手

拿一份 5 行的 `data/users.csv`（前 4 行以 `\n` 结尾，最后一行没有换行符）：

```text
id,name
1,alice
2,bob
3,carol
4,dave
```

```go
package main

import (
	"fmt"

	"github.com/corenca/teatool-go/filesplit"
)

func main() {
	n, err := filesplit.CountLines("data/users.csv")
	if err != nil {
		return
	}
	fmt.Println("总行数:", n) // 5

	// 每份 2 行
	paths, err := filesplit.SplitByLines("data/users.csv", "out/by-lines", 2)
	if err != nil {
		return
	}
	for _, p := range paths {
		fmt.Println(p)
	}
}
```

输出：

```text
总行数: 5
/tmp/demo/out/by-lines/users_part_1.csv
/tmp/demo/out/by-lines/users_part_2.csv
/tmp/demo/out/by-lines/users_part_3.csv
```

三份内容分别是 `"id,name\n1,alice\n"`、`"2,bob\n3,carol\n"`、`"4,dave"`——行尾
原样保留，最后一份没有补齐换行符。

## 两个入口

| 入口 | 参数 | 切分规则 | 典型用途 |
| --- | --- | --- | --- |
| `SplitByLines(src, dstDir, linesPerFile)` | 每份**行数** | 除最后一份外每份恰好 `linesPerFile` 行 | 固定批量大小、控制单批内存 |
| `SplitByParts(src, dstDir, parts)` | 期望**份数** | 余数从靠前的份开始各多 1 行，任意两份相差不超过 1 | 按机器数、并发数均分 |

```go
// 均分 3 份：5 行 → 2 + 2 + 1
paths, err := filesplit.SplitByParts("data/users.csv", "out/by-parts", 3)
```

`SplitByParts` 的余数分配与 `collection.SplitByParts` 口径一致：靠前的份多拿。
`parts` 大于总行数时只产出总行数个非空子文件，**不会生成空文件**，因此返回的路径
可能少于 `parts` 个。

`CountLines` 用于事先估算份数：

```go
total, err := filesplit.CountLines(src)
if err != nil {
	return err
}
parts := int((total + batchSize - 1) / batchSize) // 向上取整
```

## 行语义

这里是最容易踩坑的地方，口径与 `wc -l` 不同：

| 输入内容 | `CountLines` | 说明 |
| --- | --- | --- |
| `"a\nb\nc\n"` | 3 | 常规情况 |
| `"a\nb\nc"` | 3 | 末尾无换行，最后一段仍计一行 |
| `""` | 0 | 空文件 |
| `"\n"` | 1 | 一个空行 |
| `"a\r\nb\r\n"` | 2 | `\r\n` 同样按字节原样保留 |

`wc -l` 只数换行符，所以对 `"a\nb\nc"` 会输出 2。按行切分必须把最后一段算作一行，
否则会丢数据。

## 命名与输出

- 文件名：`<原名主干>_part_<序号><扩展名>`，例如 `users_part_1.csv`。
- 序号从 1 开始，补零到总份数的位数：3 份是 `_part_1`，12 份是 `_part_01`。
  这样 `ls` 的字典序与切分顺序一致。
- 点开头的文件（`.env`）视为无扩展名，输出 `.env_part_1`。
- `dstDir` 不存在时自动创建（权限 0755），会递归创建父目录。
- 子文件权限取系统默认（通常 0644），**不继承源文件权限位**。
- 目标路径已存在同名文件时直接覆盖，不报错、不备份。

## 参数与错误

| API | 参数 | 返回 / 错误 |
| --- | --- | --- |
| `CountLines(path)` | 源文件路径，只读 | 行数；打开/读取失败返回底层错误 |
| `SplitByLines(src, dstDir, linesPerFile)` | `linesPerFile >= 1`，`dstDir` 非空 | 绝对路径列表；参数非法、读写失败返回错误 |
| `SplitByParts(src, dstDir, parts)` | `parts >= 1`，`dstDir` 非空 | 绝对路径列表；同上 |

错误情况：

- `linesPerFile < 1`、`parts < 1`、`dstDir == ""`：参数错误，源文件不会被读取。
- 源文件不存在或无权限：返回 `*os.PathError`，可用 `errors.Is(err, os.ErrNotExist)` 判断。
- 目标目录不可写：返回 `*os.PathError`。
- 切分期间源文件被截断（如日志轮转）：返回包装后的 `io.ErrUnexpectedEOF`。
- 除参数错误外的失败都会尽力删除本次已写出的子文件；已存在且被覆盖的同名文件
  不会恢复。

## 典型用法：分发后并行处理

```go
func main() {
	const batchSize = 500_000

	paths, err := filesplit.SplitByLines("data/big.csv", "out/batches", batchSize)
	if err != nil {
		return
	}

	// 交给 batch.Map 并行处理，每个子文件是一个任务
	results := batch.Map(context.Background(), paths,
		func(ctx context.Context, path string) (int64, error) {
			return importCSV(ctx, path)
		},
		batch.Config{Limit: 4},
	)
	for _, r := range results {
		if r.Failed() {
			log.Printf("%s 失败: %v", r.Param, r.Err)
		}
	}
}
```

切分本身是单线程顺序读写的，瓶颈通常在磁盘；并行处理阶段才需要控制并发。

## 常见错误

**行数比 `wc -l` 多 1**：文件末尾没有换行符时，本包把最后一段计为一行，`wc -l`
不数。这是刻意行为，否则会丢最后一行。

**切出来比预期少一份**：`SplitByParts` 的 `parts` 大于总行数时只产出非空份；也可能
源文件末尾多了一个空行，被算进总行数。

**子文件权限不对**：子文件用 `os.Create` 创建，权限是系统默认值。需要沿用源文件权限
时，切分后自行 `os.Chmod`。

**想按字节数切分**：本包只按行切分。按字节切会把一行劈成两半，需要字节切分请
直接操作 `io.Reader`（如 `storage` 的分片上传）。

**并发跑到同一个输出目录**：同名源文件会产生同名子文件而互相覆盖。每个任务用独立
子目录，或切分完再并发处理。

**中文/emoji 被切坏**：不会。切分只认 `\n` 字节，不解析 UTF-8，只要行边界本身合法，
多字节字符不会被拆开。

**切分后内存还是涨**：读缓冲固定 256KB，与文件大小无关。内存上涨通常来自后续处理
阶段把子文件整份读入。
