// Package batch 提供带并发上限的批量任务执行能力。
//
// Map 是本包唯一的执行入口：对一批入参并发调用同一个函数，并发度由 Config.Limit
// 控制，返回值与入参等长且下标一一对应。适合"打一批接口、处理一批文件、批量写库"
// 这类既有重复劳动又需要限流的场景。
//
// 既定语义：
//
//   - 固定 min(Limit, len(params)) 个 worker 用原子下标领任务，没有任务队列；
//     每个任务只执行一次，结果写回自己的下标。
//   - 返回长度恒等于入参长度。未执行的任务其 Err 为 ErrNotRun，用
//     errors.Is(err, batch.ErrNotRun) 判断，不要比较错误字符串。
//   - Result 的 Success/Failed/Skipped 是互斥三态：成功、执行了但失败、未执行。
//   - 单个任务的 panic 会被恢复为 *PanicError（含堆栈），按失败处理，不会拖垮
//     整批任务。
//   - 超时是协作式的：TaskTimeout 只派生带 deadline 的 context，不会中断 fn。
//
// 不提供的功能：Each/Stream 流式版本、可复用的任务池、自动重试、限速、硬超时。
// 需要这些能力时在 fn 内部实现，或在上层再包一层。
package batch

import (
	"errors"
	"fmt"
	"time"
)

// ErrNotRun 标记任务因失败策略或上下文取消而未执行。
//
// 结果切片初始时全部置为该错误，Map 只覆盖真正执行到的下标，因此未执行的项
// 始终保持原值。用 errors.Is(err, batch.ErrNotRun) 判断，不要比较错误字符串。
var ErrNotRun = errors.New("batch: task not run")

// ErrorPolicy 指定单个任务失败后对整批任务的影响。
//
// 零值为 ContinueOnError。策略只影响"后续是否继续"，已经领到的任务不会被强行
// 中止（CancelOnError 除外，它会取消传给任务的 context，由任务自己响应）。
type ErrorPolicy int

const (
	// ContinueOnError 表示失败互不影响，其余任务照常执行。
	//
	// 这是零值，也是默认策略：适合批量处理互不依赖的条目，最后统一看失败列表。
	ContinueOnError ErrorPolicy = iota
	// StopOnError 表示失败后不再派发新任务，已开始的任务执行完成。
	//
	// 已开始的任务不会被取消，它们的 context 仍然是原来那个；适合"前置条件失败
	// 后继续做也没意义"的场景。剩余任务的结果为 ErrNotRun。
	StopOnError
	// CancelOnError 表示失败后停止派发，并取消正在执行的任务。
	//
	// 取消以 context 方式传递：fn 必须检查 ctx.Done() / ctx.Err() 才能及时退出。
	// 不响应 ctx 的任务会继续执行到返回，其错误按 fn 的返回值记录。
	// 未派发的任务结果为 ErrNotRun，正在执行的任务可能得到 fn 返回的
	// context.Canceled。
	CancelOnError
)

// stopsScheduling 报告策略是否要求失败后停止派发新任务。
func (p ErrorPolicy) stopsScheduling() bool {
	return p == StopOnError || p == CancelOnError
}

// Config 配置一次批量执行的行为，零值可直接使用。
//
// 零值含义：并发度取 runtime.GOMAXPROCS(0)，不限时，失败互不影响。
type Config struct {
	// Limit 是同时执行的最大任务数。
	//
	// 小于等于 0 时取 runtime.GOMAXPROCS(0)；大于 len(params) 时按
	// len(params) 处理，不会启动多余的空 worker。
	Limit int
	// TaskTimeout 是单个任务的超时时间，小于等于 0 表示不限制。
	//
	// 超时以 context 协作方式传递，本包不会强制中断任务：fn 必须在阻塞操作
	// 或长循环中检查 ctx.Done() / ctx.Err() 并尽快返回，否则任务会一直执行到
	// 返回，且不会得到超时错误。计时从 fn 开始执行时开始，领任务之前的排队
	// 时间不计入。
	TaskTimeout time.Duration
	// Policy 指定任务失败后的处理方式，零值为 ContinueOnError。
	Policy ErrorPolicy
}

// PanicError 描述任务执行中发生的 panic。
//
// fn 内部的 panic 会被 recover 成该类型并写入对应结果，整批任务继续执行；
// 错误信息包含 panic 值与完整堆栈，可直接落日志。
type PanicError struct {
	// Value 是 recover 得到的原始 panic 值，类型不确定。
	Value any
	// Stack 是 panic 发生时的调用栈（debug.Stack），已经包含 goroutine 信息。
	Stack []byte
}

// Error 实现 error 接口，输出 panic 值与堆栈。
//
// 返回值是多行文本，适合直接落日志，不建议用来做告警聚合的键。
func (e *PanicError) Error() string {
	return fmt.Sprintf("batch: task panicked: %v\n%s", e.Value, e.Stack)
}

// Unwrap 返回原始 panic 值中的 error，便于用 errors.Is 与 errors.As 判断。
//
// panic 值不是 error（例如字符串或 int）时返回 nil，此时只能先用 errors.As
// 取出 *PanicError，再自行断言 Value。
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}
