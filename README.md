# teatool-go

Go 工程工具库（module `github.com/corenca/teatool-go`，要求 Go 1.27.2）。
核心包零第三方依赖，仅 `storage/s3` 引入 aws-sdk-go-v2。

## 包

| 包 | 用途 |
| --- | --- |
| `batch` | 带并发上限的批量任务执行器 |
| `collection` | `Set` / `OrderedSet`、切片切分与去重 |
| `datetime` | 时区、日/周/月/季边界、逐日序列、显式解析 |
| `filesplit` | 大文件按行切分 |
| `fsutil` | 判存、建父目录、文件名清洗、单文件复制与移动 |
| `masking` | 字段与文本脱敏，类型可注册 |
| `storage` | 对象存储契约；`storage/s3` 覆盖 S3 / OSS / COS / OBS / MinIO |

存储契约、配置与厂商差异见 [`storage/README.md`](storage/README.md)。

## 使用

```bash
go get github.com/corenca/teatool-go
```

## 测试

```bash
go test ./...          # 全部测试；storage/s3 读 s3_test.json 跑真实连接，缺失则跳过
go test -short ./...   # 跳过真实连接测试
```
