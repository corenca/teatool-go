package parquet_test

import (
	"math/big"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/corenca/teatool-go/parquet"
)

// sparkLikeRow 对应 testdata/spark_like.parquet 的列（pyarrow 生成）。
type sparkLikeRow struct {
	ID    int64     `parquet:"id"`
	Name  string    `parquet:"name"`
	Score float64   `parquet:"score"`
	Ts    time.Time `parquet:"ts,timestamp(microsecond)"`
	Day   time.Time `parquet:"day,date"`
	Tags  []string  `parquet:"tags,list"`
	Info  struct {
		City string `parquet:"city"`
	} `parquet:"info"`
	Note *string `parquet:"note"`
}

// decBytesRow 用 []byte + decimal 标签读取 decimal 的原始字节（scale 2）。
type decBytesRow struct {
	Amount []byte `parquet:"amount,decimal(2:10)"`
}

// TestInteropSamples 读取 pyarrow 生成的真实文件，验证能消费第三方写入器的产物。
//
// 样本由 testdata/gen_samples.py 生成；缺失时跳过，不阻塞本地测试。
func TestInteropSamples(t *testing.T) {
	const dir = "testdata"
	if _, err := os.Stat(dir); err != nil {
		t.Skipf("没有 %s 目录，跳过互操作测试: %v", dir, err)
	}

	t.Run("spark_like", func(t *testing.T) {
		path := filepath.Join(dir, "spark_like.parquet")
		if _, err := os.Stat(path); err != nil {
			t.Skipf("样本缺失: %v", err)
		}
		rows, err := parquet.ReadFile[sparkLikeRow](path, parquet.ReadOptions{})
		if err != nil {
			t.Fatalf("读取失败: %v", err)
		}
		if len(rows) != 2 {
			t.Fatalf("行数 = %d, 期望 2", len(rows))
		}
		r := rows[0]
		t.Logf("首行: %+v", r)
		if r.ID != 1 || r.Name != "张三" || r.Score != 98.5 {
			t.Errorf("标量不对: %+v", r)
		}
		if want := time.Date(2026, 11, 10, 9, 30, 0, 0, time.UTC); !r.Ts.Equal(want) {
			t.Errorf("ts = %v, 期望 %v", r.Ts, want)
		}
		if want := time.Date(2026, 11, 10, 0, 0, 0, 0, time.UTC); !r.Day.Equal(want) {
			t.Errorf("day = %v, 期望 %v", r.Day, want)
		}
		if len(r.Tags) != 2 || r.Tags[0] != "a" {
			t.Errorf("tags = %v", r.Tags)
		}
		if r.Info.City != "北京" {
			t.Errorf("info.city = %q, 期望 北京", r.Info.City)
		}
		if r.Note != nil {
			t.Errorf("note = %v, 期望 nil", *r.Note)
		}
		if rows[1].Note == nil || *rows[1].Note != "备注" {
			t.Errorf("第二行 note = %v, 期望 备注", rows[1].Note)
		}
	})

	t.Run("decimal", func(t *testing.T) {
		path := filepath.Join(dir, "decimal.parquet")
		if _, err := os.Stat(path); err != nil {
			t.Skipf("样本缺失: %v", err)
		}
		rows, err := parquet.ReadFile[map[string]any](path, parquet.ReadOptions{})
		if err != nil {
			t.Fatalf("动态读取失败: %v", err)
		}
		t.Logf("decimal 动态行: %#v", rows)
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, 期望 1", len(rows))
		}
		// 动态模式把 decimal 还原成 *big.Float（值 1234.50）。
		amount, ok := rows[0]["amount"].(*big.Float)
		if !ok || amount.Cmp(big.NewFloat(1234.5)) != 0 {
			t.Fatalf("amount = %#v, 期望 *big.Float 1234.5", rows[0]["amount"])
		}

		// 需要原始字节时用 []byte + decimal(scale:precision)。
		raw, err := parquet.ReadFile[decBytesRow](path, parquet.ReadOptions{})
		if err != nil {
			t.Fatalf("按 []byte 读取 decimal 失败: %v", err)
		}
		t.Logf("decimal 原始字节: %v", raw[0].Amount)
		if got := new(big.Int).SetBytes(raw[0].Amount).Int64(); got != 123450 {
			t.Errorf("decimal 未缩放整数 = %d, 期望 123450", got)
		}
	})

	t.Run("nested", func(t *testing.T) {
		path := filepath.Join(dir, "nested.parquet")
		if _, err := os.Stat(path); err != nil {
			t.Skipf("样本缺失: %v", err)
		}
		rows, err := parquet.ReadFile[map[string]any](path, parquet.ReadOptions{})
		if err != nil {
			t.Fatalf("动态读取失败: %v", err)
		}
		t.Logf("nested 动态行: %#v", rows)
		if len(rows) != 1 {
			t.Fatalf("行数 = %d, 期望 1", len(rows))
		}
		outer, ok := rows[0]["outer"].(map[string]any)
		if !ok {
			t.Fatalf("outer = %#v, 期望 map[string]any", rows[0]["outer"])
		}
		if inner, ok := outer["inner"].(map[string]any); !ok || inner["city"] != "北京" {
			t.Errorf("outer.inner = %#v", outer["inner"])
		}
		if items, ok := rows[0]["items"].([]any); !ok || len(items) != 1 {
			t.Errorf("items = %#v, 期望长度 1 的 []any", rows[0]["items"])
		}
		// MAP 的值由上游按 schema 的 Go 类型解码，这里是指针。
		attrs, ok := rows[0]["attrs"].(map[string]any)
		if !ok {
			t.Fatalf("attrs = %#v, 期望 map[string]any", rows[0]["attrs"])
		}
		if v, ok := attrs["k"].(*int32); !ok || *v != 5 {
			t.Errorf("attrs[k] = %#v, 期望 *int32 5", attrs["k"])
		}
	})
}
