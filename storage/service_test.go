package storage_test

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/corenca/teatool-go/storage"
)

// recordedCall 记录一次转发到假 Client 的调用。
type recordedCall struct {
	method string
	args   []any
}

// fakeClient 记录每次调用并把固定返回值交回调用方；err 非空时所有方法返回它。
type fakeClient struct {
	calls []recordedCall
	err   error
}

// 编译期断言：假 Client 必须完整实现契约。
var _ storage.Client = (*fakeClient)(nil)

func (f *fakeClient) record(method string, args ...any) {
	f.calls = append(f.calls, recordedCall{method: method, args: args})
}

// nopWriter 是 Put 返回的写入器。
type nopWriter struct{}

func (nopWriter) Write(p []byte) (int, error) { return len(p), nil }
func (nopWriter) Close() error                { return nil }
func (nopWriter) Abort() error                { return nil }

// 固定返回值，用于断言参数与返回值都原样透传。
var (
	fakeInfo    = storage.ObjectInfo{Key: "a.txt", Size: 7, ETag: "e"}
	fakePage    = storage.ListPage{PageSize: 3, NextCursor: "c"}
	fakeDir     = storage.DirInfo{Prefix: "p/", Dirs: []string{"d"}}
	fakeURL     = storage.PresignedURL{URL: "https://example.com/x", Headers: map[string]string{"h": "v"}}
	fakeReader  = &storage.Reader{}
	fakeWriter  = storage.Writer(nopWriter{})
	fakeBytes   = []byte("fake bytes")
	fakeObjects = []storage.ObjectInfo{fakeInfo}
	fakeKeys    = []string{"k1", "k2"}
)

func (f *fakeClient) ListBuckets(ctx context.Context) ([]string, error) {
	f.record("ListBuckets")
	return []string{"b1"}, f.err
}

func (f *fakeClient) Stat(ctx context.Context, bucket, key string) (storage.ObjectInfo, error) {
	f.record("Stat", bucket, key)
	return fakeInfo, f.err
}

func (f *fakeClient) Exists(ctx context.Context, bucket, key string) (bool, error) {
	f.record("Exists", bucket, key)
	return true, f.err
}

func (f *fakeClient) Get(ctx context.Context, bucket, key string, opts storage.ReaderOptions) (*storage.Reader, error) {
	f.record("Get", bucket, key, opts)
	return fakeReader, f.err
}

func (f *fakeClient) GetBytes(ctx context.Context, bucket, key string, opts storage.ReaderOptions) ([]byte, error) {
	f.record("GetBytes", bucket, key, opts)
	return fakeBytes, f.err
}

func (f *fakeClient) Put(ctx context.Context, bucket, key string, opts storage.WriterOptions) (storage.Writer, error) {
	f.record("Put", bucket, key, opts)
	return fakeWriter, f.err
}

func (f *fakeClient) PutBytes(ctx context.Context, bucket, key string, data []byte, opts storage.WriterOptions) error {
	f.record("PutBytes", bucket, key, data, opts)
	return f.err
}

func (f *fakeClient) Upload(ctx context.Context, bucket, key, path string, opts storage.WriterOptions) error {
	f.record("Upload", bucket, key, path, opts)
	return f.err
}

func (f *fakeClient) Download(ctx context.Context, bucket, key, path string, opts storage.ReaderOptions) error {
	f.record("Download", bucket, key, path, opts)
	return f.err
}

func (f *fakeClient) Delete(ctx context.Context, bucket, key string) error {
	f.record("Delete", bucket, key)
	return f.err
}

func (f *fakeClient) DeleteMany(ctx context.Context, bucket string, keys []string) error {
	f.record("DeleteMany", bucket, keys)
	return f.err
}

func (f *fakeClient) DeletePrefix(ctx context.Context, bucket, prefix string) error {
	f.record("DeletePrefix", bucket, prefix)
	return f.err
}

func (f *fakeClient) Copy(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	f.record("Copy", srcBucket, srcKey, dstBucket, dstKey)
	return f.err
}

func (f *fakeClient) Move(ctx context.Context, srcBucket, srcKey, dstBucket, dstKey string) error {
	f.record("Move", srcBucket, srcKey, dstBucket, dstKey)
	return f.err
}

func (f *fakeClient) List(ctx context.Context, bucket string, opts storage.ListOptions) (storage.ListPage, error) {
	f.record("List", bucket, opts)
	return fakePage, f.err
}

func (f *fakeClient) ListAll(ctx context.Context, bucket, prefix string) ([]storage.ObjectInfo, error) {
	f.record("ListAll", bucket, prefix)
	return fakeObjects, f.err
}

func (f *fakeClient) Walk(ctx context.Context, bucket, prefix string, fn func(storage.ObjectInfo) error) error {
	f.record("Walk", bucket, prefix)
	if f.err != nil {
		return f.err
	}
	for _, object := range fakeObjects {
		if err := fn(object); err != nil {
			return err
		}
	}
	return nil
}

func (f *fakeClient) ListDir(ctx context.Context, bucket, prefix string) (storage.DirInfo, error) {
	f.record("ListDir", bucket, prefix)
	return fakeDir, f.err
}

func (f *fakeClient) PresignGet(ctx context.Context, bucket, key string, opts storage.PresignOptions) (storage.PresignedURL, error) {
	f.record("PresignGet", bucket, key, opts)
	return fakeURL, f.err
}

func (f *fakeClient) PresignPut(ctx context.Context, bucket, key string, opts storage.PresignOptions) (storage.PresignedURL, error) {
	f.record("PresignPut", bucket, key, opts)
	return fakeURL, f.err
}

// assertValue 断言返回值与错误原样透传。
func assertValue(t *testing.T, label string, got, want any, err error) {
	t.Helper()
	if err != nil {
		t.Fatalf("%s 返回错误: %v", label, err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s 返回值不匹配\n got: %#v\nwant: %#v", label, got, want)
	}
}

func TestNewService(t *testing.T) {
	cases := []struct {
		name    string
		client  storage.Client
		bucket  string
		wantErr bool
	}{
		{name: "nil client", client: nil, bucket: "demo", wantErr: true},
		{name: "empty bucket", client: &fakeClient{}, bucket: "", wantErr: true},
		{name: "ok", client: &fakeClient{}, bucket: "demo", wantErr: false},
		// 桶名原样保留：不裁剪空白、不改大小写。
		{name: "bucket preserved", client: &fakeClient{}, bucket: " Demo ", wantErr: false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			svc, err := storage.NewService(tc.client, tc.bucket)
			t.Logf("NewService(client=%T, bucket=%q) → svc=%v err=%v", tc.client, tc.bucket, svc, err)
			if tc.wantErr {
				if !errors.Is(err, storage.ErrInvalidArgument) {
					t.Fatalf("期望 ErrInvalidArgument，得到 %v", err)
				}
				return
			}
			if err != nil {
				t.Fatalf("构造失败: %v", err)
			}
			if got := svc.Bucket(); got != tc.bucket {
				t.Fatalf("Bucket() = %q, want %q", got, tc.bucket)
			}
		})
	}
}

func TestServiceForwards(t *testing.T) {
	ctx := context.Background()
	data := []byte("hello")
	readerOpts := storage.ReaderOptions{Range: &storage.ByteRange{Start: 1, End: 3}}
	writerOpts := storage.WriterOptions{ContentType: "text/plain", Metadata: map[string]string{"X-Custom": "v"}, PartSize: 1024}
	listOpts := storage.ListOptions{Prefix: "p/", Delimiter: "/", Cursor: "c", MaxKeys: 5}
	presignOpts := storage.PresignOptions{Expires: time.Minute, FileName: "f.txt"}

	cases := []struct {
		name string
		call func(t *testing.T, svc *storage.Service)
		want []recordedCall
	}{
		{
			name: "Stat",
			call: func(t *testing.T, svc *storage.Service) {
				got, err := svc.Stat(ctx, "a.txt")
				assertValue(t, "Stat", got, fakeInfo, err)
			},
			want: []recordedCall{{"Stat", []any{"demo", "a.txt"}}},
		},
		{
			name: "Exists",
			call: func(t *testing.T, svc *storage.Service) {
				got, err := svc.Exists(ctx, "a.txt")
				assertValue(t, "Exists", got, true, err)
			},
			want: []recordedCall{{"Exists", []any{"demo", "a.txt"}}},
		},
		{
			name: "Get",
			call: func(t *testing.T, svc *storage.Service) {
				got, err := svc.Get(ctx, "a.txt", readerOpts)
				if err != nil {
					t.Fatalf("Get 返回错误: %v", err)
				}
				// Reader 必须原样返回同一个指针，而不是内容相同的另一个实例。
				if got != fakeReader {
					t.Fatalf("Get 返回值不是假 Client 的那个 Reader: %p", got)
				}
			},
			want: []recordedCall{{"Get", []any{"demo", "a.txt", readerOpts}}},
		},
		{
			name: "GetBytes",
			call: func(t *testing.T, svc *storage.Service) {
				got, err := svc.GetBytes(ctx, "a.txt", readerOpts)
				assertValue(t, "GetBytes", got, fakeBytes, err)
			},
			want: []recordedCall{{"GetBytes", []any{"demo", "a.txt", readerOpts}}},
		},
		{
			name: "Put",
			call: func(t *testing.T, svc *storage.Service) {
				got, err := svc.Put(ctx, "a.txt", writerOpts)
				if err != nil {
					t.Fatalf("Put 返回错误: %v", err)
				}
				// Writer 必须原样返回假 Client 的那个实例。
				if got != fakeWriter {
					t.Fatalf("Put 返回值不是假 Client 的那个 Writer: %#v", got)
				}
			},
			want: []recordedCall{{"Put", []any{"demo", "a.txt", writerOpts}}},
		},
		{
			name: "PutBytes",
			call: func(t *testing.T, svc *storage.Service) {
				assertValue(t, "PutBytes 返回值", svc.PutBytes(ctx, "a.txt", data, writerOpts), nil, nil)
			},
			want: []recordedCall{{"PutBytes", []any{"demo", "a.txt", data, writerOpts}}},
		},
		{
			name: "Upload",
			call: func(t *testing.T, svc *storage.Service) {
				assertValue(t, "Upload 返回值", svc.Upload(ctx, "a.txt", "/tmp/a.txt", writerOpts), nil, nil)
			},
			want: []recordedCall{{"Upload", []any{"demo", "a.txt", "/tmp/a.txt", writerOpts}}},
		},
		{
			name: "Download",
			call: func(t *testing.T, svc *storage.Service) {
				assertValue(t, "Download 返回值", svc.Download(ctx, "a.txt", "/tmp/a.txt", readerOpts), nil, nil)
			},
			want: []recordedCall{{"Download", []any{"demo", "a.txt", "/tmp/a.txt", readerOpts}}},
		},
		{
			name: "Delete",
			call: func(t *testing.T, svc *storage.Service) {
				assertValue(t, "Delete 返回值", svc.Delete(ctx, "a.txt"), nil, nil)
			},
			want: []recordedCall{{"Delete", []any{"demo", "a.txt"}}},
		},
		{
			name: "DeleteMany",
			call: func(t *testing.T, svc *storage.Service) {
				assertValue(t, "DeleteMany 返回值", svc.DeleteMany(ctx, fakeKeys), nil, nil)
			},
			want: []recordedCall{{"DeleteMany", []any{"demo", fakeKeys}}},
		},
		{
			name: "DeletePrefix",
			call: func(t *testing.T, svc *storage.Service) {
				assertValue(t, "DeletePrefix 返回值", svc.DeletePrefix(ctx, "p/"), nil, nil)
			},
			want: []recordedCall{{"DeletePrefix", []any{"demo", "p/"}}},
		},
		{
			name: "Copy",
			call: func(t *testing.T, svc *storage.Service) {
				assertValue(t, "Copy 返回值", svc.Copy(ctx, "a.txt", "b.txt"), nil, nil)
			},
			want: []recordedCall{{"Copy", []any{"demo", "a.txt", "demo", "b.txt"}}},
		},
		{
			name: "CopySameKey",
			call: func(t *testing.T, svc *storage.Service) {
				// 源与目标相同不做本地校验，交给底层决定。
				assertValue(t, "CopySameKey 返回值", svc.Copy(ctx, "a.txt", "a.txt"), nil, nil)
			},
			want: []recordedCall{{"Copy", []any{"demo", "a.txt", "demo", "a.txt"}}},
		},
		{
			name: "Move",
			call: func(t *testing.T, svc *storage.Service) {
				assertValue(t, "Move 返回值", svc.Move(ctx, "a.txt", "b.txt"), nil, nil)
			},
			want: []recordedCall{{"Move", []any{"demo", "a.txt", "demo", "b.txt"}}},
		},
		{
			name: "List",
			call: func(t *testing.T, svc *storage.Service) {
				got, err := svc.List(ctx, listOpts)
				assertValue(t, "List", got, fakePage, err)
			},
			want: []recordedCall{{"List", []any{"demo", listOpts}}},
		},
		{
			name: "ListAll",
			call: func(t *testing.T, svc *storage.Service) {
				got, err := svc.ListAll(ctx, "p/")
				assertValue(t, "ListAll", got, fakeObjects, err)
			},
			want: []recordedCall{{"ListAll", []any{"demo", "p/"}}},
		},
		{
			name: "Walk",
			call: func(t *testing.T, svc *storage.Service) {
				var keys []string
				err := svc.Walk(ctx, "p/", func(object storage.ObjectInfo) error {
					keys = append(keys, object.Key)
					return nil
				})
				t.Logf("Walk 收集到 key = %v err = %v", keys, err)
				if err != nil {
					t.Fatalf("Walk 返回错误: %v", err)
				}
				if len(keys) != len(fakeObjects) {
					t.Fatalf("Walk 回调 %d 次, want %d", len(keys), len(fakeObjects))
				}
			},
			want: []recordedCall{{"Walk", []any{"demo", "p/"}}},
		},
		{
			name: "ListDir",
			call: func(t *testing.T, svc *storage.Service) {
				got, err := svc.ListDir(ctx, "p/")
				assertValue(t, "ListDir", got, fakeDir, err)
			},
			want: []recordedCall{{"ListDir", []any{"demo", "p/"}}},
		},
		{
			name: "PresignGet",
			call: func(t *testing.T, svc *storage.Service) {
				got, err := svc.PresignGet(ctx, "a.txt", presignOpts)
				assertValue(t, "PresignGet", got, fakeURL, err)
			},
			want: []recordedCall{{"PresignGet", []any{"demo", "a.txt", presignOpts}}},
		},
		{
			name: "PresignPut",
			call: func(t *testing.T, svc *storage.Service) {
				got, err := svc.PresignPut(ctx, "a.txt", presignOpts)
				assertValue(t, "PresignPut", got, fakeURL, err)
			},
			want: []recordedCall{{"PresignPut", []any{"demo", "a.txt", presignOpts}}},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			fake := &fakeClient{}
			svc, err := storage.NewService(fake, "demo")
			if err != nil {
				t.Fatalf("构造失败: %v", err)
			}

			tc.call(t, svc)
			t.Logf("%s 转发记录: %v", tc.name, fake.calls)

			if !reflect.DeepEqual(fake.calls, tc.want) {
				t.Fatalf("%s 转发不匹配\n got: %#v\nwant: %#v", tc.name, fake.calls, tc.want)
			}
		})
	}
}

func TestServicePassesErrors(t *testing.T) {
	errBoom := errors.New("boom")
	fake := &fakeClient{err: errBoom}
	svc, err := storage.NewService(fake, "demo")
	if err != nil {
		t.Fatalf("构造失败: %v", err)
	}

	_, statErr := svc.Stat(context.Background(), "a.txt")
	deleteErr := svc.Delete(context.Background(), "a.txt")
	t.Logf("Stat err = %v, Delete err = %v, 转发记录 = %v", statErr, deleteErr, fake.calls)

	if !errors.Is(statErr, errBoom) || !errors.Is(deleteErr, errBoom) {
		t.Fatalf("错误未原样透传: stat=%v delete=%v", statErr, deleteErr)
	}
}
