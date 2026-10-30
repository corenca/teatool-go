# gm 使用说明

国密 SM2 / SM3 / SM4 加解密，覆盖两类小数据：

- **配置项**：后端自己写、自己读的 AK/SK、数据库密码、连接串。
- **接口报文**：Web 前端与 Go 后端之间的敏感字段或整包 body，两端按同一契约互通。

不做大文件流式加密，不做 SM1（算法未公开，只能依赖硬件密码设备）。

```bash
go get github.com/corenca/teatool-go
```

## 先选对场景

| 场景 | 用哪个 | 算法 | 密钥 | 前端参与 |
| --- | --- | --- | --- | --- |
| 配置项、密钥文件 | `EncryptSM4GCM` / `DecryptSM4GCM` | SM4-GCM | 16 字节 | 否 |
| 接口报文、整包 body | `EncryptSM4Packet` / `DecryptSM4Packet` | SM4-CBC + PKCS#7 + HMAC-SM3 | 32 字节 | 是 |
| 密码、令牌、短字段 | `EncryptSM2` / `DecryptSM2` | SM2 | 公钥加密、私钥解密 | 是 |
| 摘要、签名前的哈希 | `HashSM3` / `HMACSM3` | SM3 | — | 可选 |

配置项不需要和前端对齐，直接用 GCM；报文必须迁就前端库，所以用 CBC + HMAC。
SM2 只适合短数据，大报文用 `EncryptSM4Packet`。

## 密钥怎么注入：环境变量 + hex

密钥通常以 hex 字符串放在环境变量或配置中心，例如 16 字节密钥对应 32 个 hex 字符，
32 字节密钥对应 64 个 hex 字符：

```bash
export CRYPTO_CONFIG_KEY=000102030405060708090a0b0c0d0e0f
export CRYPTO_PACKET_KEY=000102030405060708090a0b0c0d0e0f101112131415161718191a1b1c1d1e1f
```

生成密钥（只需要执行一次，把输出保存到环境变量或密钥管理系统）：

```go
key16, _ := gm.GenerateSM4Key(gm.KeySizeSM4GCM)    // 16 字节
key32, _ := gm.GenerateSM4Key(gm.KeySizeSM4Packet) // 32 字节

fmt.Println(hex.EncodeToString(key16)) // 32 个 hex 字符
fmt.Println(hex.EncodeToString(key32)) // 64 个 hex 字符
```

程序启动时用 `ParseSM4KeyHex` 解码并校验长度：

```go
configKey, err := gm.ParseSM4KeyHex(os.Getenv("CRYPTO_CONFIG_KEY"), gm.KeySizeSM4GCM)
packetKey, err := gm.ParseSM4KeyHex(os.Getenv("CRYPTO_PACKET_KEY"), gm.KeySizeSM4Packet)
if err != nil {
	return err // 空值、非法 hex、长度不符都返回 ErrInvalidKey
}
```

`ParseSM4KeyHex` 会忽略首尾空白，大小写均可；`size` 只接受 `KeySizeSM4GCM` 或
`KeySizeSM4Packet`。是否要求环境变量必须存在、缺失时中断启动，由调用方决定。

注意：

- `ParseSM4KeyHex` 做的是 hex 解码，得到的是**字节**，不是字符串的 ASCII 值；
  不要直接把 hex 字符串当 key 传给 `EncryptSM4GCM` / `EncryptSM4Packet`。
- 密钥长度必须精确：GCM 16 字节，报文 32 字节。
- 日志里不要打印密钥；排查问题时只打印长度或 `kid`。

## 场景一：配置项加密（SM4-GCM）

典型用法：启动时解密数据库密码，或把一段敏感配置写回文件。

```go
package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/corenca/teatool-go/gm"
)

func main() {
	key, err := gm.ParseSM4KeyHex(os.Getenv("CRYPTO_CONFIG_KEY"), gm.KeySizeSM4GCM)
	if err != nil {
		panic(err)
	}

	// AAD 绑定配置项路径，密文挪到别的字段就解不开
	aad := []byte("db.password")

	// 写入：加密后存进配置
	sealed, err := gm.EncryptSM4GCM(key, []byte("p@ssw0rd"), aad, gm.EncryptOptions{
		KID: "2026-01", // 可选，密钥版本标识
	})
	if err != nil {
		panic(err)
	}
	fmt.Println("存进配置的密文:", sealed)

	// 读取：解密
	plain, err := gm.DecryptSM4GCM(key, sealed, aad)
	if err != nil {
		if errors.Is(err, gm.ErrDecrypt) {
			// 密钥不对、AAD 不匹配，或密文被篡改
			panic("配置解密失败")
		}
		panic(err)
	}
	fmt.Println("配置明文:", string(plain))
}
```

`EncryptSM4GCM` 参数：

| 参数 | 说明 |
| --- | --- |
| `key` | 16 字节 SM4 密钥 |
| `plaintext` | 任意长度，允许空 |
| `aad` | 附加认证数据，可为 `nil`；解密必须完全相同 |
| `opts.KID` | 密钥版本标识，可空；只允许 `[A-Za-z0-9_-]`，最长 64 |

`DecryptSM4GCM` 参数相同，只是把 `plaintext` 换成密文字符串。解密失败统一返回
`ErrDecrypt`，不返回部分明文。

## 场景二：接口报文加密（SM4-CBC + HMAC-SM3）

前后端共享一个 **32 字节**密钥：前 16 字节给 SM4，后 16 字节给 HMAC-SM3。
后端解密请求、加密响应都用 `EncryptSM4Packet` / `DecryptSM4Packet`。

AAD 建议把请求上下文拼进去，防止密文跨接口复用：

```go
aad := []byte(r.Method + "|" + r.URL.Path + "|" + timestamp)
```

后端解密示例：

```go
func decryptBody(r *http.Request, key []byte) ([]byte, error) {
	var req struct {
		Data string `json:"data"`
		TS   int64  `json:"ts"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return nil, err
	}

	// 时间窗由业务自己校验；加密本身不防重放
	if time.Since(time.Unix(req.TS, 0)) > 5*time.Minute {
		return nil, errors.New("请求已过期")
	}

	aad := []byte(r.Method + "|" + r.URL.Path + "|" + strconv.FormatInt(req.TS, 10))
	plain, err := gm.DecryptSM4Packet(key, req.Data, aad)
	if err != nil {
		if errors.Is(err, gm.ErrDecrypt) {
			return nil, errors.New("报文校验失败")
		}
		return nil, err
	}
	return plain, nil
}
```

加密响应的写法对称：

```go
sealed, err := gm.EncryptSM4Packet(key, respBody,
	[]byte("POST|/api/login|"+strconv.FormatInt(time.Now().Unix(), 10)),
	gm.EncryptOptions{KID: "2026-01"})
```

`EncryptSM4Packet` / `DecryptSM4Packet` 参数：

| 参数 | 说明 |
| --- | --- |
| `key` | 32 字节：`key[:16]` 加密，`key[16:]` 做 HMAC |
| `plaintext` | 任意长度，内部自动 PKCS#7 填充 |
| `aad` | 参与 MAC 计算，可为 `nil`；解密必须完全相同 |
| `opts.KID` | 写入密文头，解密时从密文头读取，不单独传 |

报文超过几十 KB 时前端纯 JS 的 SM4 会变慢，建议只加密敏感字段，而不是整个 body。

## 场景三：SM2 加解密（前端提交密码）

SM2 是非对称算法，前端只拿公钥，私钥永远留在后端。

### 生成密钥并保存

```bash
# 生成一次，私钥保存到密钥管理系统
```

```go
priv, _ := gm.GenerateSM2Key()

// 私钥：PKCS#8 PEM 或 64 位 hex，二选一
pemBytes, _ := gm.MarshalSM2PrivateKeyPEM(priv)
os.WriteFile("sm2-private.pem", pemBytes, 0o600)

// 仅首次生成时打印，随后请存入密钥管理系统，不要写进日志
fmt.Println("私钥 hex:", gm.SM2PrivateKeyHex(priv))

// 公钥：下发给前端，固定用 04||X||Y 的 hex
fmt.Println("公钥 hex:", gm.SM2PublicKeyHex(&priv.PublicKey))
```

### 从环境变量或文件加载私钥

两段代码二选一：

```go
// 方式一：hex（适合环境变量、KMS 返回的字符串）
priv, err := gm.ParseSM2PrivateKeyHex(os.Getenv("CRYPTO_SM2_PRIVATE_KEY"))
```

```go
// 方式二：PEM 文件
pemBytes, err := os.ReadFile("sm2-private.pem")
if err != nil {
	return err
}
priv, err := gm.ParseSM2PrivateKeyPEM(pemBytes)
```

### 解密前端提交的密文

```go
// 前端提交的密文是 base64 或 hex，先解码成 []byte
ciphertext, err := hex.DecodeString(req.Password)
if err != nil {
	return err
}

plain, err := gm.DecryptSM2(priv, ciphertext, gm.SM2Options{})
if err != nil {
	if errors.Is(err, gm.ErrDecrypt) {
		return errors.New("密码解密失败")
	}
	return err
}
```

`EncryptSM2` / `DecryptSM2` 参数：

| 参数 | 说明 |
| --- | --- |
| `pub` / `priv` | SM2 公钥（`*ecdsa.PublicKey`）/ 私钥（`*sm2.PrivateKey`） |
| `plaintext` | 短数据，建议不超过几百字节 |
| `opts.Order` | `C1C3C2`（默认）或 `C1C2C3`，两端必须一致 |
| `opts.ASN1` | `true` 时使用 ASN.1 DER 编码；此时 `Order` 不参与编码 |

密文是裸字节，传输时自行编码（推荐 base64url 或 hex）。默认带 `04` 前缀，
前端 `sm-crypto` 的输出不带 `04`，按契约由前端补上后再提交。

## 密文格式

对称密文统一为：

```
v1.<alg>.<kid>.<base64url(payload)>
```

| 算法 | payload | 说明 |
| --- | --- | --- |
| `sm4gcm` | `nonce(12) ‖ ciphertext ‖ tag(16)` | 认证加密，篡改会直接失败 |
| `sm4cbc` | `iv(16) ‖ ciphertext ‖ mac(32)` | Encrypt-then-MAC，先验 MAC 再解密 |

格式带版本号 `v1`，后续演进不会误读旧密文。`kid` 可空，空时信封里会出现连续两个点
（`v1.sm4gcm..payload`），这是正常格式。

需要按密钥版本选择密钥时：

```go
kid, err := gm.KID(sealed)
key, ok := keys[kid] // 未知 kid 直接拒绝，不要回退猜测
```

## 前端对接（sm-crypto）

要求 `sm-crypto >= 0.5.0`。低于该版本在没有 `crypto.getRandomValues` 的环境中
随机数可预测，历史密钥应轮换。

关键约定：

- **SM4 必须显式 `mode: 'cbc'`**，`sm-crypto` 不传 `mode` 时走 ECB。
- 密钥是 32 字节：前 16 字节给 SM4，后 16 字节给 HMAC-SM3。
- **SM2 密文不带 `04`**：`sm2.doEncrypt` 的输出要补 `04` 后再交给后端；
  后端下发的公钥本身就是 `04||X||Y`。
- 文本统一 UTF-8，密文外层 base64url 无填充。

报文加密：

```js
import { sm2, sm3, sm4 } from 'sm-crypto'

const enc = new TextEncoder()
const hex = (bytes) => Array.from(bytes, (b) => b.toString(16).padStart(2, '0')).join('')
const unhex = (s) => new Uint8Array(s.match(/.{2}/g).map((b) => parseInt(b, 16)))
const b64url = (bytes) =>
  btoa(String.fromCharCode(...bytes)).replace(/\+/g, '-').replace(/\//g, '_').replace(/=+$/, '')

// key32：32 字节 Uint8Array，来自后端下发的 hex 解码
function sealPacket(key32, plaintext, aad, kid) {
  const encKey = hex(key32.slice(0, 16))
  const macKey = hex(key32.slice(16))
  const iv = crypto.getRandomValues(new Uint8Array(16))

  const ctHex = sm4.encrypt(enc.encode(plaintext), encKey, {
    mode: 'cbc',       // 不传会走 ECB
    iv: hex(iv),
    padding: 'pkcs#7',
  })
  const ct = unhex(ctHex)

  const header = enc.encode('v1.sm4cbc')
  const macInput = new Uint8Array([
    ...header, 0, ...enc.encode(kid), 0,
    ...enc.encode(aad), ...iv, ...ct,
  ])
  const mac = unhex(sm3(macInput, { key: macKey }))

  return `v1.sm4cbc.${kid}.${b64url(new Uint8Array([...iv, ...ct, ...mac]))}`
}

// 后端下发的公钥是 130 位 hex（04||X||Y）
function encryptSM2(publicKeyHex, plaintext) {
  return '04' + sm2.doEncrypt(Array.from(enc.encode(plaintext)), publicKeyHex, 1)
}
```

解密响应的步骤与加密相反：base64url 解码 → 拆出 iv/密文/mac → 校验 MAC →
`sm4.decrypt` 去填充。后端返回的密文同样按上面的信封格式生成。

## 参数速查

```go
// 密钥
GenerateSM4Key(size int) ([]byte, error)              // size 只能是 16 或 32
ParseSM4KeyHex(s string, size int) ([]byte, error)    // 解析 env/KMS 的 hex 密钥

// 配置项
EncryptSM4GCM(key, plaintext, aad []byte, opts EncryptOptions) (string, error)
DecryptSM4GCM(key []byte, sealed string, aad []byte) ([]byte, error)

// 报文
EncryptSM4Packet(key, plaintext, aad []byte, opts EncryptOptions) (string, error)
DecryptSM4Packet(key []byte, sealed string, aad []byte) ([]byte, error)

// 密文头
KID(sealed string) (string, error)

// SM3
HashSM3(data []byte) []byte
HMACSM3(key, data []byte) []byte

// SM2
GenerateSM2Key() (*sm2.PrivateKey, error)
MarshalSM2PrivateKeyPEM(priv *sm2.PrivateKey) ([]byte, error)  // PKCS#8
ParseSM2PrivateKeyPEM(data []byte) (*sm2.PrivateKey, error)
SM2PrivateKeyHex(priv *sm2.PrivateKey) string                  // 64 位 hex
ParseSM2PrivateKeyHex(s string) (*sm2.PrivateKey, error)
MarshalSM2PublicKeyPEM(pub *ecdsa.PublicKey) ([]byte, error)   // PKIX
ParseSM2PublicKeyPEM(data []byte) (*ecdsa.PublicKey, error)
SM2PublicKeyHex(pub *ecdsa.PublicKey) string                   // 04||X||Y
ParseSM2PublicKeyHex(s string) (*ecdsa.PublicKey, error)
EncryptSM2(pub *ecdsa.PublicKey, plaintext []byte, opts SM2Options) ([]byte, error)
DecryptSM2(priv *sm2.PrivateKey, ciphertext []byte, opts SM2Options) ([]byte, error)
```

错误哨兵：

| 错误 | 含义 |
| --- | --- |
| `ErrInvalidKey` | 密钥/公钥/私钥长度或内容不合法 |
| `ErrInvalidFormat` | 信封、base64、载荷长度不合法 |
| `ErrUnsupported` | 版本、算法或 SM2 选项不支持 |
| `ErrDecrypt` | 解密失败的统一出口（认证失败、填充错误、密钥不对） |

## 常见问题

**`ErrInvalidKey`：密钥长度不对。**
检查 hex 是否解码后再传：16 字节密钥是 32 个 hex 字符，32 字节密钥是 64 个。

**`ErrDecrypt`：密钥、AAD 或密文有一处不一致。**
先确认加解密两侧的 AAD 完全相同（包括分隔符和顺序），再确认 `kid` 对应的密钥正确。
错误信息故意不区分具体原因，避免泄露信息。

**为什么配置用 GCM，报文用 CBC + HMAC？**
前端 `sm-crypto` 只实现 SM4 的 ECB/CBC，没有 GCM。CBC 本身不防篡改，所以叠加
HMAC-SM3，并在解密前先验 MAC。

**SM2 密文为什么前端要补 `04`？**
`sm-crypto` 的 `doEncrypt` 输出会去掉未压缩点前缀 `04`；本库按标准格式要求带 `04`，
所以由前端补，后端不做隐式修补。

**加密能防重放吗？**
不能。需要业务侧配合时间戳窗口和 nonce 去重。

## 安全约定

- nonce 与 IV 每次都从 `crypto/rand` 生成，不使用固定 IV、不使用 ECB。
- 解密失败统一返回 `ErrDecrypt`，不返回部分明文。
- 库不管理密钥：密钥由调用方从环境变量、KMS 或密钥文件注入，`kid` 只标识版本。
- 前端加密不是安全边界：HTTPS 必须保留，前端无法真正隐藏密钥，敏感数据仍应由
  后端做最终校验。
- 不要在日志里打印明文、密钥或完整密文。

## 非目标

SM1 / SM9 / ZUC、SM2 签名与验签、国密 TLS、AES/RSA、密钥托管与轮换调度、
流式或分块大文件加密。
