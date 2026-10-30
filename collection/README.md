# collection 使用说明

业务代码里反复出现的集合与切片操作：去重集合、保序集合、集合运算、按份切分、
保序去重。

适用场景：

- 需要判断"某值是否已处理过"、求两个 ID 列表的并集/差集。
- 需要稳定输出顺序（去重后仍按首次出现排列）。
- 把大切片按每份条数或总份数拆分给多个 goroutine / 多个请求。
- 去掉重复项且保持原有顺序。

约束与边界：

- 元素类型必须 `comparable`（可用 `==` 比较）；集合不支持 `[]byte`、map、切片作元素。
- **不是并发安全的**，多 goroutine 同时读写需要调用方加锁。
- **两个集合类型只能通过指针使用**，不要按值拷贝结构体；需要独立副本用 `Clone`。
- 不提供排序、过滤、分组等通用切片工具，那些用 `slices` / `maps` 标准库。

```bash
go get github.com/corenca/teatool-go
```

## 快速上手

```go
package main

import (
	"fmt"

	"github.com/corenca/teatool-go/collection"
)

func main() {
	set := collection.NewSet(3, 1, 2, 1, 3)

	fmt.Println("len:", set.Len())              // 3
	fmt.Println("contains 2:", set.Contains(2)) // true
	fmt.Println("values:", set.Values())        // 顺序不保证，如 [3 1 2]

	a := collection.NewSet(1, 2, 3)
	b := collection.NewSet(3, 4)

	fmt.Println("union:", a.Union(b).Values())        // 含 1 2 3 4
	fmt.Println("intersect:", a.Intersect(b).Values()) // [3]
	fmt.Println("diff:", a.Difference(b).Values())     // [1 2]
}
```

`Union` / `Intersect` / `Difference` 都返回新集合，不修改 `a`、`b`；`Equal` 只比较
元素、不比较顺序。

## Set 还是 OrderedSet

| | `Set` | `OrderedSet` |
| --- | --- | --- |
| 顺序 | 无，`Values` 顺序随机 | 首次插入顺序 |
| 底层 | 一个 map | map + 顺序切片 |
| 删除代价 | O(1) | O(n)，保持其余元素相对顺序 |
| 适用 | 只关心"有没有" | 输出要稳定、可预期 |

选择判据：结果要落库、返回给前端、写日志——用 `OrderedSet`；只在内存里判重、求
差集——用 `Set`。

```go
ordered := collection.NewOrderedSet("c", "a", "b", "a")
fmt.Println(ordered.Values()) // [c a b]

ordered.Remove("a")
ordered.Add("a")
fmt.Println(ordered.Values()) // [c b a]：删除后重新加入排到末尾
```

`OrderedSet.Equal` 与 `Set.Equal` 语义一致：`NewOrderedSet(1, 2).Equal(NewOrderedSet(2, 1))`
返回 `true`，需要比较顺序时用 `Values` 自行对比。

## 切片工具

```go
bySize, _ := collection.SplitBySize([]int{1, 2, 3, 4, 5}, 2)
fmt.Println(bySize) // [[1 2] [3 4] [5]]

byParts, _ := collection.SplitByParts([]int{1, 2, 3, 4, 5}, 3)
fmt.Println(byParts) // [[1 2] [3 4] [5]]，长度 2 2 1

fmt.Println(collection.Dedupe([]string{"a", "b", "a", "c", "b"})) // [a b c]
```

| 函数 | 参数 | 返回 / 错误 |
| --- | --- | --- |
| `SplitBySize(s, size)` | `s` 源切片；`size` 每份最大元素数 | 份数为 `ceil(len/size)`；`size < 1` 返回错误；空输入返回空切片 |
| `SplitByParts(s, parts)` | `s` 源切片；`parts` 期望份数 | 余数从靠前的份开始各多 1，任意两份长度相差不超过 1；`parts < 1` 返回错误 |
| `Dedupe(s)` | `s` 源切片 | 保序去重后的新切片，底层数组独立 |

两个切分函数的细节：

- **`parts` 大于元素个数时只产出非空份**：`SplitByParts([]int{1,2}, 5)` 得到
  `[[1] [2]]`，不会补空份。
- **子切片与源切片共享底层数组**，但容量已裁剪到各自长度。所以对某一份 `append`
  不会覆写相邻份；但直接修改 `parts[0][0]` 会改到源切片。需要隔离时先 clone 源切片。
- 两个函数都不修改源切片，只做视图切分，不产生元素拷贝——切分大切片几乎零成本。

## 常用写法

```go
// 批量处理时按并发数切分
batches, err := collection.SplitByParts(ids, 8)
if err != nil {
	return err
}
for _, batch := range batches {
	process(batch)
}

// 多来源 ID 合并去重，保持首次出现顺序
all := collection.NewOrderedSet[int]()
for _, chunk := range chunks {
	all.Add(chunk...)
}

// 找出新增与删除
added := newIDs.Difference(oldIDs).Values()
removed := oldIDs.Difference(newIDs).Values()

// 幂等：只有未处理过才处理
if !seen.Contains(id) {
	seen.Add(id)
	handle(id)
}
```

## 参数与错误

| API | 参数 | 返回 / 错误 |
| --- | --- | --- |
| `NewSet(values...)` / `NewOrderedSet(values...)` | 可变参数，重复项自动去重 | 可直接使用的 `*Set` / `*OrderedSet` |
| `Add(values...)` | 可变参数 | 无返回，不失败 |
| `Remove(values...)` | 可变参数，不存在的值忽略 | 无返回，不失败 |
| `Contains(v)` / `Len()` | 单个元素 / 无 | `bool` / `int` |
| `Clear()` | 无 | 无返回；清空后仍可继续使用 |
| `Values()` | 无 | 新切片；空集合返回空切片（非 nil） |
| `Clone()` | 无 | 独立副本，互不影响 |
| `Union / Intersect / Difference` | 另一个集合（不能为 nil） | 新集合，不修改入参 |
| `Equal` | 另一个集合（不能为 nil） | `bool`，不比较顺序 |
| `SplitBySize` / `SplitByParts` | 源切片 + 正整数 | 子切片列表；参数非法返回 error |
| `Dedupe` | 源切片 | 去重后的新切片 |

`SplitBySize` / `SplitByParts` 的参数非法时返回 error 而不是 panic，这与包内其他
纯集合操作保持一致：只要没有"必然失败"的输入，就不制造错误通道。

## 常见错误

**按值拷贝集合**：`s2 := *s1` 之后 `s2.Clear()` 不影响 `s1`（`Set` 只共享 map，
`OrderedSet` 甚至会让 map 与顺序切片脱节）。集合一律用指针传递，需要副本用 `Clone`。

**nil 指针**：`var s *collection.Set[int]` 直接调方法会 panic。用
`NewSet[int]()` 或可寻址的零值变量 `var s collection.Set[int]` 后取 `&s`。

**并发写导致 panic**：集合内部没有锁，多 goroutine 同时 `Add` 会触发
`concurrent map writes`。要么加锁，要么每个 goroutine 用独立集合再合并。

**`Set[any]` 存入切片**：`any` 本身可比较，但运行时存入 `[]int{}`、map 这类不可哈希
的动态值，`Add` 会 panic。元素类型不确定时先用 `fmt.Sprint` 转成字符串。

**`SplitByParts` 份数不符预期**：`parts` 大于元素个数时只返回非空份；余数从**靠前**
的份开始分配，所以前几份会多 1 个。

**修改切分结果影响了源数据**：子切片共享底层数组。需要独立数据时先拷贝源切片，或在
切分后对每份做 `slices.Clone`。

**`Values()` 想排序**：`Set.Values` 顺序不保证。要稳定输出用 `OrderedSet`，或对结果
调用 `slices.Sort`。
