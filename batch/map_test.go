package batch_test

import (
	"context"
	"errors"
	"fmt"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/corenca/teatool-go/batch"
)

var errBoom = errors.New("boom")

// trackPeak 记录并发峰值，供并发上限相关的测试使用。
func trackPeak(current, peak *atomic.Int32) {
	n := current.Add(1)
	for {
		p := peak.Load()
		if n <= p || peak.CompareAndSwap(p, n) {
			break
		}
	}
}

// TestMapPreservesOrderAndValues 验证结果与入参等长、下标对齐并保留参数。
func TestMapPreservesOrderAndValues(t *testing.T) {
	params := []int{0, 1, 2, 3, 4}

	results := batch.Map(context.Background(), params,
		func(_ context.Context, param int) (int, error) {
			return param * 10, nil
		},
		batch.Config{Limit: 2},
	)

	if len(results) != len(params) {
		t.Fatalf("结果长度 = %d, 期望 %d", len(results), len(params))
	}
	for i, result := range results {
		if result.Param != params[i] {
			t.Errorf("results[%d].Param = %d, 期望 %d", i, result.Param, params[i])
		}
		if result.Value != params[i]*10 {
			t.Errorf("results[%d].Value = %d, 期望 %d", i, result.Value, params[i]*10)
		}
		if !result.Success() {
			t.Errorf("results[%d] 应为成功, Err = %v", i, result.Err)
		}
		t.Logf("idx = %d, result = %v", i, result)
	}
	// t.Errorf("%v", results)
}

// TestMapLimitsConcurrency 验证同时执行的任务数不超过 Limit 且能跑满。
func TestMapLimitsConcurrency(t *testing.T) {
	const total, limit = 24, 3
	var current, peak atomic.Int32

	results := batch.Map(context.Background(), make([]int, total),
		func(_ context.Context, _ int) (struct{}, error) {
			trackPeak(&current, &peak)
			time.Sleep(5 * time.Millisecond)
			current.Add(-1)
			return struct{}{}, nil
		},
		batch.Config{Limit: limit},
	)

	if len(results) != total {
		t.Fatalf("结果长度 = %d, 期望 %d", len(results), total)
	}
	if got := peak.Load(); got > limit {
		t.Errorf("并发峰值 = %d, 超过上限 %d", got, limit)
	}
	if got := peak.Load(); got < limit {
		t.Errorf("并发峰值 = %d, 未跑满上限 %d", got, limit)
	}
}

// TestMapNonPositiveLimitFallsBackToGOMAXPROCS 验证非正数 Limit 回退到 GOMAXPROCS。
func TestMapNonPositiveLimitFallsBackToGOMAXPROCS(t *testing.T) {
	var current, peak atomic.Int32

	results := batch.Map(context.Background(), make([]int, 4),
		func(_ context.Context, _ int) (struct{}, error) {
			trackPeak(&current, &peak)
			time.Sleep(5 * time.Millisecond)
			current.Add(-1)
			return struct{}{}, nil
		},
		batch.Config{Limit: -1},
	)

	for i, result := range results {
		if !result.Success() {
			t.Errorf("results[%d] 应为成功, Err = %v", i, result.Err)
		}
	}
	if got, max := peak.Load(), int32(runtime.GOMAXPROCS(0)); got > max {
		t.Errorf("并发峰值 = %d, 超过 GOMAXPROCS %d", got, max)
	}
}

// TestMapEmptyInput 验证空入参返回空结果。
func TestMapEmptyInput(t *testing.T) {
	results := batch.Map(context.Background(), nil,
		func(_ context.Context, param int) (int, error) { return param, nil },
		batch.Config{},
	)

	if len(results) != 0 {
		t.Fatalf("结果长度 = %d, 期望 0", len(results))
	}
}

// TestMapContinueOnErrorRunsAll 验证默认策略下失败互不影响。
func TestMapContinueOnErrorRunsAll(t *testing.T) {
	var executed atomic.Int32

	results := batch.Map(context.Background(), []int{0, 1, 2},
		func(_ context.Context, param int) (int, error) {
			executed.Add(1)
			if param == 1 {
				return 0, errBoom
			}
			return param, nil
		},
		batch.Config{Limit: 2},
	)

	if got := executed.Load(); got != 3 {
		t.Fatalf("执行任务数 = %d, 期望 3", got)
	}
	if !results[0].Success() || !results[2].Success() {
		t.Errorf("0、2 号任务应成功")
	}
	if !results[1].Failed() || !errors.Is(results[1].Err, errBoom) {
		t.Errorf("1 号任务应失败并返回 errBoom, Err = %v", results[1].Err)
	}
	for i, result := range results {
		if result.Skipped() {
			t.Errorf("results[%d] 不应被跳过", i)
		}
	}
}

// TestMapStopOnErrorSkipsRemaining 验证失败后不再派发新任务。
func TestMapStopOnErrorSkipsRemaining(t *testing.T) {
	var executed atomic.Int32

	results := batch.Map(context.Background(), []int{0, 1, 2, 3},
		func(_ context.Context, param int) (int, error) {
			executed.Add(1)
			if param == 0 {
				return 0, errBoom
			}
			return param, nil
		},
		batch.Config{Limit: 1, Policy: batch.StopOnError},
	)

	if got := executed.Load(); got != 1 {
		t.Errorf("执行任务数 = %d, 期望 1", got)
	}
	if !results[0].Failed() {
		t.Errorf("0 号任务应失败, Err = %v", results[0].Err)
	}
	for i := 1; i < len(results); i++ {
		if !results[i].Skipped() {
			t.Errorf("results[%d] 应被跳过, Err = %v", i, results[i].Err)
		}
		if results[i].Param != i {
			t.Errorf("results[%d].Param = %d, 期望 %d", i, results[i].Param, i)
		}
	}
}

// TestMapCancelOnErrorCancelsRunning 验证失败后取消正在执行的任务。
func TestMapCancelOnErrorCancelsRunning(t *testing.T) {
	started := make(chan struct{})

	results := batch.Map(context.Background(), []int{0, 1, 2, 3},
		func(ctx context.Context, param int) (int, error) {
			switch param {
			case 0:
				// 等到 1 号任务确认开始后再失败，确保存在在跑任务。
				<-started
				return 0, errBoom
			case 1:
				close(started)
				<-ctx.Done()
				return 0, ctx.Err()
			default:
				return param, nil
			}
		},
		batch.Config{Limit: 2, Policy: batch.CancelOnError},
	)

	if !results[0].Failed() || !errors.Is(results[0].Err, errBoom) {
		t.Errorf("0 号任务应失败并返回 errBoom, Err = %v", results[0].Err)
	}
	if !results[1].Failed() || !errors.Is(results[1].Err, context.Canceled) {
		t.Errorf("1 号任务应被取消, Err = %v", results[1].Err)
	}
	for i := 2; i < len(results); i++ {
		if !results[i].Skipped() {
			t.Errorf("results[%d] 应被跳过, Err = %v", i, results[i].Err)
		}
	}
}

// TestMapRecoversPanic 验证任务 panic 被转换为 PanicError。
func TestMapRecoversPanic(t *testing.T) {
	results := batch.Map(context.Background(), []int{0, 1},
		func(_ context.Context, param int) (int, error) {
			if param == 0 {
				panic(errBoom)
			}
			return param, nil
		},
		batch.Config{Limit: 2},
	)

	var panicErr *batch.PanicError
	if !errors.As(results[0].Err, &panicErr) {
		t.Fatalf("0 号任务应返回 *batch.PanicError, Err = %v", results[0].Err)
	}
	if len(panicErr.Stack) == 0 {
		t.Errorf("PanicError.Stack 不应为空")
	}
	if !errors.Is(results[0].Err, errBoom) {
		t.Errorf("PanicError 应可通过 Unwrap 匹配原始 error")
	}
	if !results[0].Failed() {
		t.Errorf("panic 的任务应判定为失败")
	}
	if !results[1].Success() {
		t.Errorf("1 号任务应成功, Err = %v", results[1].Err)
	}
}

// TestMapTaskTimeout 验证超时通过 ctx 传递给任务。
func TestMapTaskTimeout(t *testing.T) {
	results := batch.Map(context.Background(), []int{0},
		func(ctx context.Context, _ int) (int, error) {
			<-ctx.Done()
			return 0, ctx.Err()
		},
		batch.Config{Limit: 1, TaskTimeout: 20 * time.Millisecond},
	)

	if !errors.Is(results[0].Err, context.DeadlineExceeded) {
		t.Fatalf("期望 DeadlineExceeded, Err = %v", results[0].Err)
	}
}

// TestMapTimeoutDoesNotInterruptUncooperativeTask 验证超时不强杀忽略 ctx 的任务。
func TestMapTimeoutDoesNotInterruptUncooperativeTask(t *testing.T) {
	results := batch.Map(context.Background(), []int{0},
		func(_ context.Context, _ int) (string, error) {
			time.Sleep(30 * time.Millisecond)
			return "done", nil
		},
		batch.Config{Limit: 1, TaskTimeout: 10 * time.Millisecond},
	)

	if !results[0].Success() || results[0].Value != "done" {
		t.Fatalf("未响应 ctx 的任务应正常完成, Value = %q, Err = %v", results[0].Value, results[0].Err)
	}
}

// TestMapCanceledContextSkipsAll 验证 ctx 已取消时不执行任何任务。
func TestMapCanceledContextSkipsAll(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	results := batch.Map(ctx, []int{0, 1, 2},
		func(_ context.Context, param int) (int, error) { return param, nil },
		batch.Config{Limit: 2},
	)

	for i, result := range results {
		if !result.Skipped() {
			t.Errorf("results[%d] 应被跳过, Err = %v", i, result.Err)
		}
		if result.Param != i {
			t.Errorf("results[%d].Param = %d, 期望 %d", i, result.Param, i)
		}
	}
}

// TestMapCancelDuringRun 验证运行中取消后已开始的记录取消、未开始的跳过。
func TestMapCancelDuringRun(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	started := make(chan struct{})

	done := make(chan []batch.Result[int, int], 1)
	go func() {
		done <- batch.Map(ctx, []int{0, 1, 2},
			func(ctx context.Context, param int) (int, error) {
				if param == 0 {
					close(started)
					<-ctx.Done()
					return 0, ctx.Err()
				}
				return param, nil
			},
			batch.Config{Limit: 1},
		)
	}()

	<-started
	cancel()
	results := <-done

	if !results[0].Failed() || !errors.Is(results[0].Err, context.Canceled) {
		t.Errorf("0 号任务应记录取消, Err = %v", results[0].Err)
	}
	for i := 1; i < len(results); i++ {
		if !results[i].Skipped() {
			t.Errorf("results[%d] 应被跳过, Err = %v", i, results[i].Err)
		}
	}
}

// TestResultPredicates 验证 Success、Failed、Skipped 三种互斥状态。
func TestResultPredicates(t *testing.T) {
	tests := []struct {
		name                     string
		err                      error
		success, failed, skipped bool
	}{
		{name: "成功", err: nil, success: true},
		{name: "失败", err: errBoom, failed: true},
		{name: "未执行", err: batch.ErrNotRun, skipped: true},
		{name: "包装的未执行", err: fmt.Errorf("wrapped: %w", batch.ErrNotRun), skipped: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := batch.Result[int, int]{Param: 1, Err: tt.err}
			if got := result.Success(); got != tt.success {
				t.Errorf("Success() = %v, 期望 %v", got, tt.success)
			}
			if got := result.Failed(); got != tt.failed {
				t.Errorf("Failed() = %v, 期望 %v", got, tt.failed)
			}
			if got := result.Skipped(); got != tt.skipped {
				t.Errorf("Skipped() = %v, 期望 %v", got, tt.skipped)
			}
		})
	}
}

type User struct {
	Name string
	Age  int
}

func (u User) String() string {
	return fmt.Sprintf("User{Name: %s, Age: %d}", u.Name, u.Age)
}

func TestTask(t *testing.T) {
	users := []User{
		User{Name: "Tom", Age: 11},
		User{Name: "Lucy", Age: 22},
		User{Name: "Alex", Age: 78},
		User{Name: "lily", Age: 2},
		User{Name: "Jack", Age: 18},
		User{Name: "Corenca", Age: 45},
		User{Name: "李华", Age: 65},
	}

	ctx := context.Background()

	results := batch.Map(ctx, users, func(ctx context.Context, user User) (int, error) {
		fmt.Printf("开始执行任务，参数: %s\n", user)
		select {
		case <-time.After(10 * time.Second):
			return user.Age * 10, nil
		case <-ctx.Done():
			return 0, ctx.Err() // 5 秒后 ctx 到期，返回 DeadlineExceeded
		}
	}, batch.Config{Limit: 2, TaskTimeout: 5 * time.Second, Policy: batch.CancelOnError})

	for idx, res := range results {
		if res.Success() {
			fmt.Printf("idx = %d, age = %d, res = %d\n", idx, users[idx].Age, res.Value)
		} else {
			fmt.Printf("idx = %d, error = %s\n", idx, res.Err)
		}

	}
}
