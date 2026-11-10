package parquet_test

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	pq "github.com/parquet-go/parquet-go"

	"github.com/corenca/teatool-go/parquet"
)

// dynWriteRow 是动态写出用的 schema 来源。
type dynWriteRow struct {
	ID   int64    `parquet:"id"`
	Name string   `parquet:"name"`
	At   int64    `parquet:"at,timestamp(microsecond)"`
	Tags []string `parquet:"tags,list"`
	Note *string  `parquet:"note"`
}

// int32Row 用于验证写入值类型不匹配。
type int32Row struct {
	Count int32 `parquet:"count"`
}

// TestWriteDynamicRequiresSchema 验证动态写出必须提供 Schema。
func TestWriteDynamicRequiresSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "no-schema.parquet")
	err := parquet.WriteFile[map[string]any](path, []map[string]any{{"id": int64(1)}}, parquet.WriteOptions{})
	if !errors.Is(err, parquet.ErrInvalidSchema) {
		t.Fatalf("err = %v, 期望 ErrInvalidSchema", err)
	}
}

// TestWriteDynamicRoundTrip 验证 map 行按给定 schema 写出后能按物理值读回。
func TestWriteDynamicRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic-write.parquet")
	schema := pq.SchemaOf(new(dynWriteRow))

	rows := []map[string]any{{
		"id":   int64(1),
		"name": "甲",
		"at":   sampleTime,
		"tags": []string{"a", "b"},
		"note": nil,
	}}
	if err := parquet.WriteFile[map[string]any](path, rows, parquet.WriteOptions{Schema: schema}); err != nil {
		t.Fatalf("WriteFile 失败: %v", err)
	}

	got, err := parquet.ReadFile[map[string]any](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	t.Logf("动态往返: %#v", got)
	if len(got) != 1 {
		t.Fatalf("行数 = %d, 期望 1", len(got))
	}
	row := got[0]
	if v, ok := row["id"].(int64); !ok || v != 1 {
		t.Errorf("id = %#v, 期望 int64 1", row["id"])
	}
	if row["name"] != "甲" {
		t.Errorf("name = %#v, 期望 甲", row["name"])
	}
	if v, ok := row["at"].(int64); !ok || v != sampleTime.UnixMicro() {
		t.Errorf("at = %#v, 期望微秒时间戳 %d", row["at"], sampleTime.UnixMicro())
	}
	if v, ok := row["tags"].([]any); !ok || !slices.Equal(v, []any{"a", "b"}) {
		t.Errorf("tags = %#v, 期望 [a b]", row["tags"])
	}
	if row["note"] != nil {
		t.Errorf("note = %#v, 期望 nil", row["note"])
	}
}

// TestWriteDynamicTypeMismatch 验证值类型不匹配时返回 error 而不是 panic，且不落盘。
func TestWriteDynamicTypeMismatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "bad-value.parquet")
	schema := pq.SchemaOf(new(int32Row))

	err := parquet.WriteFile[map[string]any](path, []map[string]any{{"count": "不是数字"}}, parquet.WriteOptions{Schema: schema})
	if err == nil {
		t.Fatalf("类型不匹配应返回错误")
	}
	t.Logf("错误: %v", err)
	if !strings.Contains(err.Error(), "int32") {
		t.Errorf("错误文本应指出列类型: %v", err)
	}
	if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
		t.Errorf("失败后不应产生目标文件: %v", statErr)
	}
	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("读取目录失败: %v", readErr)
	}
	if len(entries) != 0 {
		t.Errorf("失败后目录有残留: %v", names(entries))
	}
}
