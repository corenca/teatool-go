// Package masking 提供业务字段与自由文本的脱敏能力。
//
// 脱敏按"类型 + 格式"两层组织：Type 描述数据是什么（手机号、身份证等），
// MaskFunc 描述怎么脱（保留首尾、整体替换、加盐哈希等）。类型在注册表里
// 绑定一个默认格式，调用方按类型脱敏；需要不同格式时既可注册覆盖，也可用
// Keep 等工厂直接构造函数。
//
// 两个入口的失败策略不同：
//
//   - Mask 按类型名工作，属于"已知字段"场景，采用 fail-closed：类型未注册时
//     返回 RedactedMarker，绝不回显原值。
//   - MaskText 在自由文本中自动识别，属于"未知内容"场景，采用 fail-open：
//     没有命中时原样返回，避免整段文本被误抹。
//
// 脱敏函数按码点处理，中文与 emoji 不会被从字符中间切断；Match 的区间则使用
// 字节下标，与 Go 的切片语义一致。
//
// Masker 的读写由内部锁保护：Register 与 Mask/MaskText 可并发调用，但 Register
// 属于装配期操作，不应出现在热路径上。
package masking

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"regexp"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"
)

// RedactedMarker 是 Mask 遇到未注册类型时的固定输出。
//
// 取值固定为 "[REDACTED]"，不含原值任何片段，便于日志检索与告警规则匹配。
// 该常量属于 fail-closed 策略的一部分：类型拼写错误、配置漏项都会走到这里，
// 调用方看到这串标记就应排查注册表，而不是把它当成脱敏结果。
const RedactedMarker = "[REDACTED]"

// 注册期错误。两个错误都由 Register 返回，调用方用 errors.Is 判断。
var (
	// ErrNilMaskFunc 表示传给 Register 的脱敏函数为 nil。
	//
	// 注册 nil 会让该类型的 Mask 在运行期 panic，因此在装配期就拒绝。
	ErrNilMaskFunc = errors.New("masking: 脱敏函数为 nil")
	// ErrInvalidType 表示传给 Register 的类型名为空字符串。
	//
	// 空类型名无法被 Mask 命中，只会让注册表出现不可达条目，因此直接拒绝。
	ErrInvalidType = errors.New("masking: 类型名为空")
)

// Type 标识需要脱敏的数据类型，是注册表与按名调用的键。
//
// 类型描述"数据是什么"，MaskFunc 描述"怎么脱"；两者分离使得同一类型可以在
// 不同场景使用不同格式（日志保留首尾、导出全遮蔽）。
type Type string

// 内置类型。常量值是注册表的键，也是日志与配置里出现的字符串，改动等于破坏兼容。
//
// 其中带检测器的六种（phone、telephone、id_card、bank_card、email、ip）会被
// MaskText 在自由文本中自动识别；name、address、password、secret 依赖业务语义，
// 只能按类型名用 Mask 显式脱敏。默认格式由 New 装配，逐条见下方各预置函数。
const (
	// TypePhone 是手机号，键为 "phone"。
	//
	// MaskText 按大陆 11 位号段（1[3-9] 开头）识别，默认格式保留前三后四，
	// 例如 "138****5678"。
	TypePhone Type = "phone"
	// TypeTelephone 是固定电话，键为 "telephone"。
	//
	// MaskText 识别 0 开头的区号号码与 400 号码，默认格式保留前三后四。
	TypeTelephone Type = "telephone"
	// TypeIDCard 是居民身份证号，键为 "id_card"。
	//
	// MaskText 识别 18 位（末位可为 X）与 15 位号码，默认格式保留前六后四，
	// 例如 "110101********1234"。
	TypeIDCard Type = "id_card"
	// TypeBankCard 是银行卡号，键为 "bank_card"。
	//
	// MaskText 识别 13~19 位数字并用 Luhn 校验过滤伪号，默认格式保留前六后四。
	TypeBankCard Type = "bank_card"
	// TypeEmail 是电子邮箱，键为 "email"。
	//
	// MaskText 识别完整的 user@domain 形态，默认格式保留本地部分首字符与完整
	// 域名，例如 "a****@example.com"。
	TypeEmail Type = "email"
	// TypeName 是姓名，键为 "name"，默认格式保留首字符，例如 "张*"。
	//
	// 姓名无法从自由文本中可靠识别，MaskText 不会处理它；请用 Mask 按字段脱敏。
	TypeName Type = "name"
	// TypeAddress 是地址，键为 "address"，默认格式保留前六个字符。
	//
	// 地址同样依赖业务语义，MaskText 不识别。地址常以省市区开头，保留前六个
	// 字符足以支撑人工核对，又不会暴露门牌号。
	TypeAddress Type = "address"
	// TypeIP 是 IP 地址，键为 "ip"。
	//
	// MaskText 识别 IPv4 与常见 IPv6 写法（不含内嵌 IPv4），默认全遮蔽。
	TypeIP Type = "ip"
	// TypePassword 是密码，键为 "password"，默认全遮蔽。
	//
	// 密码不应出现在任何日志里；这里的遮蔽只作为兜底，MaskText 不识别它。
	TypePassword Type = "password"
	// TypeSecret 是通用密文（令牌、密钥、连接串口令等），键为 "secret"。
	//
	// 默认全遮蔽，用于那些没有专门类型、但一旦落盘就必须抹掉的字段。
	TypeSecret Type = "secret"
)

// 预置场景的默认脱敏函数。
//
// 这些函数既可直接调用，也是 New 装配注册表时的默认格式来源；修改此处即
// 同时改变两条路径的行为，不会出现按名调用与直接调用不一致。
//
// 预置只提供最常用的保留位，业务需要不同格式时用 Keep 等工厂自行构造，或
// 通过 Register 覆盖注册表中的同名类型。

// Phone 返回手机号的默认脱敏函数，保留前三后四，如 "138****5678"。
func Phone() MaskFunc {
	return Keep(3, 4)
}

// Telephone 返回固定电话的默认脱敏函数，保留前三后四，如 "057******6666"。
func Telephone() MaskFunc {
	return Keep(3, 4)
}

// IDCard 返回居民身份证号的默认脱敏函数，保留前六后四，如 "110101********1234"。
func IDCard() MaskFunc {
	return Keep(6, 4)
}

// BankCard 返回银行卡号的默认脱敏函数，保留前六后四，如 "622202******0000"。
func BankCard() MaskFunc {
	return Keep(6, 4)
}

// Email 返回电子邮箱的默认脱敏函数，保留本地部分首字符与完整域名，如
// "a****@example.com"。
func Email() MaskFunc {
	return EmailKeepLocal(1)
}

// Name 返回姓名的默认脱敏函数，保留首字符，如 "张*"。
func Name() MaskFunc {
	return Keep(1, 0)
}

// Address 返回地址的默认脱敏函数，保留前六个字符，如 "浙江省杭州市************"。
func Address() MaskFunc {
	return Keep(6, 0)
}

// IP 返回 IP 地址的默认脱敏函数，全遮蔽，输出与原值等长。
func IP() MaskFunc {
	return Keep(0, 0)
}

// Password 返回密码的默认脱敏函数，全遮蔽；需要完全不留痕时改用 Replace("")。
func Password() MaskFunc {
	return Keep(0, 0)
}

// Secret 返回通用密文的默认脱敏函数，全遮蔽，用于令牌、AK/SK 等字段。
func Secret() MaskFunc {
	return Keep(0, 0)
}

// MaskFunc 对单个字符串执行脱敏。
//
// 约定：对任意输入（空串、格式不符的值、非法 UTF-8）都必须返回结果，不得
// panic；无法脱敏时返回全遮蔽，绝不回显原值。任何 func(string) string 都可
// 直接传入 Register，无需显式转换。
type MaskFunc func(value string) string

// Keep 返回保留首尾各若干码点、中间以 '*' 遮蔽的脱敏函数。
//
// 例如 Keep(3, 4)("13812345678") 得到 "138****5678"。prefix 与 suffix 为
// 负数时按 0 处理；两者之和覆盖整个字符串时退化为全遮蔽，避免保留区重叠。
func Keep(prefix, suffix int) MaskFunc {
	return KeepWithChar(prefix, suffix, '*')
}

// KeepWithChar 与 Keep 相同，但可自定义掩码字符；char 为零值时回退为 '*'。
func KeepWithChar(prefix, suffix int, char rune) MaskFunc {
	if prefix < 0 {
		prefix = 0
	}
	if suffix < 0 {
		suffix = 0
	}
	if char == 0 {
		char = '*'
	}

	return func(value string) string {
		runes := []rune(value)
		length := len(runes)
		if length == 0 {
			return ""
		}
		if prefix+suffix >= length {
			return strings.Repeat(string(char), length)
		}

		head := string(runes[:prefix])
		middle := strings.Repeat(string(char), length-prefix-suffix)
		tail := string(runes[length-suffix:])
		return head + middle + tail
	}
}

// Replace 返回将整个值替换为固定文本的脱敏函数。
//
// text 为空字符串时等价于删除原值。替换文本不含原值任何信息，适用于密码、
// 令牌等不需要保留可辨识片段的字段。
func Replace(text string) MaskFunc {
	return func(string) string {
		return text
	}
}

// Hash 返回以加盐 SHA-256 摘要替换原值的脱敏函数，输出形如
// "sha256:<hex>"。
//
// 同一盐与同一输入恒定产生同一输出，可用于跨记录关联；盐的作用是抵御彩虹
// 表，语义上不是消息认证码，需要防篡改时应使用带密钥的 HMAC 方案。
func Hash(salt []byte) MaskFunc {
	return func(value string) string {
		digest := sha256.New()
		digest.Write(salt)
		digest.Write([]byte(value))
		return "sha256:" + hex.EncodeToString(digest.Sum(nil))
	}
}

// EmailKeepLocal 返回保留邮箱本地部分前 keep 个码点、域名原样保留的脱敏函数。
//
// 例如 EmailKeepLocal(1)("alice@example.com") 得到 "a****@example.com"。
// 本地部分长度不超过 keep 时整体遮蔽，避免短地址（如 "a@x.com"）被完整暴露；
// 值中不含 '@' 时按普通 Keep(keep, 0) 处理。
func EmailKeepLocal(keep int) MaskFunc {
	keepLocal := Keep(keep, 0)

	return func(value string) string {
		at := strings.LastIndex(value, "@")
		if at <= 0 || at == len(value)-1 {
			return keepLocal(value)
		}
		return keepLocal(value[:at]) + value[at:]
	}
}

// Match 描述识别器在文本中的一次命中。
//
// Start 与 End 是字节下标（左闭右开），与切片表达式 text[Start:End] 直接对应；
// Value 为命中原文。
type Match struct {
	Start int
	End   int
	Value string
}

// Detector 在文本中定位全部敏感片段。
//
// 返回的片段必须满足 Start < End 且落在文本范围内，顺序不限。零宽命中不构成
// 可替换片段，实现应自行忽略。Detector 会被并发调用，必须无副作用。
type Detector func(text string) []Match

// RegexOption 是 Regex 的可选配置，用于补充 Go 正则不支持的前后边界断言。
//
// 选项按传入顺序应用到内部配置，后传入者覆盖先传入者；选项值为 nil 时跳过，
// 因此可以安全地透传可能为空的配置。具体选项见 NotPrecededBy 与 NotFollowedBy。
type RegexOption func(*regexConfig)

// regexConfig 保存正则识别器的边界约束。
type regexConfig struct {
	notPrecededBy string
	notFollowedBy string
}

// NotPrecededBy 要求命中片段的前一个码点不在 set 中。
//
// Go 的正则引擎不支持前后向断言，边界条件改由匹配后的码点检查完成。例如
// NotPrecededBy("0123456789") 等价于 Python 正则里的 (?<!\d)。
func NotPrecededBy(set string) RegexOption {
	return func(config *regexConfig) {
		config.notPrecededBy = set
	}
}

// NotFollowedBy 要求命中片段的后一个码点不在 set 中。
//
// set 是字符集合，按码点逐个比较，不是正则。用于排除"更长的数字串被截断命中"
// 的情况，例如 NotFollowedBy("0123456789") 等价于 Python 正则里的 (?!\d)。
// set 为空字符串时不做检查。
func NotFollowedBy(set string) RegexOption {
	return func(config *regexConfig) {
		config.notFollowedBy = set
	}
}

// Regex 以正则表达式构造识别器。
//
// pattern 编译失败时返回错误；options 用于补充边界断言。命中区间必须非零宽，
// 零宽匹配一律忽略。
func Regex(pattern string, opts ...RegexOption) (Detector, error) {
	compiled, err := regexp.Compile(pattern)
	if err != nil {
		return nil, fmt.Errorf("masking: 编译正则 %q: %w", pattern, err)
	}

	var config regexConfig
	for _, opt := range opts {
		if opt != nil {
			opt(&config)
		}
	}

	return func(text string) []Match {
		indexes := compiled.FindAllStringIndex(text, -1)
		matches := make([]Match, 0, len(indexes))
		for _, index := range indexes {
			start, end := index[0], index[1]
			if start >= end {
				continue
			}
			if config.notPrecededBy != "" && precededBySet(text, start, config.notPrecededBy) {
				continue
			}
			if config.notFollowedBy != "" && followedBySet(text, end, config.notFollowedBy) {
				continue
			}
			matches = append(matches, Match{Start: start, End: end, Value: text[start:end]})
		}
		return matches
	}, nil
}

// rule 是注册表中的一条规则。
type rule struct {
	mask     MaskFunc
	detector Detector // nil 表示不参与自动识别
	priority int
}

// RuleOption 是 Register 的可选配置，用于补充检测器与优先级。
//
// 不传任何选项时，注册的规则只参与 Mask 按名脱敏，不参与 MaskText 自动识别，
// 优先级为 0。选项值为 nil 时跳过。具体选项见 WithDetector 与 WithPriority。
type RuleOption func(*rule)

// WithDetector 让注册的类型参与 MaskText 的自动识别。
//
// detector 为 nil 时等价于不传该选项（该类型只能按名脱敏）。detector 会被并发
// 调用，实现必须无副作用；可用 Regex 构造，也可自行实现。优先级默认 0，通常
// 需要配合 WithPriority 指定，否则会输给内置规则。
func WithDetector(detector Detector) RuleOption {
	return func(r *rule) {
		r.detector = detector
	}
}

// WithPriority 设置自动识别命中重叠时的优先级，数值大者优先。
//
// 内置规则的优先级为：身份证 200 > 银行卡 150 > 手机号 100 > 固话 90 >
// 邮箱 80 > IP 50。未显式设置的自定义规则优先级为 0。
func WithPriority(priority int) RuleOption {
	return func(r *rule) {
		r.priority = priority
	}
}

// Masker 是脱敏编排器，持有类型到规则的映射。
//
// 零值不可用，必须通过 New 构造。Masker 可安全地被多个 goroutine 共享。
type Masker struct {
	mu    sync.RWMutex
	rules map[Type]rule
}

// New 构造脱敏器，并装入全部内置类型的默认规则。
//
// 返回的 Masker 已可直接使用：十个内置类型都能被 Mask 命中，其中 phone、
// telephone、id_card、bank_card、email、ip 带检测器，参与 MaskText 自动识别。
// 需要不同格式或额外类型时再调用 Register 覆盖。没有参数，也不会失败；每次
// 调用返回互不影响的新实例。包级便捷函数使用包内单独的默认实例。
func New() *Masker {
	return &Masker{
		rules: map[Type]rule{
			TypePhone:     {mask: Phone(), detector: phoneDetector, priority: 100},
			TypeTelephone: {mask: Telephone(), detector: telephoneDetector, priority: 90},
			TypeIDCard:    {mask: IDCard(), detector: idCardDetector, priority: 200},
			TypeBankCard:  {mask: BankCard(), detector: bankCardDetector, priority: 150},
			TypeEmail:     {mask: Email(), detector: emailDetector, priority: 80},
			TypeIP:        {mask: IP(), detector: ipDetector, priority: 50},
			TypeName:      {mask: Name()},
			TypeAddress:   {mask: Address()},
			TypePassword:  {mask: Password()},
			TypeSecret:    {mask: Secret()},
		},
	}
}

// Register 注册或覆盖一个类型的脱敏规则。
//
// 同名类型会被覆盖，既可用于替换内置格式，也可用于新增业务类型。类型名为空
// 返回 ErrInvalidType，脱敏函数为 nil 返回 ErrNilMaskFunc。
func (m *Masker) Register(typ Type, mask MaskFunc, opts ...RuleOption) error {
	if typ == "" {
		return ErrInvalidType
	}
	if mask == nil {
		return ErrNilMaskFunc
	}

	registered := rule{mask: mask}
	for _, opt := range opts {
		if opt != nil {
			opt(&registered)
		}
	}

	m.mu.Lock()
	m.rules[typ] = registered
	m.mu.Unlock()
	return nil
}

// Mask 按类型名对单个值脱敏。
//
// 类型未注册时返回 RedactedMarker（fail-closed），空值返回空串。脱敏函数在
// 持有锁之外的代码路径上调用，规则实现内再调用 Register 不会死锁。
func (m *Masker) Mask(typ Type, value string) string {
	m.mu.RLock()
	registered, ok := m.rules[typ]
	m.mu.RUnlock()

	if !ok {
		return RedactedMarker
	}
	return registered.mask(value)
}

// MaskText 对自由文本自动识别并脱敏，采用 fail-open：没有命中时原样返回。
//
// 全部规则的检测器都会被扫描，重叠区间按"优先级高、起点靠前、区间长"的顺序
// 贪心消解，保证身份证等长区间不会被银行卡等子串规则拆开。命中片段使用所属
// 类型的默认格式，例如日志中的手机号输出为 "138****5678" 而非全星号。
func (m *Masker) MaskText(text string) string {
	if text == "" {
		return text
	}

	// 先快照规则再释放锁，避免在调用检测器期间阻塞注册。
	m.mu.RLock()
	rules := make([]rule, 0, len(m.rules))
	for _, registered := range m.rules {
		if registered.detector != nil {
			rules = append(rules, registered)
		}
	}
	m.mu.RUnlock()

	candidates := make([]candidate, 0, len(rules))
	for _, registered := range rules {
		for _, match := range registered.detector(text) {
			if match.Start < 0 || match.End > len(text) || match.Start >= match.End {
				continue
			}
			candidates = append(candidates, candidate{
				match:    match,
				mask:     registered.mask,
				priority: registered.priority,
			})
		}
	}
	if len(candidates) == 0 {
		return text
	}

	sort.SliceStable(candidates, func(i, j int) bool {
		left, right := candidates[i], candidates[j]
		if left.priority != right.priority {
			return left.priority > right.priority
		}
		if left.match.Start != right.match.Start {
			return left.match.Start < right.match.Start
		}
		return left.match.End > right.match.End
	})

	selected := make([]candidate, 0, len(candidates))
	for _, current := range candidates {
		if overlapsAny(selected, current.match) {
			continue
		}
		selected = append(selected, current)
	}

	sort.SliceStable(selected, func(i, j int) bool {
		return selected[i].match.Start < selected[j].match.Start
	})

	var builder strings.Builder
	builder.Grow(len(text))
	position := 0
	for _, current := range selected {
		builder.WriteString(text[position:current.match.Start])
		builder.WriteString(current.mask(current.match.Value))
		position = current.match.End
	}
	builder.WriteString(text[position:])
	return builder.String()
}

// MaskFields 按字段映射对结构化数据脱敏，返回新的 map，不修改入参。
//
// fields 是字段名到类型的映射，未声明的字段原样保留。同一份映射会递归应用到
// 嵌套的 map[string]any 与 []any；命中字段的值先转为字符串再脱敏，因此数字、
// 字节切片等类型统一输出为字符串形态。结构体不做反射处理，需要时先转为 map。
func (m *Masker) MaskFields(data map[string]any, fields map[string]Type) map[string]any {
	result := make(map[string]any, len(data))
	for key, value := range data {
		if typ, ok := fields[key]; ok {
			result[key] = m.Mask(typ, stringify(value))
			continue
		}
		result[key] = m.maskNested(value, fields)
	}
	return result
}

// Types 返回当前注册的全部类型，按字典序排列；返回新切片，修改不影响注册表。
func (m *Masker) Types() []Type {
	m.mu.RLock()
	types := make([]Type, 0, len(m.rules))
	for typ := range m.rules {
		types = append(types, typ)
	}
	m.mu.RUnlock()

	slices.Sort(types)
	return types
}

// defaultMasker 承载包级便捷函数。
var defaultMasker = New()

// Mask 使用包级默认脱敏器按类型名脱敏；类型未注册时返回 RedactedMarker。
func Mask(typ Type, value string) string {
	return defaultMasker.Mask(typ, value)
}

// MaskText 使用包级默认脱敏器对自由文本脱敏；没有命中时原样返回。
func MaskText(text string) string {
	return defaultMasker.MaskText(text)
}

// MaskFields 使用包级默认脱敏器按字段映射脱敏，返回新 map。
func MaskFields(data map[string]any, fields map[string]Type) map[string]any {
	return defaultMasker.MaskFields(data, fields)
}

// Register 在包级默认脱敏器上注册规则，通常在 init 阶段调用一次。
func Register(typ Type, mask MaskFunc, opts ...RuleOption) error {
	return defaultMasker.Register(typ, mask, opts...)
}

// Types 返回包级默认脱敏器已注册的全部类型，按字典序排列。
func Types() []Type {
	return defaultMasker.Types()
}

// candidate 是带脱敏方式的一次自动识别命中。
type candidate struct {
	match    Match
	mask     MaskFunc
	priority int
}

// overlapsAny 报告 match 是否与已选中的任一命中重叠。
func overlapsAny(selected []candidate, match Match) bool {
	for _, current := range selected {
		if match.Start < current.match.End && current.match.Start < match.End {
			return true
		}
	}
	return false
}

// maskNested 递归处理嵌套容器，非容器值原样返回。
func (m *Masker) maskNested(value any, fields map[string]Type) any {
	switch typed := value.(type) {
	case map[string]any:
		return m.MaskFields(typed, fields)
	case []any:
		result := make([]any, len(typed))
		for i, item := range typed {
			result[i] = m.maskNested(item, fields)
		}
		return result
	default:
		return value
	}
}

// stringify 把待脱敏值转为字符串，nil 视为空串。
func stringify(value any) string {
	switch typed := value.(type) {
	case nil:
		return ""
	case string:
		return typed
	case []byte:
		return string(typed)
	case fmt.Stringer:
		return typed.String()
	default:
		return fmt.Sprint(typed)
	}
}

// precededBySet 报告 index 位置的前一个码点是否属于 set。
func precededBySet(text string, index int, set string) bool {
	if index <= 0 {
		return false
	}
	r, _ := utf8.DecodeLastRuneInString(text[:index])
	return strings.ContainsRune(set, r)
}

// followedBySet 报告 index 位置的下一个码点是否属于 set。
func followedBySet(text string, index int, set string) bool {
	if index >= len(text) {
		return false
	}
	r, _ := utf8.DecodeRuneInString(text[index:])
	return strings.ContainsRune(set, r)
}
