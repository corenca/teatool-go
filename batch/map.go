package batch

import (
	"context"
	"errors"
	"runtime"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"
)

// Result 保存单个任务的入参、返回值与错误。
//
// 结果切片与入参切片等长、下标对齐；即使任务未执行，Param 也已经被填好。
// 三态判断用 Success / Failed / Skipped，不要直接比较 Err：Failed 不包含未执行
// 的任务，误用容易把跳过的项计成失败。
type Result[T, R any] struct {
	// Param 是传入的本条参数，原样保留，便于把结果对回输入。
	Param T
	// Value 是 fn 的返回值；任务失败或未执行时为零值，不要依赖它。
	Value R
	// Err 是任务结果：nil 表示成功，ErrNotRun 表示未执行，其余为 fn 返回的错误
	// 或 *PanicError。
	Err error
}

// Success 报告任务是否执行且无错误。
//
// 未执行的任务不算成功；与 Failed、Skipped 三者互斥。
func (r Result[T, R]) Success() bool {
	return r.Err == nil
}

// Failed 报告任务是否执行并返回了错误，未执行的任务不算失败。
//
// 统计失败数用它，不要用 len(results)-成功数：后者会把被策略跳过的项算成失败。
func (r Result[T, R]) Failed() bool {
	return r.Err != nil && !r.Skipped()
}

// Skipped 报告任务是否因取消或失败策略而未执行。
//
// 等价于 errors.Is(r.Err, ErrNotRun)，覆盖"还没领到就停了"与"上游 ctx 已取消"
// 两种情况。
func (r Result[T, R]) Skipped() bool {
	return errors.Is(r.Err, ErrNotRun)
}

// Map 以不超过 config.Limit 的并发度执行 fn，返回长度与 params 相同、
// 下标一一对应的结果。
//
// 参数：
//   - ctx：整批任务的上下文，被取消后不再派发新任务；已开始的任务由 fn 自行
//     检查 ctx.Done() 退出。传 nil 会 panic，需使用 context.Background()。
//   - params：入参列表，元素原样传给 fn，也原样写入结果。空切片直接返回空结果。
//   - fn：任务函数，接收派生后的 context 与单个入参；返回值与错误写入对应结果。
//     fn 会被多个 goroutine 并发调用，必须自行保证共享状态的并发安全。
//   - config：并发上限、单任务超时与失败策略，零值可直接使用。
//
// 返回长度为 len(params) 的结果切片，下标与 params 一一对应；调用方可以直接
// 按顺序过滤、汇总，无需重新排序。
//
// 未执行的任务（被失败策略或 ctx 取消挡住）其 Err 为 ErrNotRun；fn 内的 panic
// 会被恢复为 *PanicError 并记录在该任务的结果里，不会向上抛出。
//
// 超时是协作式的：config.TaskTimeout 只给 fn 派生一个带 deadline 的 context，
// 本包不会中断 fn。fn 若不响应 ctx（例如直接调用 time.Sleep），会继续执行到
// 返回，结果按其返回值记录，不会出现超时错误。
func Map[T, R any](
	ctx context.Context,
	params []T,
	fn func(context.Context, T) (R, error),
	config Config,
) []Result[T, R] {
	results := make([]Result[T, R], len(params))
	if len(params) == 0 {
		return results
	}
	for i, param := range params {
		results[i] = Result[T, R]{Param: param, Err: ErrNotRun}
	}

	workers := config.Limit
	if workers <= 0 {
		workers = runtime.GOMAXPROCS(0)
	}
	if workers > len(params) {
		workers = len(params)
	}

	var cancel context.CancelFunc
	if config.Policy == CancelOnError {
		ctx, cancel = context.WithCancel(ctx)
		defer cancel()
	}

	var (
		next atomic.Int64
		stop atomic.Bool
		wg   sync.WaitGroup
	)

	for range workers {
		wg.Go(func() {
			for {
				if stop.Load() || ctx.Err() != nil {
					return
				}

				index := int(next.Add(1)) - 1
				if index >= len(params) {
					return
				}

				// 领到任务后再检查一次，避免失败或取消发生后才领取的任务继续执行。
				if stop.Load() || ctx.Err() != nil {
					return
				}

				results[index] = runTask(ctx, params[index], fn, config.TaskTimeout)

				if results[index].Err != nil && config.Policy.stopsScheduling() {
					stop.Store(true)
					if cancel != nil {
						cancel()
					}
				}
			}
		})
	}

	wg.Wait()
	return results
}

// runTask 执行单个任务，并把 panic 与超时转换成结果中的错误。
func runTask[T, R any](
	ctx context.Context,
	param T,
	fn func(context.Context, T) (R, error),
	timeout time.Duration,
) (result Result[T, R]) {
	result.Param = param

	taskCtx := ctx
	if timeout > 0 {
		var cancel context.CancelFunc
		taskCtx, cancel = context.WithTimeout(ctx, timeout)
		defer cancel()
	}

	defer func() {
		if recovered := recover(); recovered != nil {
			result.Err = &PanicError{Value: recovered, Stack: debug.Stack()}
		}
	}()

	result.Value, result.Err = fn(taskCtx, param)
	return result
}
