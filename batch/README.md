# batch 使用说明

带并发上限的批量任务执行器。对一批入参并发调用同一个函数，并发度可控，返回值与
入参等长且下标一一对应。

适用场景：

- 打一批接口、拉一批用户、处理一批文件，既要并发提速又不想打爆下游。
- 批量写库或调外部服务，需要限制同时进行的请求数。
- 批处理里某条失败不能拖垮整批，最后统一汇总失败项。

不做的事情：

- 不提供流式接口（`Each` / `Stream`），所有结果一次性返回。
- 不提供可复用的 worker 池，每次调用 `Map` 自带一组 worker。
- 不做自动重试、限速、优先级、硬超时。
- 不保证任务开始顺序，只保证结果下标与入参一致。

```bash
go get github.com/corenca/teatool-go
```

## 快速上手

```go
package main

import (
	"context"
	"fmt"

	"github.com/corenca/teatool-go/batch"
)

func main() {
	ids := []int{1, 2, 3, 4, 5, 6}

	results := batch.Map(context.Background(), ids,
		func(ctx context.Context, id int) (string, error) {
			if id == 3 {
				return "", fmt.Errorf("用户 %d 不存在", id)
			}
			return fmt.Sprintf("用户%d的详情", id), nil
		},
		batch.Config{Limit: 3}, // 最多 3 个任务同时执行
	)

	for _, r := range results {
		switch {
		case r.Success():
			fmt.Printf("%d 成功: %s\n", r.Param, r.Value)
		case r.Skipped():
			fmt.Printf("%d 未执行\n", r.Param)
		default:
			fmt.Printf("%d 失败: %v\n", r.Param, r.Err)
		}
	}
}
```

输出（顺序固定，与入参一致；并发完成顺序不影响结果顺序）：

```text
1 成功: 用户1的详情
2 成功: 用户2的详情
3 失败: 用户 3 不存在
4 成功: 用户4的详情
5 成功: 用户5的详情
6 成功: 用户6的详情
```

## 参数

| 参数 | 含义 |
| --- | --- |
| `ctx` | 整批任务的上下文。取消后不再派发新任务；已开始的任务由 `fn` 自己检查 `ctx.Done()` 退出。传 `nil` 会 panic |
| `params` | 入参列表。元素原样传给 `fn`，也原样写回结果的 `Param`。空切片直接返回空结果 |
| `fn` | 任务函数，签名 `func(context.Context, T) (R, error)`。会被多个 goroutine 并发调用 |
| `config` | 并发上限、单任务超时与失败策略，零值可直接使用 |

`Config` 三个字段：

| 字段 | 零值 | 含义 |
| --- | --- | --- |
| `Limit` | `GOMAXPROCS(0)` | 同时执行的最大任务数。小于等于 0 时取 `runtime.GOMAXPROCS(0)`；大于 `len(params)` 时按 `len(params)` 处理 |
| `TaskTimeout` | 不限制 | 单个任务的超时。**协作式**：只派生带 deadline 的 context，不会中断 `fn`；从 `fn` 开始执行时计时，排队时间不计入 |
| `Policy` | `ContinueOnError` | 单个任务失败后的处理方式，见下节 |

并发上限的准确含义是"同时执行的 `fn` 不超过 `Limit` 个"。`Map` 启动固定
`min(Limit, len(params))` 个 worker 用原子下标领任务，没有任务队列，每个任务只执行
一次。因此 `Limit` 不是"每秒请求数"，需要限速请在 `fn` 里自行加限流器。

## 结果三态

`Result` 有 `Param`、`Value`、`Err` 三个字段，用三个方法区分状态，三者互斥：

| 方法 | 成立条件 | 含义 |
| --- | --- | --- |
| `Success()` | `Err == nil` | 执行了且没有错误 |
| `Failed()` | `Err != nil` 且不是 `ErrNotRun` | 执行了但失败（含 panic 恢复） |
| `Skipped()` | `errors.Is(Err, ErrNotRun)` | 因失败策略或 `ctx` 取消而未执行 |

统计时用这三个方法，不要用 `len(results) - 成功数` 计算失败数——那会把跳过的项
算成失败：

```go
var ok, failed, skipped int
for _, r := range results {
	switch {
	case r.Success():
		ok++
	case r.Failed():
		failed++
	case r.Skipped():
		skipped++
	}
}
fmt.Printf("成功 %d / 失败 %d / 未执行 %d\n", ok, failed, skipped)
```

未执行的任务 `Param` 已填好、`Value` 是零值，所以仍能知道有哪些没跑。

## 失败策略

| 策略 | 失败后行为 | 适用场景 |
| --- | --- | --- |
| `ContinueOnError`（零值） | 其余任务照常执行 | 条目之间互不依赖，最后统一看失败列表 |
| `StopOnError` | 不再派发新任务；已开始的任务执行完成 | 前置条件失败后继续做没有意义 |
| `CancelOnError` | 不再派发，并取消传给任务的 context | 希望尽快收手，且 `fn` 会响应取消 |

把上例的 `Limit` 改成 1、加上 `Policy: batch.StopOnError`，第 3 条失败后剩下的
条目就不会再派发：

```go
batch.Config{Limit: 1, Policy: batch.StopOnError}
```

```text
1 成功: 用户1的详情
2 成功: 用户2的详情
3 失败: 用户 3 不存在
4 未执行
5 未执行
6 未执行
成功 2 / 失败 1 / 未执行 3
```

并发下"未执行的数量"不确定：多个 worker 可能已经领了任务，或失败消息到达时其他
任务正在执行。策略保证的是"失败观察点之后不再领新任务"，不保证具体跳过多少条。
需要确定性行为时把 `Limit` 设为 1。

## 超时是协作式的

`TaskTimeout` 只给 `fn` 的 context 加 deadline，不会杀 goroutine——硬中断会突破
并发上限并泄漏状态。所以 `fn` 必须自己响应（片段，`client` 需自行创建）：

```go
results := batch.Map(ctx, urls,
	func(ctx context.Context, url string) (int, error) {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
		if err != nil {
			return 0, err
		}
		resp, err := client.Do(req) // 由 context 控制超时
		if err != nil {
			return 0, err
		}
		defer resp.Body.Close()
		return resp.StatusCode, nil
	},
	batch.Config{Limit: 5, TaskTimeout: 2 * time.Second},
)
```

超时任务的 `Err` 是 `fn` 返回的错误（通常是 `context.DeadlineExceeded`）。如果 `fn`
不检查 `ctx`，例如直接 `time.Sleep(10 * time.Second)`，任务会跑满 10 秒，结果按
返回值记录，**不会**得到超时错误。

## panic 恢复

`fn` 里的 panic 会被恢复为 `*PanicError` 写进该条结果，整批任务继续执行：

```go
var panicErr *batch.PanicError
if errors.As(r.Err, &panicErr) {
	fmt.Println("panic 值:", panicErr.Value)
	fmt.Println("堆栈:", string(panicErr.Stack))
}
```

`PanicError.Unwrap` 返回 panic 值中的 error，因此 `errors.Is` 也能穿透到业务错误。

## 并发与安全

- `fn` 会被多个 goroutine 并发调用，共享的 map、切片、计数器必须自己加锁或用原子
  操作。需要串行写入时用 `sync.Mutex`，不要假设"按入参顺序执行"。
- `Limit` 限制的是**同时执行**的 `fn` 数量，不是累计次数；一批 1000 条、`Limit=10`
  时也只会同时存在 10 个 `fn` 调用。
- `Map` 会等全部已派发的任务返回后才返回，不会提前返回。中途放弃请取消 `ctx`，
  并确保 `fn` 响应取消，否则 `Map` 仍会被慢任务拖住。
- `TaskTimeout` 与 `CancelOnError` 都依赖 `fn` 响应 `ctx`；`Map` 传 `nil` ctx 会 panic。

## 常见错误

**把 `ErrNotRun` 算成失败**：用 `r.Failed()`，它已经把未执行的项排除在外；或直接
`errors.Is(r.Err, batch.ErrNotRun)` 判断。

**以为超时会中断任务**：`TaskTimeout` 不会。`fn` 不检查 `ctx` 时慢任务照跑，超时
只体现在你返回的错误里。

**`fn` 里并发写 map 导致 `concurrent map writes`**：`Map` 不串行化 `fn`，写入共享
结构必须加锁。

**结果顺序预期错误**：结果切片与 `params` 下标一一对应，但任务完成顺序不可预期。
需要按完成顺序处理时，自己加 channel 或时间戳。

**服务端被打爆**：`Limit` 默认是 `GOMAXPROCS`，对 HTTP 下游通常太大，按接口承压
能力显式设置（如 5~20）。

**忘记 `ctx` 已取消**：`Map` 不再派发，但已开始的任务只有 `fn` 检查 `ctx` 才会退出。
