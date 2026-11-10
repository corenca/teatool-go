package parquet_test

import (
	"errors"
	"path/filepath"
	"slices"
	"testing"

	pq "github.com/parquet-go/parquet-go"

	"github.com/corenca/teatool-go/parquet"
)

// dynRow 覆盖动态读取需要的物理值类型：标量、LIST、可空、嵌套 group。
type dynRow struct {
	B    bool     `parquet:"b"`
	I32  int32    `parquet:"i32"`
	I64  int64    `parquet:"i64"`
	F32  float32  `parquet:"f32"`
	F64  float64  `parquet:"f64"`
	S    string   `parquet:"s"`
	Tags []string `parquet:"tags,list"`
	Note *string  `parquet:"note"`
	Addr address  `parquet:"addr"`
}

// TestReadDynamicUsesFileSchema 验证 map 模式不传 Schema 时用文件 schema，返回物理值。
func TestReadDynamicUsesFileSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic.parquet")
	if err := pq.WriteFile(path, []dynRow{{
		B: true, I32: 7, I64: 8, F32: 1.5, F64: 2.5, S: "str",
		Tags: []string{"x", "y"}, Note: nil, Addr: address{City: "北京"},
	}}); err != nil {
		t.Fatalf("写样本文件失败: %v", err)
	}

	got, err := parquet.ReadFile[map[string]any](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("动态读取失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("行数 = %d, 期望 1", len(got))
	}
	row := got[0]
	t.Logf("动态行: %#v", row)

	if v, ok := row["b"].(bool); !ok || !v {
		t.Errorf("b = %#v, 期望 bool true", row["b"])
	}
	if v, ok := row["i32"].(int32); !ok || v != 7 {
		t.Errorf("i32 = %#v, 期望 int32 7", row["i32"])
	}
	if v, ok := row["i64"].(int64); !ok || v != 8 {
		t.Errorf("i64 = %#v, 期望 int64 8", row["i64"])
	}
	if v, ok := row["f32"].(float32); !ok || v != 1.5 {
		t.Errorf("f32 = %#v, 期望 float32 1.5", row["f32"])
	}
	if v, ok := row["f64"].(float64); !ok || v != 2.5 {
		t.Errorf("f64 = %#v, 期望 float64 2.5", row["f64"])
	}
	if v, ok := row["s"].(string); !ok || v != "str" {
		t.Errorf("s = %#v, 期望 string str", row["s"])
	}
	if v, ok := row["tags"].([]any); !ok || !slices.Equal(v, []any{"x", "y"}) {
		t.Errorf("tags = %#v, 期望 []any[x y]", row["tags"])
	}
	if v, ok := row["note"]; !ok || v != nil {
		t.Errorf("note = %#v, 期望 nil", row["note"])
	}
	addr, ok := row["addr"].(map[string]any)
	if !ok || addr["city"] != "北京" {
		t.Errorf("addr = %#v, 期望 map[city:北京]", row["addr"])
	}
}

// TestReadDynamicProjection 验证动态读取同样支持列投影。
func TestReadDynamicProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic-proj.parquet")
	writeSampleFile(t, path, []sampleRow{{ID: 1, Name: "a"}})

	got, err := parquet.ReadFile[map[string]any](path, parquet.ReadOptions{Columns: []string{"id"}})
	if err != nil {
		t.Fatalf("动态投影读取失败: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("行数 = %d, 期望 1", len(got))
	}
	if _, ok := got[0]["name"]; ok {
		t.Errorf("未投影的列不应出现: %#v", got[0])
	}
	if v, ok := got[0]["id"].(int64); !ok || v != 1 {
		t.Errorf("id = %#v, 期望 int64 1", got[0]["id"])
	}
}

// TestReadDynamicStructWithSchema 验证结构体读取不接受 Schema 选项。
func TestReadDynamicStructWithSchema(t *testing.T) {
	path := filepath.Join(t.TempDir(), "dynamic-struct.parquet")
	writeSampleFile(t, path, []sampleRow{{ID: 1, Name: "a"}})

	_, err := parquet.OpenReader[sampleRow](path, parquet.ReadOptions{Schema: pq.SchemaOf(new(sampleRow))})
	if !errors.Is(err, parquet.ErrInvalidOption) {
		t.Fatalf("err = %v, 期望 ErrInvalidOption", err)
	}
}
