package concurrency

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
type Result[T, R any] struct {
	Param T
	Value R
	Err   error
}

// Success 报告任务是否执行且无错误。
func (r Result[T, R]) Success() bool {
	return r.Err == nil
}

// Failed 报告任务是否执行并返回了错误，未执行的任务不算失败。
func (r Result[T, R]) Failed() bool {
	return r.Err != nil && !r.Skipped()
}

// Skipped 报告任务是否因取消或失败策略而未执行。
func (r Result[T, R]) Skipped() bool {
	return errors.Is(r.Err, ErrNotRun)
}

// Map 以不超过 config.Limit 的并发度执行 fn，返回长度与 params 相同、
// 下标一一对应的结果。
//
// 未执行的任务（被失败策略或 ctx 取消挡住）其 Err 为 ErrNotRun；fn 内
// 的 panic 会被恢复为 *PanicError 并记录在该任务的结果里。
//
// 超时是协作式的：config.TaskTimeout 只给 fn 派生一个带 deadline 的
// context，本包不会中断 fn。fn 若不响应 ctx（例如直接调用 time.Sleep），
// 会继续执行到返回，结果按其返回值记录，不会出现超时错误。
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
