package parquet_test

import (
	"testing"

	pq "github.com/parquet-go/parquet-go"
)

// sampleRow 是测试用的最小结构体，tag 与文件列一一对应。
type sampleRow struct {
	ID   int64  `parquet:"id"`
	Name string `parquet:"name"`
}

// writeSampleFile 用上游 API 直接写文件，避免测试依赖本包的写出实现。
func writeSampleFile(t *testing.T, path string, rows []sampleRow) {
	t.Helper()
	if err := pq.WriteFile(path, rows); err != nil {
		t.Fatalf("写样本文件失败: %v", err)
	}
}
