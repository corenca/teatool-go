# fsutil 使用说明

业务开发中的文件系统工具：判存、建父目录、上传文件名清洗与安全拼接、单文件
复制/移动、目录内文件遍历。

适用场景：

- 上传接口处理用户提供的文件名，防止目录穿越与非法字符。
- 单个文件的复制、移动，要求"不覆盖、失败不留半成品"。
- 递归收集目录下的全部文件路径，交给后续批处理。

约定：

- **判存类函数跟随符号链接**：`Exists` / `IsFile` / `IsDir` 看到的是链接指向的
  目标；断链视为不存在。
- **复制、移动、遍历不跟随符号链接**，只处理普通文件（regular file）。
- 所有返回 `error` 的函数都用 `%w` 保留底层错误，可用 `errors.Is` 判断。
- 不提供递归删除、目录复制、权限/属主/时间戳保留、原子替换。目录操作请直接用
  `os` / `filepath`。

```bash
go get github.com/corenca/teatool-go
```

## 快速上手

```go
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/corenca/teatool-go/fsutil"
)

func main() {
	ok, err := fsutil.Exists("data/users.csv")
	fmt.Println(ok, err) // 存在 true <nil>，不存在 false <nil>

	// 先建父目录，再写文件
	if err := fsutil.EnsureParent("out/2026/10/report.csv"); err != nil {
		return
	}
	_ = os.WriteFile("out/2026/10/report.csv", []byte("id,name\n"), 0o644)

	// 复制：目标已存在会报错，不覆盖
	if err := fsutil.Copy("data/users.csv", "backup/users.csv"); err != nil {
		if errors.Is(err, os.ErrExist) {
			fmt.Println("目标已存在，跳过")
		}
		return
	}

	// 移动：跨设备时自动回退为复制 + 删除源
	if err := fsutil.Move("out/2026/10/report.csv", "out/latest.csv"); err != nil {
		return
	}

	files, err := fsutil.ListFiles("out")
	fmt.Println(files, err) // [/绝对路径/out/latest.csv] <nil>
}
```

## 判存三兄弟

| 函数 | 返回 true 的条件 | 路径不存在时 |
| --- | --- | --- |
| `Exists(path)` | 路径存在（跟随链接） | `(false, nil)` |
| `IsFile(path)` | 存在且是普通文件 | `(false, nil)` |
| `IsDir(path)` | 存在且是目录 | `(false, nil)` |

关键区别是**"不存在"与"无法判断"**：

```go
ok, err := fsutil.Exists("/root/secret.txt")
switch {
case err != nil:
	// 权限不足等：无法判断，不能当成"不存在"
case !ok:
	// 确实不存在
}
```

权限不足、路径中间是普通文件（`a.txt/b`）都返回 `(false, err)`，不会伪装成
"不存在"。目录、设备、FIFO、socket 对 `IsFile` 返回 false 且 err 为 nil。

## 建父目录

`EnsureParent(filePath)` 为**文件路径**准备父目录，等价于对 `filepath.Dir(filePath)`
做 `MkdirAll(dir, 0o755)`：

```go
fsutil.EnsureParent("a/b/c.txt") // 创建 a/b
fsutil.EnsureParent("a/b/")      // "a/b/" 被当作文件路径，创建 a/b
fsutil.EnsureParent("a.txt")     // 没有目录部分，什么都不做
fsutil.EnsureParent("")          // 返回错误
```

## 上传文件名：清洗 + 拼接 + 去重

三步组合，缺一不可（片段，需自行 `import "path/filepath"`）：

```go
func saveUpload(dir, original string, data []byte) (string, error) {
	// 1. 清洗：去掉路径、控制字符、Windows 非法字符，处理保留设备名
	name := fsutil.SanitizeFileName(original)

	// 2. 拼接：拒绝越出基准目录的输入（纯词法校验）
	path, err := fsutil.JoinWithin(dir, name)
	if err != nil {
		return "", err // errors.Is(err, fsutil.ErrOutsideBase)
	}

	// 3. 去重：目标已存在时改用 name_1.ext、name_2.ext……
	path, err = fsutil.UniqueName(filepath.Dir(path), filepath.Base(path))
	if err != nil {
		return "", err
	}

	// 4. 自己用 O_EXCL 写入，堵住 UniqueName 的检查-创建竞态
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return "", err
	}
	defer f.Close()
	if _, err := f.Write(data); err != nil {
		return "", err
	}
	return path, nil
}
```

`SanitizeFileName` 的规则（所有平台一致执行）：

| 输入 | 输出 | 说明 |
| --- | --- | --- |
| `../../etc/passwd` | `.._.._etc_passwd` | `/` 与 `\` 变下划线 |
| `a\b:c*d?.txt` | `a_b_c_d_.txt` | Windows 非法字符变下划线 |
| `"  report . . "` | `report` | 去掉首尾空格与结尾的点 |
| `CON` | `_CON` | Windows 保留设备名前加下划线 |
| `.env` | `.env` | 前导点不算扩展名 |
| `"   "` | `unnamed` | 空结果回退 |
| `中文 文件.txt` | `中文 文件.txt` | 中文与空格合法，原样保留 |

不做两件事：**不截断长度**（超长名需自行限制，多数文件系统单段上限 255 字节）、
**不做 Unicode 归一化**（NFC/NFD 形式差异需业务处理）。

`JoinWithin(base, name)` 是**纯词法**校验：拒绝绝对路径、空串、`..` 越界
（如 `a/../../b`），接受 `./ok.txt`、`sub/ok.txt`。它**不解析符号链接**——如果
`base/link` 是指向 `/etc` 的链接，`JoinWithin(base, "link/passwd")` 仍会通过。
需要解析级防护时用 `os.Root`（Go 1.24+）。

`UniqueName(dir, name)` 依次探测 `name`、`stem_1.ext`、`stem_2.ext`……最多 10000 次，
用 `os.Lstat` 判断，目录与断链符号链接都算占用。**只保证"当时不存在"**，并发下
两个请求可能拿到同一个名字，所以最后一步必须用 `O_EXCL` 打开。

## Copy 与 Move

```go
err := fsutil.Copy("a.txt", "dir/b.txt")  // dst 父目录不存在时自动创建
err = fsutil.Move("dir/b.txt", "c.txt")
```

| 行为 | `Copy` | `Move` |
| --- | --- | --- |
| 源必须是普通文件 | 是，不跟随符号链接 | 是，不跟随符号链接 |
| 目标已存在 | 报错，`errors.Is(err, os.ErrExist)` | 报错，同上 |
| 父目录不存在 | 自动创建（0755） | 自动创建（0755） |
| 权限位 | 精确还原为源文件权限 | 同 `Copy`（跨设备回退时） |
| 时间戳 | 不保留 mtime | 同 `Copy`（回退时） |
| 失败处理 | 删除本次创建的目标文件 | 复制成功但删源失败时保留目标并报错 |
| 原子性 | 非原子，写完才可见 | 同设备 `os.Rename` 原子；跨设备回退为非原子 |

`Move` 先尝试 `os.Rename`，失败后回退为 `Copy` + 删除源文件，以覆盖跨设备场景。
回退路径上"复制成功、删除源失败"是可能的，此时目标文件保留并返回明确错误——
调用方需自行判断是否重试删除。

Windows 上 `os.Chmod` 只能切换只读属性，权限还原以平台能力为限。

## 遍历目录

```go
files, err := fsutil.ListFiles("data")
```

- `root` 必须是目录；`root` 本身是符号链接时跟随。
- **不跟随任何子目录符号链接**，符号链接、设备、FIFO、socket 都不计入结果。
- 返回**绝对路径**，按字典序**全局排序**（不是逐目录排序）。
- 没有文件时返回非 nil 空切片，可直接 `range`。
- 任一层读取失败立即返回错误，**不返回部分结果**——避免调用方拿着残缺清单继续
  处理。

## 参数与错误

| API | 参数 | 返回 / 错误 |
| --- | --- | --- |
| `Exists` / `IsFile` / `IsDir(path)` | 任意路径 | `(bool, error)`；不存在 → `(false, nil)`；无法判断 → `(false, err)` |
| `EnsureParent(filePath)` | 文件路径 | 空串返回错误；其余返回建目录的底层错误 |
| `JoinWithin(base, name)` | 基准目录、相对路径 | 拼好的路径；越界 → `ErrOutsideBase`；`base` 空 → 错误 |
| `SanitizeFileName(name)` | 任意字符串 | 安全的单段文件名，不返回错误 |
| `UniqueName(dir, name)` | 目录、**单段**文件名 | 不冲突的完整路径；名字含 `/\` → 错误；探测满 10000 次 → 错误 |
| `Copy(src, dst)` | 源文件、目标文件路径 | 目标已存在 → `fs.ErrExist`；源非普通文件 → 错误 |
| `Move(src, dst)` | 同上 | 同上 |
| `ListFiles(root)` | 目录 | 绝对路径切片（字典序）；非目录或读取失败 → 错误 |

## 常见错误

**把 `err != nil` 当成"文件不存在"**：权限不足是"无法判断"，不是"不存在"。只有
`ok == false && err == nil` 才是确定不存在。

**以为 `Copy` / `Move` 会覆盖**：不会。目标已存在一律报错（`fs.ErrExist`）。需要
覆盖时自己先删除或改用 `os.Rename`。

**`UniqueName` 在高并发下撞名**：它只做存在性检查，不做预留。最终写入必须用
`O_EXCL`，或在上层加锁。

**`JoinWithin` 当成安全边界**：它只做词法校验，不解析符号链接。上传目录里若能
创建符号链接，需要改用 `os.Root`。

**清洗后的文件名超长**：`SanitizeFileName` 不截断。超长名落盘会返回
`ENAMETOOLONG`，需在调用前按字节数截断（注意 UTF-8 边界）。

**`ListFiles` 漏掉了软链接目录**：设计如此。需要跟随时自行实现遍历并显式处理
链接，注意防环。

**`ListFiles` 返回路径不是相对路径**：返回的是绝对路径，便于跨工作目录使用。需要
相对路径时用 `filepath.Rel(root, p)`。

**移动大文件时程序卡住**：`Move` 跨设备时是真实复制，耗时与文件大小成正比。大文件
或跨网络存储请放到后台任务，不要放在请求链路里。
