# teatool-go

Go 业务工具库（module `github.com/corenca/teatool-go`，要求 Go 1.27.2）。核心包零
第三方依赖；例外是 `storage/s3`（aws-sdk-go-v2）与 `gm`（emmansun/gmsm）。
每个模块的用法、参数与常见错误见各自 README。

## 功能

| 模块 | 提供的功能 | 文档 |
| --- | --- | --- |
| `batch` | 带并发上限的批量任务执行：结果与入参下标对齐、三种失败策略、协作式超时、panic 恢复 | [batch/README.md](batch/README.md) |
| `collection` | 无序集合 `Set`、保序集合 `OrderedSet`（增删查、并交差、克隆）、切片按份数/条数切分、保序去重 | [collection/README.md](collection/README.md) |
| `datetime` | 常用时间布局与 IANA 时区常量、日/周/月/季边界、逐日序列、显式布局解析 | [datetime/README.md](datetime/README.md) |
| `filesplit` | 大文件按行切分：按每份行数或总份数，流式读写、行尾原样保留、失败清理 | [filesplit/README.md](filesplit/README.md) |
| `fsutil` | 判存、建父目录、上传文件名清洗与安全拼接、单文件复制/移动、目录文件遍历 | [fsutil/README.md](fsutil/README.md) |
| `masking` | 字段与自由文本脱敏：十种内置业务类型、可注册自定义类型与自动识别规则 | [masking/README.md](masking/README.md) |
| `gm` | 国密 SM2 / SM3 / SM4：配置项加密（SM4-GCM）、前后端报文加密（SM4-CBC + HMAC-SM3）、SM2 短数据加密、摘要与 HMAC | [gm/README.md](gm/README.md) |
| `storage` | 对象存储统一契约：读写、区间与末尾 N 字节、分片上传、磁盘上传下载、列举与目录视图、批量删除、复制移动、预签名 | [storage/README.md](storage/README.md) |
| `storage/s3` | 上述契约的 S3 实现，同一份代码覆盖 AWS S3、OSS、COS、OBS、MinIO | [storage/README.md](storage/README.md) |
