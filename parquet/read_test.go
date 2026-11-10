package parquet_test

import (
	"errors"
	"io"
	"path/filepath"
	"slices"
	"testing"
	"time"

	pq "github.com/parquet-go/parquet-go"

	"github.com/corenca/teatool-go/parquet"
)

// address 是可用于验证嵌套 group 的结构体。
type address struct {
	City string `parquet:"city"`
}

// allKindsRow 覆盖常用标量、时间、LIST、可空与嵌套结构。
type allKindsRow struct {
	ID     int64     `parquet:"id"`
	Name   string    `parquet:"name"`
	Score  float64   `parquet:"score"`
	Active bool      `parquet:"active"`
	At     time.Time `parquet:"at,timestamp(microsecond)"`
	Tags   []string  `parquet:"tags,list"`
	Note   *string   `parquet:"note"`
	Addr   address   `parquet:"addr"`
}

// rowWithAge 多出一列 age，用于验证缺列报错。
type rowWithAge struct {
	ID  int64 `parquet:"id"`
	Age int64 `parquet:"age"`
}

// sampleTime 是测试用的固定时间，精确到微秒。
var sampleTime = time.Date(2026, 11, 10, 12, 30, 0, 0, time.UTC)

// writeAllKinds 写两份 allKindsRow，第二行的 Note 为空。
func writeAllKinds(t *testing.T, path string) []allKindsRow {
	t.Helper()
	note := "备注一"
	rows := []allKindsRow{
		{ID: 1, Name: "甲", Score: 1.5, Active: true, At: sampleTime,
			Tags: []string{"a", "b"}, Note: &note, Addr: address{City: "北京"}},
		{ID: 2, Name: "乙", Score: 2.5, Active: false, At: sampleTime.Add(time.Hour),
			Tags: []string{}, Note: nil, Addr: address{City: "上海"}},
	}
	if err := pq.WriteFile(path, rows); err != nil {
		t.Fatalf("写样本文件失败: %v", err)
	}
	return rows
}

// TestReadStructRoundTrip 验证结构体全类型往返：标量、时间、LIST、可空、嵌套。
func TestReadStructRoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "all.parquet")
	want := writeAllKinds(t, path)

	got, err := parquet.ReadFile[allKindsRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	t.Logf("读取 %d 行，首行=%+v", len(got), got[0])
	if len(got) != len(want) {
		t.Fatalf("行数 = %d, 期望 %d", len(got), len(want))
	}
	for i := range want {
		w, g := want[i], got[i]
		if g.ID != w.ID || g.Name != w.Name || g.Score != w.Score || g.Active != w.Active {
			t.Errorf("第 %d 行标量不一致: got=%+v want=%+v", i, g, w)
		}
		if !g.At.Equal(w.At) {
			t.Errorf("第 %d 行时间 = %v, 期望 %v", i, g.At, w.At)
		}
		if !slices.Equal(g.Tags, w.Tags) {
			t.Errorf("第 %d 行 Tags = %v, 期望 %v", i, g.Tags, w.Tags)
		}
		if (g.Note == nil) != (w.Note == nil) || (g.Note != nil && *g.Note != *w.Note) {
			t.Errorf("第 %d 行 Note = %v, 期望 %v", i, g.Note, w.Note)
		}
		if g.Addr.City != w.Addr.City {
			t.Errorf("第 %d 行城市 = %q, 期望 %q", i, g.Addr.City, w.Addr.City)
		}
	}
}

// TestReadProjection 验证只读部分列时其余字段为零值。
func TestReadProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proj.parquet")
	writeAllKinds(t, path)

	got, err := parquet.ReadFile[allKindsRow](path, parquet.ReadOptions{Columns: []string{"id", "name"}})
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if len(got) != 2 || got[0].ID != 1 || got[0].Name != "甲" {
		t.Fatalf("投影结果不对: %+v", got)
	}
	if got[0].Score != 0 || !got[0].At.IsZero() || got[0].Addr.City != "" {
		t.Errorf("未投影字段不是零值: %+v", got[0])
	}
}

// TestReadUnknownColumn 验证投影里拼错的列名返回 ErrUnknownColumn。
func TestReadUnknownColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "unknown.parquet")
	writeAllKinds(t, path)

	_, err := parquet.ReadFile[allKindsRow](path, parquet.ReadOptions{Columns: []string{"nope"}})
	if !errors.Is(err, parquet.ErrUnknownColumn) {
		t.Fatalf("err = %v, 期望 ErrUnknownColumn", err)
	}
}

// TestReadMissingColumn 验证缺列默认报错、允许缺失时读回零值。
func TestReadMissingColumn(t *testing.T) {
	path := filepath.Join(t.TempDir(), "missing.parquet")
	writeAllKinds(t, path)

	_, err := parquet.ReadFile[rowWithAge](path, parquet.ReadOptions{})
	var missing *parquet.MissingColumnsError
	if !errors.As(err, &missing) {
		t.Fatalf("err = %v, 期望 *MissingColumnsError", err)
	}
	if !errors.Is(err, parquet.ErrMissingColumn) {
		t.Errorf("err 未命中 ErrMissingColumn: %v", err)
	}
	if !slices.Equal(missing.Columns, []string{"age"}) {
		t.Errorf("缺失列 = %v, 期望 [age]", missing.Columns)
	}

	got, err := parquet.ReadFile[rowWithAge](path, parquet.ReadOptions{AllowMissingColumns: true})
	if err != nil {
		t.Fatalf("允许缺列时读取失败: %v", err)
	}
	if len(got) != 2 || got[0].Age != 0 {
		t.Fatalf("缺列零值不符合预期: %+v", got)
	}
}

// TestReadSkipRows 验证跳过指定行数，以及超出总行数时立即 EOF。
func TestReadSkipRows(t *testing.T) {
	path := filepath.Join(t.TempDir(), "skip.parquet")
	writeSampleFile(t, path, []sampleRow{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}, {ID: 3, Name: "c"}})

	r, err := parquet.OpenReader[sampleRow](path, parquet.ReadOptions{SkipRows: 2})
	if err != nil {
		t.Fatalf("OpenReader 失败: %v", err)
	}
	defer r.Close()
	buf := make([]sampleRow, 10)
	n, err := r.Read(buf)
	if n != 1 || buf[0].ID != 3 {
		t.Fatalf("跳过 2 行后读到 n=%d, rows=%+v", n, buf[:n])
	}
	if !errors.Is(err, io.EOF) && err != nil {
		t.Fatalf("读到末尾的 err = %v, 期望 io.EOF 或 nil", err)
	}

	r2, err := parquet.OpenReader[sampleRow](path, parquet.ReadOptions{SkipRows: 99})
	if err != nil {
		t.Fatalf("OpenReader 失败: %v", err)
	}
	defer r2.Close()
	if _, err := r2.Read(buf); !errors.Is(err, io.EOF) && err == nil {
		t.Fatalf("SkipRows 超出总行数时未返回 io.EOF: err=%v", err)
	}
}

// TestReadEachStop 验证 ErrStop 表示成功提前结束，其他错误原样返回。
func TestReadEachStop(t *testing.T) {
	path := filepath.Join(t.TempDir(), "each.parquet")
	writeSampleFile(t, path, []sampleRow{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}, {ID: 3, Name: "c"}})

	r, err := parquet.OpenReader[sampleRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("OpenReader 失败: %v", err)
	}
	defer r.Close()

	count := 0
	if err := r.Each(func(row sampleRow) error {
		count++
		if count == 2 {
			return parquet.ErrStop
		}
		return nil
	}); err != nil {
		t.Fatalf("ErrStop 应返回 nil, 得到 %v", err)
	}
	if count != 2 {
		t.Errorf("回调次数 = %d, 期望 2", count)
	}

	// 上一次 Each 已经消费掉剩余行，普通错误用新的 Reader 验证。
	r2, err := parquet.OpenReader[sampleRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("OpenReader 失败: %v", err)
	}
	defer r2.Close()
	boom := errors.New("boom")
	if err := r2.Each(func(row sampleRow) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("普通错误应原样返回, 得到 %v", err)
	}
}

// TestReadClosed 验证关闭后所有读取方法返回 ErrClosed。
func TestReadClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "closed.parquet")
	writeSampleFile(t, path, []sampleRow{{ID: 1, Name: "a"}})

	r, err := parquet.OpenReader[sampleRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("OpenReader 失败: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}
	if _, err := r.Read(make([]sampleRow, 1)); !errors.Is(err, parquet.ErrClosed) {
		t.Errorf("Read err = %v, 期望 ErrClosed", err)
	}
	if err := r.Each(func(sampleRow) error { return nil }); !errors.Is(err, parquet.ErrClosed) {
		t.Errorf("Each err = %v, 期望 ErrClosed", err)
	}
	if err := r.SeekToRow(0); !errors.Is(err, parquet.ErrClosed) {
		t.Errorf("SeekToRow err = %v, 期望 ErrClosed", err)
	}
}

// TestReadInvalidType 验证不支持的 T 返回 ErrInvalidSchema。
func TestReadInvalidType(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid.parquet")
	writeSampleFile(t, path, []sampleRow{{ID: 1, Name: "a"}})

	if _, err := parquet.ReadFile[any](path, parquet.ReadOptions{}); !errors.Is(err, parquet.ErrInvalidSchema) {
		t.Errorf("T=any 的 err = %v, 期望 ErrInvalidSchema", err)
	}
	if _, err := parquet.ReadFile[int](path, parquet.ReadOptions{}); !errors.Is(err, parquet.ErrInvalidSchema) {
		t.Errorf("T=int 的 err = %v, 期望 ErrInvalidSchema", err)
	}
}

// TestReadEmptyFile 验证 0 行文件读回空切片而不是报错。
func TestReadEmptyFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "empty.parquet")
	writeSampleFile(t, path, nil)

	got, err := parquet.ReadFile[sampleRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("空文件读取失败: %v", err)
	}
	if len(got) != 0 {
		t.Errorf("空文件读回 %d 行, 期望 0", len(got))
	}
}

// TestReadNestedProjection 验证嵌套列路径只读指定子列，其余字段为零值。
func TestReadNestedProjection(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested-proj.parquet")
	writeAllKinds(t, path)

	got, err := parquet.ReadFile[allKindsRow](path, parquet.ReadOptions{Columns: []string{"addr.city"}})
	if err != nil {
		t.Fatalf("ReadFile 失败: %v", err)
	}
	if len(got) != 2 || got[0].Addr.City != "北京" {
		t.Fatalf("嵌套投影结果不对: %+v", got)
	}
	if got[0].ID != 0 || got[0].Name != "" {
		t.Errorf("未投影的顶层字段应为零值: %+v", got[0])
	}
}

// TestReadProjectionSkipsMissing 验证投影时未被投影的缺失列不报错。
func TestReadProjectionSkipsMissing(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proj-missing.parquet")
	writeSampleFile(t, path, []sampleRow{{ID: 1, Name: "a"}})

	got, err := parquet.ReadFile[rowWithAge](path, parquet.ReadOptions{Columns: []string{"id"}})
	if err != nil {
		t.Fatalf("投影时不应因未投影列缺列而失败: %v", err)
	}
	if len(got) != 1 || got[0].ID != 1 || got[0].Age != 0 {
		t.Fatalf("读取结果 = %+v, 期望 ID=1 Age=0", got)
	}
}

// TestReadProjectionListByTopLevelName 验证 LIST 列可以按顶层列名投影。
func TestReadProjectionListByTopLevelName(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proj-list.parquet")
	writeAllKinds(t, path)

	got, err := parquet.ReadFile[allKindsRow](path, parquet.ReadOptions{Columns: []string{"tags"}})
	if err != nil {
		t.Fatalf("按顶层列名投影 LIST 失败: %v", err)
	}
	if len(got) != 2 || !slices.Equal(got[0].Tags, []string{"a", "b"}) {
		t.Fatalf("Tags = %v, 期望 [a b]", got[0].Tags)
	}
	if got[0].ID != 0 {
		t.Errorf("未投影的 ID 应为零值: %+v", got[0])
	}
}

// TestEachBatch 验证批量回调：批大小生效、ErrStop 提前结束、错误原样返回。
func TestEachBatch(t *testing.T) {
	path := filepath.Join(t.TempDir(), "each-batch.parquet")
	writeSampleFile(t, path, []sampleRow{{ID: 1, Name: "a"}, {ID: 2, Name: "b"}, {ID: 3, Name: "c"}, {ID: 4, Name: "d"}, {ID: 5, Name: "e"}})

	r, err := parquet.OpenReader[sampleRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("OpenReader 失败: %v", err)
	}
	defer r.Close()

	var sizes []int
	var ids []int64
	if err := r.EachBatch(2, func(rows []sampleRow) error {
		sizes = append(sizes, len(rows))
		for _, row := range rows {
			ids = append(ids, row.ID)
		}
		return nil
	}); err != nil {
		t.Fatalf("EachBatch 失败: %v", err)
	}
	t.Logf("批大小=%v, ids=%v", sizes, ids)
	if !slices.Equal(sizes, []int{2, 2, 1}) {
		t.Errorf("批大小 = %v, 期望 [2 2 1]", sizes)
	}
	if !slices.Equal(ids, []int64{1, 2, 3, 4, 5}) {
		t.Errorf("ids = %v", ids)
	}

	// 每批是独立副本：回调结束后仍可安全保留。
	rKept, err := parquet.OpenReader[sampleRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("OpenReader 失败: %v", err)
	}
	defer rKept.Close()
	var kept [][]sampleRow
	if err := rKept.EachBatch(2, func(rows []sampleRow) error {
		kept = append(kept, rows)
		return nil
	}); err != nil {
		t.Fatalf("EachBatch 失败: %v", err)
	}
	if len(kept) != 3 || kept[0][0].ID != 1 || kept[1][0].ID != 3 || kept[2][0].ID != 5 {
		t.Errorf("保留的批次被覆盖: %v", kept)
	}

	// 上一轮已把游标读到末尾，后续语义用新的 Reader 验证。
	r2, err := parquet.OpenReader[sampleRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("OpenReader 失败: %v", err)
	}
	defer r2.Close()

	// ErrStop 在第一批停止，EachBatch 返回 nil。
	calls := 0
	if err := r2.EachBatch(2, func(rows []sampleRow) error {
		calls++
		return parquet.ErrStop
	}); err != nil {
		t.Fatalf("ErrStop 应返回 nil, 得到 %v", err)
	}
	if calls != 1 {
		t.Errorf("回调次数 = %d, 期望 1", calls)
	}

	r3, err := parquet.OpenReader[sampleRow](path, parquet.ReadOptions{})
	if err != nil {
		t.Fatalf("OpenReader 失败: %v", err)
	}
	defer r3.Close()

	// 普通错误原样返回。
	boom := errors.New("boom")
	if err := r3.EachBatch(2, func(rows []sampleRow) error { return boom }); !errors.Is(err, boom) {
		t.Fatalf("普通错误应原样返回, 得到 %v", err)
	}
}
