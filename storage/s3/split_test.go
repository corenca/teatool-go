package s3

import "testing"

// TestSplitKeys 校验批量删除的分批边界。
func TestSplitKeys(t *testing.T) {
	cases := []struct {
		name string
		keys []string
		want []int
	}{
		{name: "空列表", keys: nil, want: nil},
		{name: "不足一批", keys: make([]string, 2), want: []int{2}},
		{name: "正好一批", keys: make([]string, 1000), want: []int{1000}},
		{name: "超过一批", keys: make([]string, 1001), want: []int{1000, 1}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			batches := splitKeys(tc.keys, 1000)
			var sizes []int
			for _, batch := range batches {
				sizes = append(sizes, len(batch))
			}
			t.Logf("批次大小 = %v", sizes)
			if len(sizes) != len(tc.want) {
				t.Fatalf("批次数 = %d, want %d", len(sizes), len(tc.want))
			}
			for i := range sizes {
				if sizes[i] != tc.want[i] {
					t.Fatalf("第 %d 批 = %d, want %d", i, sizes[i], tc.want[i])
				}
			}
		})
	}
}
