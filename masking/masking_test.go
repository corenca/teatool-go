package masking_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sync"
	"testing"

	"github.com/corenca/teatool-go/masking"
)

func TestKeep(t *testing.T) {
	tests := []struct {
		name           string
		value          string
		prefix, suffix int
		want           string
	}{
		{name: "手机号保留前三后四", value: "13812345678", prefix: 3, suffix: 4, want: "138****5678"},
		{name: "身份证保留前六后四", value: "110101199003071234", prefix: 6, suffix: 4, want: "110101********1234"},
		{name: "中文按码点切分", value: "张三丰", prefix: 1, suffix: 1, want: "张*丰"},
		{name: "emoji 按码点切分", value: "a😀b", prefix: 1, suffix: 1, want: "a*b"},
		{name: "零参数即全遮蔽", value: "abc", want: "***"},
		{name: "保留位覆盖全长时整体遮蔽", value: "abc", prefix: 3, suffix: 4, want: "***"},
		{name: "空字符串原样返回", value: "", prefix: 3, suffix: 4, want: ""},
		{name: "负数按零处理", value: "abcd", prefix: -1, suffix: -1, want: "****"},
		{name: "只保留前缀", value: "abcdef", prefix: 2, want: "ab****"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := masking.Keep(tt.prefix, tt.suffix)(tt.value)
			if got != tt.want {
				t.Fatalf("Keep(%d, %d)(%q) = %q, 期望 %q", tt.prefix, tt.suffix, tt.value, got, tt.want)
			}
		})
	}
}

func TestKeepWithChar(t *testing.T) {
	if got, want := masking.KeepWithChar(1, 1, '●')("abc"), "a●c"; got != want {
		t.Fatalf("自定义掩码字符 = %q, 期望 %q", got, want)
	}

	if got, want := masking.KeepWithChar(0, 0, 0)("ab"), "**"; got != want {
		t.Fatalf("零值掩码字符应回退为 '*'，得到 %q", got)
	}
}

func TestReplace(t *testing.T) {
	if got, want := masking.Replace("[HIDDEN]")("secret"), "[HIDDEN]"; got != want {
		t.Fatalf("Replace = %q, 期望 %q", got, want)
	}

	if got, want := masking.Replace("")("secret"), ""; got != want {
		t.Fatalf("空替换文本应删除原值，得到 %q", got)
	}
}

func TestHash(t *testing.T) {
	sum := sha256.Sum256([]byte("pepper" + "13812345678"))
	want := "sha256:" + hex.EncodeToString(sum[:])

	got := masking.Hash([]byte("pepper"))("13812345678")
	if got != want {
		t.Fatalf("Hash = %q, 期望 %q", got, want)
	}

	if other := masking.Hash([]byte("other"))("13812345678"); other == got {
		t.Fatal("不同盐应产生不同结果")
	}

	if again := masking.Hash([]byte("pepper"))("13812345678"); again != got {
		t.Fatal("同盐同值应稳定输出")
	}
}

func TestEmailKeepLocal(t *testing.T) {
	tests := []struct {
		name  string
		value string
		keep  int
		want  string
	}{
		{name: "保留本地部分首字符", value: "alice@example.com", keep: 1, want: "a****@example.com"},
		{name: "保留本地部分两个字符", value: "alice@example.com", keep: 2, want: "al***@example.com"},
		{name: "本地部分不足保留位时整体遮蔽", value: "a@example.com", keep: 1, want: "*@example.com"},
		{name: "无 @ 时退化为普通保留", value: "alice", keep: 1, want: "a****"},
		{name: "空值原样返回", value: "", keep: 1, want: ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := masking.EmailKeepLocal(tt.keep)(tt.value)
			if got != tt.want {
				t.Fatalf("EmailKeepLocal(%d)(%q) = %q, 期望 %q", tt.keep, tt.value, got, tt.want)
			}
		})
	}
}

func TestMaskDefaults(t *testing.T) {
	masker := masking.New()

	tests := []struct {
		name  string
		typ   masking.Type
		value string
		want  string
	}{
		{name: "手机号", typ: masking.TypePhone, value: "13812345678", want: "138****5678"},
		{name: "固话", typ: masking.TypeTelephone, value: "010-12345678", want: "010*****5678"},
		{name: "身份证", typ: masking.TypeIDCard, value: "110101199003071234", want: "110101********1234"},
		{name: "银行卡", typ: masking.TypeBankCard, value: "6222021234567890", want: "622202******7890"},
		{name: "邮箱", typ: masking.TypeEmail, value: "alice@example.com", want: "a****@example.com"},
		{name: "姓名", typ: masking.TypeName, value: "张三", want: "张*"},
		{name: "地址", typ: masking.TypeAddress, value: "北京市朝阳区建国路1号", want: "北京市朝阳区*****"},
		{name: "IP", typ: masking.TypeIP, value: "192.168.1.1", want: "***********"},
		{name: "密码", typ: masking.TypePassword, value: "secret123", want: "*********"},
		{name: "通用密文", typ: masking.TypeSecret, value: "abc", want: "***"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := masker.Mask(tt.typ, tt.value); got != tt.want {
				t.Fatalf("Mask(%q, %q) = %q, 期望 %q", tt.typ, tt.value, got, tt.want)
			}
		})
	}
}

func TestMaskUnknownTypeIsFailClosed(t *testing.T) {
	masker := masking.New()

	if got := masker.Mask(masking.Type("no_such_type"), "13812345678"); got != masking.RedactedMarker {
		t.Fatalf("未知类型应返回 %q, 得到 %q", masking.RedactedMarker, got)
	}
}

func TestRegister(t *testing.T) {
	t.Run("覆盖内置格式", func(t *testing.T) {
		masker := masking.New()
		if err := masker.Register(masking.TypePhone, masking.Keep(4, 4)); err != nil {
			t.Fatalf("注册失败: %v", err)
		}

		if got, want := masker.Mask(masking.TypePhone, "13812345678"), "1381***5678"; got != want {
			t.Fatalf("覆盖后 = %q, 期望 %q", got, want)
		}
	})

	t.Run("新增自定义类型", func(t *testing.T) {
		masker := masking.New()
		if err := masker.Register(masking.Type("order_no"), masking.Keep(4, 4)); err != nil {
			t.Fatalf("注册失败: %v", err)
		}

		if got, want := masker.Mask("order_no", "ORD123456789"), "ORD1****6789"; got != want {
			t.Fatalf("自定义类型 = %q, 期望 %q", got, want)
		}
	})

	t.Run("空函数返回错误", func(t *testing.T) {
		if err := masking.New().Register(masking.TypePhone, nil); !errors.Is(err, masking.ErrNilMaskFunc) {
			t.Fatalf("期望 ErrNilMaskFunc, 得到 %v", err)
		}
	})

	t.Run("空类型名返回错误", func(t *testing.T) {
		if err := masking.New().Register("", masking.Keep(1, 1)); !errors.Is(err, masking.ErrInvalidType) {
			t.Fatalf("期望 ErrInvalidType, 得到 %v", err)
		}
	})
}

func TestMaskText(t *testing.T) {
	masker := masking.New()

	text := "用户 13812345678 身份证 110101199003071234 邮箱 alice@example.com"
	want := "用户 138****5678 身份证 110101********1234 邮箱 a****@example.com"

	if got := masker.MaskText(text); got != want {
		t.Fatalf("MaskText = %q, 期望 %q", got, want)
	}
}

func TestMaskTextNoMatch(t *testing.T) {
	masker := masking.New()

	for _, text := range []string{"", "hello world", "订单号 20260101001"} {
		if got := masker.MaskText(text); got != text {
			t.Fatalf("MaskText(%q) = %q, 期望原样返回", text, got)
		}
	}
}

func TestMaskTextPriority(t *testing.T) {
	masker := masking.New()

	long := mustDetector(t, `\d{10}`)
	short := mustDetector(t, `\d{5}`)

	if err := masker.Register("ten", masking.Keep(1, 1), masking.WithDetector(long), masking.WithPriority(10)); err != nil {
		t.Fatalf("注册失败: %v", err)
	}
	if err := masker.Register("five", masking.Keep(2, 2), masking.WithDetector(short), masking.WithPriority(20)); err != nil {
		t.Fatalf("注册失败: %v", err)
	}

	if got, want := masker.MaskText("1234567890"), "12*4567*90"; got != want {
		t.Fatalf("高优先级命中应整段胜出，得到 %q, 期望 %q", got, want)
	}
}

func TestMaskTextLuhnFilter(t *testing.T) {
	masker := masking.New()

	// 通过 Luhn 校验的卡号与不通过的纯数字串
	valid := "用户卡号 4111111111111111 结束"
	if got, want := masker.MaskText(valid), "用户卡号 411111******1111 结束"; got != want {
		t.Fatalf("合法卡号 = %q, 期望 %q", got, want)
	}

	invalid := "用户卡号 4111111111111112 结束"
	if got := masker.MaskText(invalid); got != invalid {
		t.Fatalf("Luhn 不通过的号码不应命中，得到 %q", got)
	}
}

func TestMaskFields(t *testing.T) {
	masker := masking.New()

	data := map[string]any{
		"phone": "13812345678",
		"user": map[string]any{
			"id_card": "110101199003071234",
			"name":    "张三",
		},
		"contacts": []any{
			map[string]any{"phone": "13912345678"},
		},
		"count": 42,
		"other": "keep me",
	}

	fields := map[string]masking.Type{
		"phone":   masking.TypePhone,
		"id_card": masking.TypeIDCard,
		"name":    masking.TypeName,
	}

	got := masker.MaskFields(data, fields)

	if got["phone"] != "138****5678" {
		t.Fatalf("顶层字段 = %v", got["phone"])
	}

	user, ok := got["user"].(map[string]any)
	if !ok {
		t.Fatalf("嵌套 map 类型错误: %T", got["user"])
	}
	if user["id_card"] != "110101********1234" || user["name"] != "张*" {
		t.Fatalf("嵌套字段 = %v", user)
	}

	contacts, ok := got["contacts"].([]any)
	if !ok || len(contacts) != 1 {
		t.Fatalf("切片字段 = %v", got["contacts"])
	}
	if contact, ok := contacts[0].(map[string]any); !ok || contact["phone"] != "139****5678" {
		t.Fatalf("切片内嵌套字段 = %v", contacts[0])
	}

	if got["count"] != 42 || got["other"] != "keep me" {
		t.Fatalf("未声明字段应原样保留: %v", got)
	}

	// 入参不应被修改
	if data["phone"] != "13812345678" {
		t.Fatal("MaskFields 不应修改入参")
	}
}

func TestMaskFieldsUnknownType(t *testing.T) {
	masker := masking.New()

	got := masker.MaskFields(
		map[string]any{"x": "anything"},
		map[string]masking.Type{"x": masking.Type("no_such_type")},
	)

	if got["x"] != masking.RedactedMarker {
		t.Fatalf("未知类型应按 fail-closed 处理，得到 %v", got["x"])
	}
}

func TestTypes(t *testing.T) {
	types := masking.New().Types()

	if len(types) != 10 {
		t.Fatalf("内置类型数 = %d, 期望 10", len(types))
	}
	for i := 1; i < len(types); i++ {
		if types[i-1] >= types[i] {
			t.Fatalf("Types() 未按字典序排列: %v", types)
		}
	}
}

func TestMaskerConcurrent(t *testing.T) {
	masker := masking.New()

	var wg sync.WaitGroup
	for range 8 {
		wg.Go(func() {
			for range 100 {
				_ = masker.Mask(masking.TypePhone, "13812345678")
				_ = masker.MaskText("联系 13812345678")
				_ = masker.MaskFields(
					map[string]any{"phone": "13812345678"},
					map[string]masking.Type{"phone": masking.TypePhone},
				)
			}
		})
	}
	wg.Wait()
}

func TestPackageLevelMask(t *testing.T) {
	if got, want := masking.Mask(masking.TypePhone, "13812345678"), "138****5678"; got != want {
		t.Fatalf("包级 Mask = %q, 期望 %q", got, want)
	}
	if got, want := masking.MaskText("联系 13812345678"), "联系 138****5678"; got != want {
		t.Fatalf("包级 MaskText = %q, 期望 %q", got, want)
	}
}

func TestPresets(t *testing.T) {
	tests := []struct {
		name string
		mask masking.MaskFunc
		in   string
		want string
	}{
		{name: "Phone", mask: masking.Phone(), in: "13812345678", want: "138****5678"},
		{name: "Telephone", mask: masking.Telephone(), in: "010-12345678", want: "010*****5678"},
		{name: "IDCard", mask: masking.IDCard(), in: "110101199003071234", want: "110101********1234"},
		{name: "BankCard", mask: masking.BankCard(), in: "6222021234567890", want: "622202******7890"},
		{name: "Email", mask: masking.Email(), in: "alice@example.com", want: "a****@example.com"},
		{name: "Name", mask: masking.Name(), in: "张三", want: "张*"},
		{name: "Address", mask: masking.Address(), in: "北京市朝阳区建国路1号", want: "北京市朝阳区*****"},
		{name: "IP", mask: masking.IP(), in: "192.168.1.1", want: "***********"},
		{name: "Password", mask: masking.Password(), in: "secret123", want: "*********"},
		{name: "Secret", mask: masking.Secret(), in: "abc", want: "***"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.mask(tt.in); got != tt.want {
				t.Fatalf("%s()(%q) = %q, 期望 %q", tt.name, tt.in, got, tt.want)
			}
		})
	}
}
