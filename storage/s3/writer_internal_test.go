package s3

import (
	"errors"
	"testing"

	"github.com/corenca/teatool-go/storage"
)

// TestDispatchRejectsTooManyParts 校验超过 MaxParts 时在发请求前报错并给出提示。
func TestDispatchRejectsTooManyParts(t *testing.T) {
	w := &writer{partSize: storage.MinPartSize, concurrency: 1, sem: make(chan struct{}, 1)}
	w.partNumber = storage.MaxParts

	err := w.dispatch([]byte("x"))
	t.Logf("分片数已满时 dispatch error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}
