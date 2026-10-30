# masking 使用说明

业务字段与自由文本脱敏。把"数据是什么"（`Type`）和"怎么脱"（`MaskFunc`）分开，
内置十种常见业务类型，也可以注册自己的类型与识别规则。

适用场景：

- 写日志、发告警、导出报表前，把手机号、身份证、银行卡等抹掉。
- 处理第三方回调或用户留言这类自由文本，自动找出其中的敏感片段。
- 对结构化数据（`map[string]any`）按字段名批量脱敏。

不做的事情，避免误用：

- **不做加密**。脱敏不可逆、不保密，只是"看不全"；需要保护数据本身请用 `gm`。
- **不做结构体反射**。结构体先转 `map[string]any` 再交给 `MaskFields`。
- **不保证识别所有格式**。`MaskText` 只认内置的六种可定位类型，姓名、地址、
  密码这类依赖上下文的字段必须按字段名显式脱敏。
- **不做可逆映射**。输出不确定、不可还原，不要用它做"取回原值"的方案。

```bash
go get github.com/corenca/teatool-go
```

## 先选对入口

| 场景 | 入口 | 失败策略 |
| --- | --- | --- |
| 已知字段名（数据库列、JSON 键） | `Mask` / `MaskFields` | fail-closed：类型未注册输出 `[REDACTED]`，绝不回显原值 |
| 未知内容的自由文本（日志、留言） | `MaskText` | fail-open：没命中就原样返回，避免整段文本被误抹 |
| 只要格式化一个字符串 | `Keep` / `Replace` / `Hash` 等工厂 | 纯函数，无状态 |

名称写错、配置漏项时，`Mask` 会输出 `[REDACTED]` 而不是悄悄放过原值——看到这串
标记就应该去查注册表，不要把它当成正常的脱敏结果。

## 内置类型

| 类型常量 | 键 | 默认格式 | 示例 | `MaskText` 自动识别 |
| --- | --- | --- | --- | --- |
| `TypePhone` | `phone` | 保留前三后四 | `138****5678` | 是，`1[3-9]` 开头 11 位 |
| `TypeTelephone` | `telephone` | 保留前三后四 | `057******6666` | 是，区号号码与 400 号码 |
| `TypeIDCard` | `id_card` | 保留前六后四 | `110101********1234` | 是，18 位（末位可为 X）与 15 位 |
| `TypeBankCard` | `bank_card` | 保留前六后四 | `622202******0000` | 是，13~19 位且通过 Luhn 校验 |
| `TypeEmail` | `email` | 保留本地部分首字符 + 完整域名 | `a****@example.com` | 是 |
| `TypeIP` | `ip` | 全遮蔽 | `************` | 是，IPv4 与常见 IPv6 |
| `TypeName` | `name` | 保留首字符 | `张*` | 否 |
| `TypeAddress` | `address` | 保留前六个字符 | `浙江省杭州市************` | 否 |
| `TypePassword` | `password` | 全遮蔽 | `********` | 否 |
| `TypeSecret` | `secret` | 全遮蔽 | `********` | 否 |

星号个数等于被遮蔽的码点数，所以脱敏结果长度与原值一致。全遮蔽同样保留长度；
不想泄露长度时改用 `Replace("***")`。

## 快速上手

```go
package main

import (
	"fmt"

	"github.com/corenca/teatool-go/masking"
)

func main() {
	// 已知字段名：按类型脱敏
	fmt.Println(masking.Mask(masking.TypePhone, "13812345678"))
	fmt.Println(masking.Mask(masking.TypeIDCard, "110101199003071234"))

	// 自由文本：自动识别并替换
	text := "用户手机 13812345678，身份证 110101199003071234，邮箱 alice@example.com，IP 192.168.1.10"
	fmt.Println(masking.MaskText(text))

	// 类型未注册：fail-closed
	fmt.Println(masking.Mask("not_registered", "不能泄露的原值"))
}
```

输出：

```text
138****5678
110101********1234
用户手机 138****5678，身份证 110101********1234，邮箱 a****@example.com，IP ************
[REDACTED]
```

## 场景一：结构化数据按字段脱敏

`MaskFields` 接收字段名到类型的映射，返回新 map，不修改入参。未在映射里声明的
字段原样保留；嵌套的 `map[string]any` 与 `[]any` 会递归应用同一份映射。

```go
data := map[string]any{
	"name":  "张三",
	"phone": "13812345678",
	"profile": map[string]any{
		"email": "alice@example.com",
		"city":  "杭州",
	},
	"tags": []any{
		map[string]any{"contact": "13800001111"},
		"普通用户",
	},
}

fields := map[string]masking.Type{
	"name":  masking.TypeName,
	"phone": masking.TypePhone,
	"email": masking.TypeEmail,
}

fmt.Printf("%v\n", masking.MaskFields(data, fields))
// map[name:张* phone:138****5678 profile:map[city:杭州 email:a****@example.com]
//     tags:[map[contact:13800001111] 普通用户]]
```

注意两点：

- 映射按**字段名**匹配，与层级无关；`tags` 里的 `contact` 没有声明，所以原样输出。
  要脱敏它，把 `"contact": masking.TypePhone` 加进映射。
- 命中字段的值会先转成字符串再脱敏，数字、`[]byte`、`fmt.Stringer` 统一输出为
  字符串形态；如果下游依赖原始类型，请在脱敏前完成类型转换。

## 场景二：日志与自由文本

`MaskText` 扫描全部带检测器的规则，重叠区间按"优先级高、起点靠前、区间长"的顺序
贪心消解，因此身份证这种长区间不会被银行卡等子串规则拆开。

```go
fmt.Println(masking.MaskText("订单号 202610210001 已完成，联系 13812345678"))
// 订单号 202610210001 已完成，联系 138****5678
```

识别是**尽力而为**：

- 12 位连续数字不会命中 11 位手机号，多一位少一位都会脱靶。
- 银行卡候选必须通过 Luhn 校验，编造的号码会被放过。
- 姓名、地址、密码不参与自动识别。
- 自由文本里没有边界信息的号码（如紧贴字母）可能漏识别。

对合规要求严格的字段，用 `MaskFields` 按字段名显式脱敏，不要把 `MaskText` 当唯一
防线。

## 自定义格式

`MaskFunc` 就是 `func(string) string`，任何同签名函数都能直接注册，也可以组合下面
几个工厂：

| 工厂 | 用途 | 示例 |
| --- | --- | --- |
| `Keep(prefix, suffix)` | 保留首尾若干码点，中间用 `*` | `Keep(3, 4)("13812345678")` → `138****5678` |
| `KeepWithChar(prefix, suffix, char)` | 同上，自定掩码字符 | `KeepWithChar(3, 4, '#')("13812345678")` → `138####5678` |
| `Replace(text)` | 整体替换为固定文本 | `Replace("***")("任意值")` → `***` |
| `Hash(salt)` | 加盐 SHA-256，输出 `sha256:<hex>` | 同一输入恒定输出，可跨记录关联 |
| `EmailKeepLocal(keep)` | 保留邮箱本地部分前 N 个码点 | `EmailKeepLocal(1)("alice@example.com")` → `a****@example.com` |

`Keep` 按**码点**处理，中文与 emoji 不会被从字符中间切断。保留位之和覆盖整个字符串
时退化为全遮蔽：`Keep(3, 4)("1234567")` 得到 `*******`。

`Hash` 只适合做关联，不适合当签名：它没有密钥，语义上不是消息认证码。

```go
// 日志场景想保留更少信息：只留后四位
loggerFormat := masking.Keep(0, 4)

// 导出场景全遮蔽，长度也不留
exporterFormat := masking.Replace("[MASKED]")

if err := masking.Register(masking.TypePhone, loggerFormat); err != nil {
	return err
}
fmt.Println(masking.Mask(masking.TypePhone, "13812345678")) // *******5678
```

`Register` 会覆盖同名类型，因此上面的写法影响整个进程；测试里需要隔离时改用
`masking.New()` 自建实例。

## 注册自定义类型与识别器

新增业务类型（比如订单号）时可以顺带声明它如何被自动识别。`Regex` 编译失败会返回
错误；`NotPrecededBy` / `NotFollowedBy` 用来补 Go 正则不支持的前后边界断言，参数是
字符集合而不是正则。

```go
package main

import (
	"fmt"
	"log"

	"github.com/corenca/teatool-go/masking"
)

const (
	orderType  masking.Type = "order_no"
	digits                  = "0123456789"
)

func main() {
	// 订单号：ORD + 12 位数字
	detector, err := masking.Regex(`ORD\d{12}`,
		masking.NotPrecededBy(digits),
		masking.NotFollowedBy(digits),
	)
	if err != nil {
		log.Fatal(err)
	}

	// 优先级 300 高于内置身份证（200），重叠时订单号整段胜出
	err = masking.Register(orderType, masking.Keep(3, 2),
		masking.WithDetector(detector),
		masking.WithPriority(300),
	)
	if err != nil {
		log.Fatal(err)
	}

	fmt.Println(masking.Mask(orderType, "ORD202610210001"))
	fmt.Println(masking.MaskText("订单 ORD202610210001 已支付"))
}
```

输出：

```text
ORD**********01
订单 ORD**********01 已支付
```

参数与约定：

- `Regex(pattern, opts...)`：`pattern` 是 RE2 语法（不支持前后向断言），`opts` 为
  `NotPrecededBy` / `NotFollowedBy`。返回的 `Detector` 命中区间是**字节下标**
  （左闭右开），零宽匹配一律忽略；`Detector` 会被并发调用，实现必须无副作用。
- `WithDetector(detector)`：不传则新类型只能按名脱敏，不参与 `MaskText`。
- `WithPriority(n)`：命中重叠时数值大者优先，内置优先级为身份证 200 > 银行卡 150 >
  手机号 100 > 固话 90 > 邮箱 80 > IP 50；未指定时为 0，会输给全部内置规则。

## 参数速查

| API | 参数 | 返回 / 错误 |
| --- | --- | --- |
| `New()` | 无 | `*Masker`，已装入十个内置类型 |
| `(*Masker).Register(typ, mask, opts...)` | 类型名、脱敏函数、规则选项 | 类型名为空 → `ErrInvalidType`；函数为 nil → `ErrNilMaskFunc` |
| `(*Masker).Mask(typ, value)` | 类型名、原文 | 脱敏结果；类型未注册 → `[REDACTED]` |
| `(*Masker).MaskText(text)` | 自由文本 | 替换敏感片段后的文本；无命中原样返回 |
| `(*Masker).MaskFields(data, fields)` | 字段映射、字段名到类型 | 新 map；命中字段统一输出字符串 |
| `(*Masker).Types()` | 无 | `[]Type`，字典序 |
| `Regex(pattern, opts...)` | RE2 表达式、边界选项 | `Detector`；表达式非法时返回错误 |
| `Keep` / `KeepWithChar` / `Replace` / `Hash` / `EmailKeepLocal` | 见上表 | `MaskFunc`，不会 panic，任何输入都返回结果 |

包级函数 `masking.Mask`、`masking.MaskText`、`masking.MaskFields`、
`masking.Register`、`masking.Types` 等价于在包级默认实例上调用同名方法。

## 并发与安全

- `Masker` 内部用读写锁保护注册表，可以被多个 goroutine 共享；`Mask` 系列与
  `Register` 并发调用是安全的。
- `Register` 属于装配期操作，通常在 `init` 或启动配置阶段调用一次，不要放在热路径。
- 脱敏函数在释放锁之后调用，规则实现里再调 `Register` 不会死锁。
- 预置类型是进程级全局状态：库代码不要随意覆盖内置格式，会影响同进程的其他调用方。
- 日志、告警、导出、埋点都要走同一套规则，否则容易在某条旁路上漏掉。

## 常见错误

**`Mask` 返回 `[REDACTED]`**：类型名没注册。检查是不是用了自定义类型却忘了
`Register`，或者常量拼写与注册值不一致（`Type` 是字符串，编译器不校验）。

**`MaskText` 没脱敏**：先确认该类型是否带检测器（姓名、地址、密码都没有）；再确认
片段是否被更长的数字串包住——多一位数字就会整体脱靶。银行卡还需通过 Luhn 校验。

**整段文本被误抹**：通常是重叠消解按优先级选中了更长的区间。给业务类型设置更高的
`WithPriority`，或改用 `MaskFields` 按字段名脱敏。

**数字字段变成字符串**：`MaskFields` 对命中字段调用 `stringify`，这是设计行为。
需要保留数值类型时先脱敏再转换，或自行控制输出。

**结构体字段没被处理**：本包不做反射。先 `json.Marshal` 成 `map[string]any`，或用
`mapstructure` 之类的工具转成 map。

**哈希结果拿去当签名**：`Hash` 是加盐摘要，不是 HMAC。需要防篡改请用带密钥的
HMAC 方案（`gm.HMACSM3` 或 `crypto/hmac`）。
