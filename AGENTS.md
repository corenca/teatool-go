# teatool-go

Go 工具库（module `github.com/corenca/teatool-go`，`go.mod` 要求 Go 1.27.2）。作者 Corenca。
核心包零第三方依赖；唯一的例外是 `storage/s3`（引入 aws-sdk-go-v2）。
无 CI、Makefile、linter 配置，不要假设它们存在。

## 项目现状

- 根目录 `teatool.go` 是 `package teatool` 占位（只有 `Hello()`），实际工具都在子包。
- `batch/`：带并发上限的批量任务执行器（`Map`）。
- `masking/`：字段与文本脱敏。类型（`Type`）+ 默认格式（`MaskFunc`）分离，
  内置 10 个业务类型，`Register` 可覆盖或新增；`Mask` 为 fail-closed（未知类型
  返回 `[REDACTED]`），`MaskText` 为 fail-open（未命中原样返回）。脱敏函数一律
  不返回 error：运行期无错误通道，只有 `Register` 这类装配期操作才返回 error。
- `datetime/`：布局常量、时区名与缓存、日/周/月/季边界、逐日序列、显式 `Parse`。
  日期统一用当日零点的 `time.Time` 表示（不定义 date 类型），边界计算始终在输入
  时区内进行；时间戳换算交由标准库（`time.Unix` 系列），不重复封装。解析只接受
  调用方给定的布局，不做格式猜测。
- `filesplit/`：大文件按行切分。`SplitByLines` 按每份行数、`SplitByParts` 按份数，
  `CountLines` 单独统计行数。整文件不读入内存，行内容与 `\n`/`\r\n` 原样拷贝；
  命名 `<原名>_part_<序号><扩展名>`，序号按总份数补零；返回绝对路径列表，
  中途失败清理已写出的子文件，空文件不产生子文件。
- `fsutil/`：业务文件系统工具。`Exists`/`IsFile`/`IsDir` 区分"不存在"与
  "无权限"；`EnsureParent` 为文件路径建父目录；`SanitizeFileName`、`JoinWithin`、
  `UniqueName` 处理上传文件名与目录穿越；`Copy`/`Move` 只处理单个普通文件、
  不覆盖、拒绝符号链接，Copy 精确保持源权限位；`ListFiles` 递归返回普通文件
  绝对路径并跳过符号链接。
- `collection/`：集合与序列工具。`Set` 无序、`OrderedSet` 保持首次插入顺序，
  都支持增删查、`Clone`、`Values` 与并/交/差/相等，非并发安全；`SplitBySize`
  按每份数量、`SplitByParts` 按份数（余数靠前，与 `filesplit` 口径一致），
  参数非法返回 error，子切片共享底层数组但容量裁剪；`Dedupe` 保序去重并返回
  独立切片。
- `storage/`：对象存储 SPI（19 个方法：判存、读写（含区间）、分片上传、磁盘上传下载、
  列举（含目录视图）、批量/前缀删除、复制移动、预签名），零第三方依赖；
  `storage/s3/` 是唯一实现，同一份代码覆盖 AWS S3 与 OSS、COS、OBS、MinIO。
  `storage/service.go` 提供单桶适配器 `Service`（`NewService(client, bucket)`），
  18 个桶内方法 + `Bucket()`，`Copy`/`Move` 收窄为桶内；跨桶操作仍用 `Client`。
  跨厂商固定动作：批量删除一律带 `Content-MD5`（OSS/COS 强制）；region 自动去
  `oss-` 前缀；metadata 键统一小写；`Put` 超过 `PartSize`（默认 8MiB）自动转
  multipart；预签名上传不约束 Content-Type（SDK 会剥离该头）。真实服务测试有两处
  配置来源：包内的 `storage/s3/s3_test.json`（模板 `s3_test.json.template` 入库）
  与 `docs/config/{s3,oss,cos,obs}.txt`；配置缺失或服务连不上时跳过对应用例。
- 对外 API 单独放一个文件（如 `masking/masking.go`），实现细节放其他文件，
  包内不导出。
- 后续工具按**能力**建包（如 `stream`、`pool`），不要再建 `concurrency` 这类领域大包。
  不要求与其他语言版本保持一致，遵循 Go 习惯。

## 常用命令

```bash
go test ./...                  # 全部测试（含 storage/s3 真实连接，约 30 秒）
go test -short ./...           # 跳过真实连接测试
go test ./batch -run TestMap   # 单个/一组测试
go test ./... -race -count=1   # 提交前：开竞态检测并禁用缓存
go vet ./...                   # 提交前
go fix ./...                   # 提交前：应用当前 Go 版本的现代化修复
gofmt -l .                     # 提交前检查；有输出则 gofmt -w .
```

`go test ./...` 约 1 秒；`map_test.go` 的 `TestTask` 是演示用例
（500ms 超时 / 1s 定时器，只打印无断言）。

## batch 包的既定语义（改动前先读）

- 固定 `min(Limit, len(params))` 个 worker，用原子下标领任务，没有任务队列；
  worker 用 `sync.WaitGroup.Go` 启动（Go 1.25+），不要改回 `wg.Add` + `go func(){ defer wg.Done() }`。
- 返回结果长度恒等于入参长度、下标一一对应；未执行的任务 `Err = ErrNotRun`，用 `errors.Is` 判断。
- `Result.Success/Failed/Skipped` 是互斥三态，`Failed` 不包含未执行；方法用值接收者，不要改成指针接收者。
- 失败策略：`ContinueOnError`（默认）、`StopOnError`、`CancelOnError`，细节见 `batch/batch.go`。
- 超时是**协作式**的：`TaskTimeout` 只派生带 deadline 的 ctx，不会中断任务。
  任务必须自己检查 `ctx.Done()` / `ctx.Err()`；不要改成硬超时或放弃 goroutine
  （会突破并发上限并泄漏）。
- 任务 panic 恢复为 `*PanicError`（含堆栈），按失败处理。
- `Limit <= 0` 回退到 `runtime.GOMAXPROCS(0)`。
- 非目标（已明确不做）：`Each`、`Stream`、可复用 `Pool`、重试、限速、硬超时。
- 参数命名用 `config`，不要用 `cfg`。

## 测试约定

- 测试放在外部测试包 `package batch_test`，通过 `batch.` 调用公开 API；不要改回 `package batch`。
- 采用 TDD：先写失败测试，再实现。
- 并发/取消相关测试必须能配合 `-race` 稳定通过，避免依赖过紧的时序断言。
- **真实服务测试配置**：每个后端包的测试读同目录的 `<pkg>_test.json`（如
  `storage/s3/s3_test.json`），只把 `<pkg>_test.json.template` 入库，真实文件
  由 `storage/.gitignore` 忽略。执行时先探测连接，文件缺失或连不上就 `t.Skip`；
  只要服务可用就必须走真实 API，不用假客户端替代。

## Git 约定

- Conventional Commits：`type(scope): description`，中文描述不超过 50 字。
- **不要自动提交**：完成改动后等 Corenca 明确要求；推送同理。
- 只 `git add` 本次相关的文件。

## 协作约定

- 中文交流，称呼 Corenca。
- 设计有不确定的地方先问，不要自行拍板。
