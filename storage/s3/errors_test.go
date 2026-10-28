package s3

import (
	"errors"
	"net/http"
	"testing"

	"github.com/aws/smithy-go"
	smithyhttp "github.com/aws/smithy-go/transport/http"

	"github.com/corenca/teatool-go/storage"
)

// TestMapErrorByCode 校验常见 API 错误码映射到契约哨兵。
func TestMapErrorByCode(t *testing.T) {
	cases := map[string]error{
		"NoSuchKey":             storage.ErrObjectNotFound,
		"NoSuchBucket":          storage.ErrBucketNotFound,
		"AccessDenied":          storage.ErrAccessDenied,
		"SignatureDoesNotMatch": storage.ErrAccessDenied,
		"PreconditionFailed":    storage.ErrPreconditionFailed,
		"InvalidRange":          storage.ErrInvalidRange,
		"NotImplemented":        storage.ErrUnsupported,
	}
	for code, want := range cases {
		err := mapError(&smithy.GenericAPIError{Code: code, Message: "x"})
		t.Logf("code=%s → %v", code, err)
		if !errors.Is(err, want) {
			t.Fatalf("code=%s: 期望 %v，实际 %v", code, want, err)
		}
	}
}

// TestMapErrorByStatus 校验只返回状态码的兼容端点也能正确分类。
func TestMapErrorByStatus(t *testing.T) {
	cases := map[int]error{
		http.StatusNotFound:                     storage.ErrObjectNotFound,
		http.StatusForbidden:                    storage.ErrAccessDenied,
		http.StatusPreconditionFailed:           storage.ErrPreconditionFailed,
		http.StatusRequestedRangeNotSatisfiable: storage.ErrInvalidRange,
		http.StatusNotImplemented:               storage.ErrUnsupported,
	}
	for status, want := range cases {
		err := mapError(&smithyhttp.ResponseError{
			Err:      errors.New("x"),
			Response: &smithyhttp.Response{Response: &http.Response{StatusCode: status}},
		})
		t.Logf("status=%d → %v", status, err)
		if !errors.Is(err, want) {
			t.Fatalf("status=%d: 期望 %v，实际 %v", status, want, err)
		}
	}
}

// TestMapErrorPassthrough 校验无法分类的错误原样返回。
func TestMapErrorPassthrough(t *testing.T) {
	original := errors.New("boom")
	if got := mapError(original); got != original {
		t.Fatalf("未知错误被改写: %v", got)
	}
	if got := mapError(nil); got != nil {
		t.Fatalf("nil 应保持 nil，实际 %v", got)
	}
}
