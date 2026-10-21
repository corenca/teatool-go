# teatool-go

Go 工具库（module `github.com/corenca/teatool-go`，`go.mod` 要求 Go 1.27.2）。作者 Corenca。
零第三方依赖，无 CI、Makefile、linter 配置，不要假设它们存在。

## 项目现状

- 根目录 `teatool.go` 是 `package teatool` 占位（只有 `Hello()`），实际工具都在子包。
- `batch/` 是当前唯一能力包：带并发上限的批量任务执行器（`Map`）。
- 后续工具按**能力**建包（如 `stream`、`pool`），不要再建 `concurrency` 这类领域大包。
  不要求与其他语言版本保持一致，遵循 Go 习惯。

## 常用命令

```bash
go test ./...                  # 全部测试
go test ./batch -run TestMap   # 单个/一组测试
go test ./... -race -count=1   # 提交前：开竞态检测并禁用缓存
go vet ./...                   # 提交前
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

## Git 约定

- Conventional Commits：`type(scope): description`，中文描述不超过 50 字。
- **不要自动提交**：完成改动后等 Corenca 明确要求；推送同理。
- 只 `git add` 本次相关的文件。

## 协作约定

- 中文交流，称呼 Corenca。
- 设计有不确定的地方先问，不要自行拍板。
