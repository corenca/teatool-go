package storage_test

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/corenca/teatool-go/storage"
)

// TestResultJSONShape 固定返回型结构体的 JSON 形状：小驼峰、无 omitempty。
func TestResultJSONShape(t *testing.T) {
	cases := []struct {
		name string
		got  any
		want string
	}{
		{
			name: "ObjectInfo",
			got: storage.ObjectInfo{
				Key:          "a.txt",
				Size:         12,
				LastModified: time.Unix(0, 0).UTC(),
				ETag:         "e",
				ContentType:  "text/plain",
				Metadata:     map[string]string{"k": "v"},
			},
			want: `{"key":"a.txt","size":12,"lastModified":"1970-01-01T00:00:00Z","etag":"e","contentType":"text/plain","contentDisposition":"","cacheControl":"","storageClass":"","metadata":{"k":"v"}}`,
		},
		{
			name: "ListPage",
			got:  storage.ListPage{PageSize: 10},
			want: `{"objects":null,"commonPrefixes":null,"nextCursor":"","pageSize":10}`,
		},
		{
			name: "DirInfo",
			got:  storage.DirInfo{Prefix: "p/"},
			want: `{"prefix":"p/","dirs":null,"objects":null}`,
		},
		{
			name: "PresignedURL",
			got:  storage.PresignedURL{URL: "u"},
			want: `{"url":"u","headers":null}`,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.got)
			if err != nil {
				t.Fatalf("json.Marshal(%s) 失败: %v", tc.name, err)
			}
			t.Logf("%s JSON = %s", tc.name, raw)
			if string(raw) != tc.want {
				t.Fatalf("%s JSON 不匹配\n got: %s\nwant: %s", tc.name, raw, tc.want)
			}
		})
	}
}

// TestErrorSentinels 保证包装后的错误仍可被 errors.Is 识别。
func TestErrorSentinels(t *testing.T) {
	wrapped := fmt.Errorf("get a.txt: %w", storage.ErrObjectNotFound)
	t.Logf("wrapped error = %v", wrapped)
	if !errors.Is(wrapped, storage.ErrObjectNotFound) {
		t.Fatal("errors.Is 未识别 ErrObjectNotFound")
	}
	if errors.Is(wrapped, storage.ErrInvalidArgument) {
		t.Fatal("ErrInvalidArgument 不应匹配")
	}
}

// TestResultString 固定返回型结构体的 String 输出，便于日志与测试观察。
func TestResultString(t *testing.T) {
	info := storage.ObjectInfo{
		Key:          "a.txt",
		Size:         12,
		LastModified: time.Unix(0, 0).UTC(),
		ETag:         "e",
		ContentType:  "text/plain",
		Metadata:     map[string]string{"k": "v"},
	}
	cases := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "ObjectInfo",
			got:  info.String(),
			want: "ObjectInfo(key: a.txt, size: 12, lastModified: 1970-01-01T00:00:00Z, etag: e, contentType: text/plain, contentDisposition: , cacheControl: , storageClass: , metadata: map[k:v])",
		},
		{
			name: "ByteRange",
			got:  storage.ByteRange{Start: 10, End: 20}.String(),
			want: "ByteRange(start: 10, end: 20)",
		},
		{
			name: "ListPage",
			got: storage.ListPage{
				Objects:        []storage.ObjectInfo{info},
				CommonPrefixes: []string{"p/a/"},
				NextCursor:     "c1",
				PageSize:       10,
			}.String(),
			want: "ListPage(objects: [ObjectInfo(key: a.txt, size: 12, lastModified: 1970-01-01T00:00:00Z, etag: e, contentType: text/plain, contentDisposition: , cacheControl: , storageClass: , metadata: map[k:v])], commonPrefixes: [p/a/], nextCursor: c1, pageSize: 10)",
		},
		{
			name: "DirInfo",
			got:  storage.DirInfo{Prefix: "p/", Dirs: []string{"a"}, Objects: []storage.ObjectInfo{info}}.String(),
			want: "DirInfo(prefix: p/, dirs: [a], objects: [ObjectInfo(key: a.txt, size: 12, lastModified: 1970-01-01T00:00:00Z, etag: e, contentType: text/plain, contentDisposition: , cacheControl: , storageClass: , metadata: map[k:v])])",
		},
		{
			name: "PresignedURL",
			got:  storage.PresignedURL{URL: "https://example.com/x", Headers: map[string]string{"h": "v"}}.String(),
			want: "PresignedURL(url: https://example.com/x, headers: map[h:v])",
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Logf("%s String = %s", tc.name, tc.got)
			if tc.got != tc.want {
				t.Fatalf("%s String 不匹配\n got: %s\nwant: %s", tc.name, tc.got, tc.want)
			}
		})
	}
}

// TestDeleteErrorUnwrap 校验批量删除错误可按 Key 取出、可追溯底层原因。
func TestDeleteErrorUnwrap(t *testing.T) {
	inner := errors.New("denied")
	err := errors.Join(
		&storage.DeleteError{Key: "a.txt", Err: inner},
		&storage.DeleteError{Key: "b.txt", Err: errors.New("locked")},
	)
	t.Logf("聚合错误 = %v", err)

	var target *storage.DeleteError
	if !errors.As(err, &target) {
		t.Fatal("errors.As 未取出 *DeleteError")
	}
	if target.Key != "a.txt" {
		t.Fatalf("首个失败 key = %q, want a.txt", target.Key)
	}
	if !errors.Is(err, inner) {
		t.Fatal("errors.Is 未识别底层错误")
	}
}
