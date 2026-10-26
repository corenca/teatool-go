package s3_test

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/corenca/teatool-go/storage"
)

// TestListPagination 校验分页游标可原样回传且无重无漏。
func TestListPagination(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	listPrefix := prefix + "page/"

	for i := range 3 {
		key := fmt.Sprintf("%s%d.txt", listPrefix, i)
		if err := client.PutBytes(ctx, bucket, key, []byte("x"), storage.WriterOptions{}); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	var keys []string
	cursor := ""
	for pages := 1; ; pages++ {
		page, err := client.List(ctx, bucket, storage.ListOptions{Prefix: listPrefix, MaxKeys: 2, Cursor: cursor})
		if err != nil {
			t.Fatalf("List 失败: %v", err)
		}
		t.Logf("第 %d 页 pageSize=%d objects=%d next=%v", pages, page.PageSize, len(page.Objects), page.NextCursor != "")
		if page.PageSize != 2 {
			t.Fatalf("PageSize = %d, want 2", page.PageSize)
		}
		for _, object := range page.Objects {
			keys = append(keys, object.Key)
		}
		if page.NextCursor == "" {
			break
		}
		cursor = page.NextCursor
		if pages > 5 {
			t.Fatal("分页未终止")
		}
	}

	t.Logf("全部分页 key = %v", keys)
	if len(keys) != 3 {
		t.Fatalf("key 数量 = %d, want 3", len(keys))
	}
	slices.Sort(keys)
	for i, key := range keys {
		if want := fmt.Sprintf("%s%d.txt", listPrefix, i); key != want {
			t.Fatalf("第 %d 个 key = %q, want %q", i, key, want)
		}
	}
}

// TestListRejectsBadCursor 校验非法游标返回 ErrInvalidArgument。
func TestListRejectsBadCursor(t *testing.T) {
	client := newOfflineClient(t)

	_, err := client.List(t.Context(), "bucket", storage.ListOptions{Cursor: "!!!"})
	t.Logf("List(非法游标) error = %v", err)
	if !errors.Is(err, storage.ErrInvalidArgument) {
		t.Fatalf("期望 ErrInvalidArgument，实际 %v", err)
	}
}

// TestListAll 校验自动翻页拼接。
func TestListAll(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	listPrefix := prefix + "all/"

	for i := range 3 {
		key := fmt.Sprintf("%s%d.txt", listPrefix, i)
		if err := client.PutBytes(ctx, bucket, key, []byte("x"), storage.WriterOptions{}); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	objects, err := client.ListAll(ctx, bucket, listPrefix)
	if err != nil {
		t.Fatalf("ListAll 失败: %v", err)
	}
	var keys []string
	for _, object := range objects {
		keys = append(keys, object.Key)
	}
	t.Logf("ListAll keys = %v", keys)
	if len(keys) != 3 {
		t.Fatalf("key 数量 = %d, want 3", len(keys))
	}
}

// TestListDirNamesOnly 校验目录视图只给本层文件夹名。
func TestListDirNamesOnly(t *testing.T) {
	client, bucket, prefix := newTestContext(t)
	ctx := t.Context()
	dirPrefix := prefix + "dir/"

	for _, key := range []string{dirPrefix + "a/1.txt", dirPrefix + "b/1.txt", dirPrefix + "top.txt"} {
		if err := client.PutBytes(ctx, bucket, key, []byte("x"), storage.WriterOptions{}); err != nil {
			t.Fatalf("写入失败: %v", err)
		}
	}

	dir, err := client.ListDir(ctx, bucket, dirPrefix)
	if err != nil {
		t.Fatalf("ListDir 失败: %v", err)
	}
	t.Logf("DirInfo = %+v", dir)

	if dir.Prefix != dirPrefix {
		t.Fatalf("Prefix = %q", dir.Prefix)
	}
	if strings.Join(dir.Dirs, ",") != "a,b" {
		t.Fatalf("Dirs = %v, want [a b]", dir.Dirs)
	}
	if len(dir.Objects) != 1 || dir.Objects[0].Key != dirPrefix+"top.txt" {
		t.Fatalf("Objects 不符: %+v", dir.Objects)
	}
}

// TestListBuckets 校验桶列表包含测试用桶。
func TestListBuckets(t *testing.T) {
	client, bucket := loadTestConfig(t)

	buckets, err := client.ListBuckets(t.Context())
	if err != nil {
		t.Fatalf("ListBuckets 失败: %v", err)
	}
	t.Logf("buckets = %v", buckets)
	if !slices.Contains(buckets, bucket) {
		t.Fatalf("桶列表缺少 %q: %v", bucket, buckets)
	}
}
