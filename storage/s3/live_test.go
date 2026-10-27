package s3_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/corenca/teatool-go/storage"
	"github.com/corenca/teatool-go/storage/s3"
)

// liveConfig 是真实连接测试的配置（来自 docs/config/<vendor>.txt）。
type liveConfig struct {
	Endpoint        string
	Region          string
	AccessKeyID     string
	SecretAccessKey string
	Bucket          string
}

// loadLiveConfig 读取配置；文件缺失时跳过该端点的测试。
func loadLiveConfig(t *testing.T, vendor string) liveConfig {
	t.Helper()
	path := filepath.Join("..", "..", "docs", "config", vendor+".txt")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("缺少 %s，跳过真实连接测试", path)
	}

	values := map[string]string{}
	for line := range strings.SplitSeq(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		values[strings.TrimSpace(key)] = strings.Trim(strings.TrimSpace(value), `"`)
	}
	cfg := liveConfig{
		Endpoint:        values["endpoint"],
		Region:          values["region"],
		AccessKeyID:     values["access_key_id"],
		SecretAccessKey: values["secret_access_key"],
		Bucket:          values["bucket"],
	}
	t.Logf("配置来源=%s endpoint=%s bucket=%s region=%s", path, cfg.Endpoint, cfg.Bucket, cfg.Region)
	return cfg
}

// loadTestConfig 读取同目录的 s3_test.json；文件缺失或服务连不上时跳过。
func loadTestConfig(t *testing.T) (*s3.Client, string) {
	t.Helper()

	raw, err := os.ReadFile("s3_test.json")
	if err != nil {
		t.Skipf("缺少 s3_test.json（可从 s3_test.json.template 复制），跳过真实服务测试")
	}
	var cfg s3.Config
	if err := json.Unmarshal(raw, &cfg); err != nil {
		t.Fatalf("解析 s3_test.json 失败: %v", err)
	}
	if cfg.Bucket == "" {
		t.Fatalf("s3_test.json 缺少 bucket")
	}

	client, err := s3.New(cfg)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}
	// 连不上按约定跳过：环境问题不应伪装成用例失败。
	if _, err := client.Exists(t.Context(), cfg.Bucket, ".teatool-connectivity-probe"); err != nil {
		t.Skipf("无法连接 %s（bucket=%s）: %v", cfg.Endpoint, cfg.Bucket, err)
	}
	t.Logf("真实服务配置 endpoint=%s bucket=%s pathStyle=%v checksum=%s",
		cfg.Endpoint, cfg.Bucket, cfg.UsePathStyle, cfg.Checksum)
	return client, cfg.Bucket
}

// newTestContext 返回真实服务客户端与该用例独占的 key 前缀，结束时清理。
func newTestContext(t *testing.T) (*s3.Client, string, string) {
	t.Helper()
	client, bucket := loadTestConfig(t)
	prefix := fmt.Sprintf("teatool-test/%s/%d/", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
	t.Cleanup(func() {
		if err := client.DeletePrefix(context.Background(), bucket, prefix); err != nil {
			t.Logf("清理前缀 %s 失败: %v", prefix, err)
		}
	})
	return client, bucket, prefix
}

// newOfflineClient 返回一个不会发出请求的客户端，供参数校验类用例使用。
func newOfflineClient(t *testing.T) *s3.Client {
	t.Helper()
	client, err := s3.New(s3.Config{
		Endpoint:        "http://127.0.0.1:1",
		AccessKeyID:     "offline-ak",
		SecretAccessKey: "offline-sk",
		UsePathStyle:    true,
	})
	if err != nil {
		t.Fatalf("创建离线客户端失败: %v", err)
	}
	return client
}

// TestLiveS3 用 s3_test.json 指向的真实服务跑一遍契约能力。
func TestLiveS3(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过真实服务测试")
	}
	client, bucket := loadTestConfig(t)
	runLive(t, client, bucket)
}

// TestLive 对 docs/config 下每个可用端点跑一遍契约能力。
// 加 -short 跳过，避免日常回归依赖外网。
func TestLive(t *testing.T) {
	if testing.Short() {
		t.Skip("短模式跳过真实连接测试")
	}
	for _, vendor := range []string{"s3", "oss", "cos", "obs"} {
		t.Run(vendor, func(t *testing.T) {
			cfg := loadLiveConfig(t, vendor)
			client, err := s3.New(s3.Config{
				Endpoint:        cfg.Endpoint,
				Region:          cfg.Region,
				AccessKeyID:     cfg.AccessKeyID,
				SecretAccessKey: cfg.SecretAccessKey,
				HTTPClient:      &http.Client{Timeout: 60 * time.Second},
			})
			if err != nil {
				t.Fatalf("创建客户端失败: %v", err)
			}
			runLive(t, client, cfg.Bucket)
		})
	}
}

// runLive 覆盖契约中的全部能力，并在结束时清理前缀。
func runLive(t *testing.T, client *s3.Client, bucket string) {
	t.Helper()
	ctx := t.Context()
	prefix := fmt.Sprintf("teatool-live/%d/", time.Now().UnixNano())

	step := func(name string, fn func() error) {
		t.Helper()
		start := time.Now()
		err := fn()
		t.Logf("op=%-14s bucket=%s cost=%v err=%v", name, bucket, time.Since(start).Round(time.Millisecond), err)
		if err != nil {
			t.Fatalf("%s 失败: %v", name, err)
		}
	}

	t.Cleanup(func() {
		if err := client.DeletePrefix(context.Background(), bucket, prefix); err != nil {
			t.Logf("清理前缀 %s 失败: %v", prefix, err)
		}
	})

	step("ListBuckets", func() error {
		buckets, err := client.ListBuckets(ctx)
		if err == nil {
			t.Logf("可见桶数量 = %d", len(buckets))
		}
		return err
	})

	step("PutBytes", func() error {
		return client.PutBytes(ctx, bucket, prefix+"small.txt", []byte("hello storage"), storage.WriterOptions{
			ContentType: "text/plain",
			Metadata:    map[string]string{"Probe": "v1"},
		})
	})

	step("Stat", func() error {
		info, err := client.Stat(ctx, bucket, prefix+"small.txt")
		if err != nil {
			return err
		}
		t.Logf("meta=%v size=%d type=%q", info.Metadata, info.Size, info.ContentType)
		if info.Metadata["probe"] != "v1" {
			return fmt.Errorf("metadata probe = %q", info.Metadata["probe"])
		}
		return nil
	})

	step("Exists", func() error {
		ok, err := client.Exists(ctx, bucket, prefix+"small.txt")
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("对象应存在")
		}
		return nil
	})

	step("GetBytes", func() error {
		data, err := client.GetBytes(ctx, bucket, prefix+"small.txt", storage.ReaderOptions{})
		if err != nil {
			return err
		}
		return expectBytes(data, "hello storage")
	})

	step("GetRange", func() error {
		reader, err := client.Get(ctx, bucket, prefix+"small.txt", storage.ReaderOptions{
			Range: &storage.ByteRange{Start: 6, End: 13},
		})
		if err != nil {
			return err
		}
		defer reader.Close()
		data, err := io.ReadAll(reader)
		if err != nil {
			return err
		}
		t.Logf("区间读取 = %q 对象总大小 = %d", data, reader.Info.Size)
		return expectBytes(data, "storage")
	})

	step("PutMultipart", func() error {
		const partSize = 5 << 20
		writer, err := client.Put(ctx, bucket, prefix+"large.bin", storage.WriterOptions{PartSize: partSize})
		if err != nil {
			return err
		}
		if _, err := writer.Write(bytes.Repeat([]byte("m"), 6<<20)); err != nil {
			return err
		}
		if err := writer.Close(); err != nil {
			return err
		}
		info, err := client.Stat(ctx, bucket, prefix+"large.bin")
		if err != nil {
			return err
		}
		t.Logf("分片对象大小 = %d", info.Size)
		if info.Size != 6<<20 {
			return fmt.Errorf("size = %d, want %d", info.Size, 6<<20)
		}
		return nil
	})

	step("Upload/Download", func() error {
		local := filepath.Join(t.TempDir(), "upload.txt")
		if err := os.WriteFile(local, []byte("file round trip"), 0o644); err != nil {
			return err
		}
		if err := client.Upload(ctx, bucket, prefix+"round.txt", local, storage.WriterOptions{}); err != nil {
			return err
		}
		target := filepath.Join(t.TempDir(), "deep", "dir", "down.txt")
		if err := client.Download(ctx, bucket, prefix+"round.txt", target, storage.ReaderOptions{}); err != nil {
			return err
		}
		data, err := os.ReadFile(target)
		if err != nil {
			return err
		}
		return expectBytes(data, "file round trip")
	})

	step("Copy", func() error {
		if err := client.Copy(ctx, bucket, prefix+"small.txt", bucket, prefix+"copy.txt"); err != nil {
			return err
		}
		_, err := client.Stat(ctx, bucket, prefix+"copy.txt")
		return err
	})

	step("Move", func() error {
		if err := client.Move(ctx, bucket, prefix+"copy.txt", bucket, prefix+"moved.txt"); err != nil {
			return err
		}
		return client.Delete(ctx, bucket, prefix+"moved.txt")
	})

	step("List", func() error {
		for i := range 3 {
			key := fmt.Sprintf("%slist/%d.txt", prefix, i)
			if err := client.PutBytes(ctx, bucket, key, []byte("x"), storage.WriterOptions{}); err != nil {
				return err
			}
		}
		cursor, pages := "", 0
		for {
			page, err := client.List(ctx, bucket, storage.ListOptions{Prefix: prefix + "list/", MaxKeys: 2, Cursor: cursor})
			if err != nil {
				return err
			}
			pages++
			t.Logf("第 %d 页 pageSize=%d objects=%d next=%v", pages, page.PageSize, len(page.Objects), page.NextCursor != "")
			if page.NextCursor == "" {
				break
			}
			cursor = page.NextCursor
		}
		if pages < 2 {
			return fmt.Errorf("期望至少两页，实际 %d", pages)
		}
		return nil
	})

	step("ListDir", func() error {
		for _, key := range []string{prefix + "dir/a/1.txt", prefix + "dir/b/1.txt"} {
			if err := client.PutBytes(ctx, bucket, key, []byte("x"), storage.WriterOptions{}); err != nil {
				return err
			}
		}
		dir, err := client.ListDir(ctx, bucket, prefix+"dir/")
		if err != nil {
			return err
		}
		t.Logf("目录视图 dirs=%v objects=%d", dir.Dirs, len(dir.Objects))
		if strings.Join(dir.Dirs, ",") != "a,b" {
			return fmt.Errorf("dirs = %v, want [a b]", dir.Dirs)
		}
		return nil
	})

	step("PresignGet", func() error {
		signed, err := client.PresignGet(ctx, bucket, prefix+"small.txt", storage.PresignOptions{Expires: 5 * time.Minute})
		if err != nil {
			return err
		}
		resp, err := http.Get(signed.URL)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		data, err := io.ReadAll(resp.Body)
		if err != nil {
			return err
		}
		if resp.StatusCode != http.StatusOK {
			return fmt.Errorf("status=%d body=%s", resp.StatusCode, data)
		}
		return expectBytes(data, "hello storage")
	})

	step("PresignPut", func() error {
		signed, err := client.PresignPut(ctx, bucket, prefix+"presigned.txt", storage.PresignOptions{Expires: 5 * time.Minute})
		if err != nil {
			return err
		}
		req, err := http.NewRequest(http.MethodPut, signed.URL, strings.NewReader("presigned body"))
		if err != nil {
			return err
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return err
		}
		defer resp.Body.Close()
		if resp.StatusCode/100 != 2 {
			body, _ := io.ReadAll(resp.Body)
			return fmt.Errorf("status=%d body=%s", resp.StatusCode, body)
		}
		data, err := client.GetBytes(ctx, bucket, prefix+"presigned.txt", storage.ReaderOptions{})
		if err != nil {
			return err
		}
		return expectBytes(data, "presigned body")
	})

	step("DeleteMany", func() error {
		return client.DeleteMany(ctx, bucket, []string{prefix + "small.txt", prefix + "large.bin"})
	})
}

// expectBytes 比较内容，失败时只报长度摘要。
func expectBytes(got []byte, want string) error {
	if string(got) != want {
		return fmt.Errorf("内容不一致：got %d 字节, want %d 字节", len(got), len(want))
	}
	return nil
}
