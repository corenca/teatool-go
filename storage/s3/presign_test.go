package s3_test

import (
	"errors"
	"io"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/corenca/teatool-go/storage"
)

// TestPresignGet 校验下载预签名 URL 真正可用，且文件名进入响应头。
func TestPresignGet(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "报告.pdf"
	payload := []byte("presigned download body")

	if err := client.PutBytes(ctx, bucket, key, payload, storage.WriterOptions{}); err != nil {
		t.Fatalf("写入失败: %v", err)
	}
	signed, err := client.PresignGet(ctx, bucket, key, storage.PresignOptions{
		Expires:  5 * time.Minute,
		FileName: "报告.pdf",
	})
	if err != nil {
		t.Fatalf("PresignGet 失败: %v", err)
	}
	t.Logf("预签名 URL 有效期参数 = %s", mustQuery(t, signed.URL).Get("X-Amz-Expires"))

	resp, err := http.Get(signed.URL)
	if err != nil {
		t.Fatalf("访问预签名 URL 失败: %v", err)
	}
	defer resp.Body.Close()
	got, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatalf("读取响应失败: %v", err)
	}
	t.Logf("状态码 = %d 内容 = %q disposition = %q",
		resp.StatusCode, got, resp.Header.Get("Content-Disposition"))

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("状态码 = %d", resp.StatusCode)
	}
	disposition := resp.Header.Get("Content-Disposition")
	if !strings.Contains(disposition, "filename*=UTF-8''%E6%8A%A5%E5%91%8A.pdf") {
		t.Fatalf("Content-Disposition 缺少 RFC 5987 文件名: %q", disposition)
	}
	if string(got) != string(payload) {
		t.Fatalf("内容 = %q, want %q", got, payload)
	}
}

// TestPresignPut 校验上传预签名 URL 真正可用。
func TestPresignPut(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	key := prefix + "presigned-put.txt"
	payload := "presigned upload body"

	signed, err := client.PresignPut(ctx, bucket, key, storage.PresignOptions{Expires: 5 * time.Minute})
	if err != nil {
		t.Fatalf("PresignPut 失败: %v", err)
	}

	req, err := http.NewRequest(http.MethodPut, signed.URL, strings.NewReader(payload))
	if err != nil {
		t.Fatalf("构造请求失败: %v", err)
	}
	req.ContentLength = int64(len(payload))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("上传失败: %v", err)
	}
	defer resp.Body.Close()
	t.Logf("上传状态码 = %d", resp.StatusCode)
	if resp.StatusCode/100 != 2 {
		body, _ := io.ReadAll(resp.Body)
		t.Fatalf("状态码 = %d body=%s", resp.StatusCode, body)
	}

	got, err := client.GetBytes(ctx, bucket, key, storage.ReaderOptions{})
	if err != nil {
		t.Fatalf("GetBytes 失败: %v", err)
	}
	if string(got) != payload {
		t.Fatalf("内容 = %q, want %q", got, payload)
	}
}

// TestPresignRejectsInvalidExpiry 校验有效期必须为正且不超过 7 天。
func TestPresignRejectsInvalidExpiry(t *testing.T) {
	client := newOfflineClient(t)

	cases := map[string]time.Duration{
		"零":    0,
		"负数":   -time.Second,
		"超过7天": storage.MaxPresignExpiry + time.Second,
	}
	for name, expires := range cases {
		_, err := client.PresignGet(t.Context(), "bucket", "k", storage.PresignOptions{Expires: expires})
		t.Logf("Expires=%s(%v) error = %v", name, expires, err)
		if !errors.Is(err, storage.ErrInvalidArgument) {
			t.Fatalf("%s: 期望 ErrInvalidArgument，实际 %v", name, err)
		}
	}
}

// TestPresignRejectsMethodMismatch 校验 FileName 只用于 GET。
func TestPresignRejectsMethodMismatch(t *testing.T) {
	client := newOfflineClient(t)

	_, err := client.PresignPut(t.Context(), "bucket", "k", storage.PresignOptions{
		Expires: time.Minute, FileName: "a.txt",
	})
	t.Logf("PresignPut+FileName error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}

// mustQuery 解析 URL 查询串，失败即终止。
func mustQuery(t *testing.T, raw string) url.Values {
	t.Helper()
	u, err := url.Parse(raw)
	if err != nil {
		t.Fatalf("解析 URL 失败: %v", err)
	}
	return u.Query()
}
