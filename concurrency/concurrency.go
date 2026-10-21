// Package concurrency 提供带并发上限的批量任务执行能力。
package concurrency

import (
	"errors"
	"fmt"
	"time"
)

// ErrNotRun 标记任务因失败策略或上下文取消而未执行。
var ErrNotRun = errors.New("concurrency: task not run")

// ErrorPolicy 指定单个任务失败后对整批任务的影响。
type ErrorPolicy int

const (
	// ContinueOnError 表示失败互不影响，其余任务照常执行。
	ContinueOnError ErrorPolicy = iota
	// StopOnError 表示失败后不再派发新任务，已开始的任务执行完成。
	StopOnError
	// CancelOnError 表示失败后停止派发，并取消正在执行的任务。
	CancelOnError
)

// stopsScheduling 报告策略是否要求失败后停止派发新任务。
func (p ErrorPolicy) stopsScheduling() bool {
	return p == StopOnError || p == CancelOnError
}

// Config 配置一次批量执行的行为，零值可直接使用。
type Config struct {
	// Limit 是同时执行的最大任务数；小于等于 0 时取 runtime.GOMAXPROCS(0)。
	Limit int
	// TaskTimeout 是单个任务的超时时间；小于等于 0 表示不限制。
	// 超时以 context 协作方式传递，本包不会强制中断任务：fn 必须在阻
	// 塞操作或长循环中检查 ctx.Done() / ctx.Err() 并尽快返回，否则任务
	// 会一直执行到返回，且不会得到超时错误。
	TaskTimeout time.Duration
	// Policy 指定任务失败后的处理方式，零值为 ContinueOnError。
	Policy ErrorPolicy
}

// PanicError 描述任务执行中发生的 panic。
type PanicError struct {
	// Value 是 recover 得到的原始 panic 值。
	Value any
	// Stack 是 panic 发生时的调用栈。
	Stack []byte
}

// Error 实现 error 接口。
func (e *PanicError) Error() string {
	return fmt.Sprintf("concurrency: task panicked: %v\n%s", e.Value, e.Stack)
}

// Unwrap 返回原始 panic 值中的 error，便于用 errors.Is 与 errors.As 判断。
func (e *PanicError) Unwrap() error {
	err, _ := e.Value.(error)
	return err
}
