package parquet_test

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	pq "github.com/parquet-go/parquet-go"

	"github.com/corenca/teatool-go/parquet"
)

// tsMicrosRow 用微秒精度的时间戳，对应 Spark/Arrow 的常见写法。
type tsMicrosRow struct {
	Ts time.Time `parquet:"ts,timestamp(microsecond)"`
}

// tsDefaultRow 不写精度，上游默认按纳秒解释。
type tsDefaultRow struct {
	Ts time.Time `parquet:"ts"`
}

// rawRow 的 raw 列是无逻辑类型的 BYTE_ARRAY，name 是 UTF8。
type rawRow struct {
	Raw  []byte `parquet:"raw"`
	Name string `parquet:"name"`
}

// textRow 与 rawRow 物理类型相同、Go 类型相反，用于验证 BYTE_ARRAY 例外规则。
type textRow struct {
	Raw  string `parquet:"raw"`
	Name []byte `parquet:"name"`
}

// decRow 声明 decimal(10,2)，decRowBad 小数位不同。
type decRow struct {
	Amount int64 `parquet:"amount,decimal(2:10)"`
}

// decRowBad 与 decRow 只差 decimal 的 scale。
type decRowBad struct {
	Amount int64 `parquet:"amount,decimal(3:10)"`
}

// TestReadTimestampUnitMismatch 验证时间戳单位不一致时给出可定位的错误与建议 tag。
func TestReadTimestampUnitMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "ts.parquet")
	if err := pq.WriteFile(path, []tsMicrosRow{{Ts: sampleTime}}); err != nil {
		t.Fatalf("写样本文件失败: %v", err)
	}

	_, err := parquet.ReadFile[tsDefaultRow](path, parquet.ReadOptions{})
	var mismatch *parquet.IncompatibleColumnsError
	if !errors.As(err, &mismatch) {
		t.Fatalf("err = %v, 期望 *IncompatibleColumnsError", err)
	}
	if !errors.Is(err, parquet.ErrIncompatibleColumn) {
		t.Errorf("err 未命中 ErrIncompatibleColumn: %v", err)
	}
	if len(mismatch.Columns) != 1 || mismatch.Columns[0].Path != "ts" {
		t.Fatalf("不兼容列 = %+v, 期望只有 ts", mismatch.Columns)
	}
	msg := err.Error()
	t.Logf("错误文本: %s", msg)
	if !strings.Contains(msg, "timestamp") || !strings.Contains(msg, "改为") {
		t.Errorf("错误文本应包含类型与建议 tag: %s", msg)
	}

	// 写对 tag 之后应当读到正确时间。
	got, err := parquet.ReadFile[tsMicrosRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("tag 对齐后读取失败: %v", err)
	}
	if len(got) != 1 || !got[0].Ts.Equal(sampleTime) {
		t.Fatalf("时间 = %v, 期望 %v", got[0].Ts, sampleTime)
	}
}

// TestReadByteArrayCompatibility 验证 BYTE_ARRAY 上 string/[]byte 与逻辑类型无/UTF8 互通。
func TestReadByteArrayCompatibility(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bytes.parquet")
	if err := pq.WriteFile(path, []rawRow{{Raw: []byte("r1"), Name: "名字"}}); err != nil {
		t.Fatalf("写样本文件失败: %v", err)
	}

	got, err := parquet.ReadFile[textRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("BYTE_ARRAY 类型互通应通过, 得到 %v", err)
	}
	if len(got) != 1 || got[0].Raw != "r1" || string(got[0].Name) != "名字" {
		t.Fatalf("读取结果 = %+v", got)
	}
}

// TestReadInt32VsInt64Mismatch 验证物理类型宽度不同视为不兼容。
func TestReadInt32VsInt64Mismatch(t *testing.T) {
	type fileRow struct {
		ID int64 `parquet:"id"`
	}
	type targetRow struct {
		ID int32 `parquet:"id"`
	}
	path := filepath.Join(t.TempDir(), "width.parquet")
	if err := pq.WriteFile(path, []fileRow{{ID: 1}}); err != nil {
		t.Fatalf("写样本文件失败: %v", err)
	}

	_, err := parquet.ReadFile[targetRow](path, parquet.ReadOptions{})
	if !errors.Is(err, parquet.ErrIncompatibleColumn) {
		t.Fatalf("err = %v, 期望 ErrIncompatibleColumn", err)
	}
}

// TestReadDecimalScaleMismatch 验证 decimal 的 scale 不同视为不兼容。
func TestReadDecimalScaleMismatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "decimal.parquet")
	if err := pq.WriteFile(path, []decRow{{Amount: 1234}}); err != nil {
		t.Fatalf("写样本文件失败: %v", err)
	}

	if _, err := parquet.ReadFile[decRowBad](path, parquet.ReadOptions{}); !errors.Is(err, parquet.ErrIncompatibleColumn) {
		t.Fatalf("scale 不同应不兼容, 得到 %v", err)
	}
	got, err := parquet.ReadFile[decRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("scale 相同时读取失败: %v", err)
	}
	if len(got) != 1 || got[0].Amount != 1234 {
		t.Fatalf("decimal 读取结果 = %+v", got)
	}
}

// mixedFileRow 的两个时间列单位不同，用于验证投影排除未读列的类型校验。
type mixedFileRow struct {
	Ts time.Time `parquet:"ts,timestamp(microsecond)"`
	At time.Time `parquet:"at,timestamp(nanosecond)"`
}

// mixedTargetRow 的 at 列 tag 与文件不一致，但投影时不应被校验。
type mixedTargetRow struct {
	Ts time.Time `parquet:"ts,timestamp(microsecond)"`
	At time.Time `parquet:"at,timestamp(microsecond)"`
}

// TestReadProjectionSkipsIncompatible 验证未投影的列不参与类型兼容校验。
func TestReadProjectionSkipsIncompatible(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proj-compat.parquet")
	if err := pq.WriteFile(path, []mixedFileRow{{Ts: sampleTime, At: sampleTime}}); err != nil {
		t.Fatalf("写样本文件失败: %v", err)
	}

	// 不投影：at 单位不一致，必须报错。
	if _, err := parquet.ReadFile[mixedTargetRow](path, parquet.ReadOptions{}); !errors.Is(err, parquet.ErrIncompatibleColumn) {
		t.Fatalf("不投影时应报类型不兼容, 得到 %v", err)
	}

	// 只投影 ts：at 不读，不应报错。
	got, err := parquet.ReadFile[mixedTargetRow](path, parquet.ReadOptions{Columns: []string{"ts"}})
	if err != nil {
		t.Fatalf("投影后不应校验未读列: %v", err)
	}
	if len(got) != 1 || !got[0].Ts.Equal(sampleTime) || !got[0].At.IsZero() {
		t.Fatalf("读取结果 = %+v", got[0])
	}
}
