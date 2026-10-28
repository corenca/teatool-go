package s3

import (
	"bytes"
	"context"
	"fmt"
	"sync"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/corenca/teatool-go/storage"
)

// writer 是 Put 返回的流式写入器：数据先入缓冲区，满分片后转分片上传。
//
// Write/Close/Abort 由调用方串行调用；分片上传在后台协程中并发执行，
// 通过 parts 收集结果，parts 与 err 由 mu 保护。
type writer struct {
	client      *Client
	ctx         context.Context
	bucket      string
	key         string
	opts        storage.WriterOptions
	partSize    int
	concurrency int

	buf        bytes.Buffer
	uploadID   *string
	parts      map[int32]types.CompletedPart
	partNumber int32

	sem chan struct{}
	wg  sync.WaitGroup
	mu  sync.Mutex
	err error

	closed  bool
	aborted bool
	done    bool // 已成功提交
}

// Write 追加数据；缓冲区放不下时派发分片。后台分片失败后返回该错误。
func (w *writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("%w: 写入器已关闭", storage.ErrInvalidArgument)
	}
	if err := w.loadError(); err != nil {
		return 0, err
	}
	if w.buf.Len()+len(p) <= w.partSize {
		w.buf.Write(p)
		return len(p), nil
	}

	written := 0
	// 先用本批数据填满缓冲区并派发。
	if w.buf.Len() > 0 {
		need := w.partSize - w.buf.Len()
		w.buf.Write(p[:need])
		p = p[need:]
		written += need
		if err := w.dispatch(w.buf.Bytes()); err != nil {
			w.setError(err)
			return written, err
		}
		w.buf.Reset()
	}
	// 剩余数据按整片派发，尾部落回缓冲区。
	for len(p) >= w.partSize {
		if err := w.dispatch(p[:w.partSize]); err != nil {
			w.setError(err)
			return written, err
		}
		p = p[w.partSize:]
		written += w.partSize
	}
	w.buf.Write(p)
	written += len(p)
	return written, nil
}

// Close 完成上传：未产生分片时走单次 PutObject，否则等待所有在途分片后提交；
// 任一环节失败都会尽力 Abort 未完成的分片。重复调用返回首次结果。
func (w *writer) Close() error {
	if w.closed {
		return w.loadError()
	}
	w.closed = true
	if w.aborted {
		return nil
	}

	// 分片上传已开始：缓冲区里的尾部数据作为最后一片。
	if w.uploadID != nil && w.buf.Len() > 0 {
		if err := w.dispatch(w.buf.Bytes()); err != nil {
			w.setError(err)
		}
		w.buf.Reset()
	}
	w.wg.Wait()

	if err := w.loadError(); err != nil {
		_ = w.abortUpload()
		return err
	}
	if w.uploadID == nil {
		return w.putSingle()
	}
	return w.complete()
}

// Abort 放弃本次写入：等待在途分片结束后回收未完成的分片上传。
func (w *writer) Abort() error {
	if w.closed {
		return nil
	}
	w.closed = true
	w.aborted = true
	w.wg.Wait()
	return w.abortUpload()
}

// dispatch 派发一个分片：首次调用创建分片上传任务，随后交给后台协程上传。
func (w *writer) dispatch(data []byte) error {
	if err := w.loadError(); err != nil {
		return err
	}
	if w.partNumber >= storage.MaxParts {
		return fmt.Errorf("%w: 分片数超过 %d，请增大 PartSize", storage.ErrInvalidArgument, storage.MaxParts)
	}
	if err := w.ensureUpload(); err != nil {
		return err
	}

	// 调用方可能在 Write 返回后复用 p，必须在返回前拷贝。
	data = bytes.Clone(data)
	w.partNumber++
	number := w.partNumber
	w.sem <- struct{}{}
	w.wg.Go(func() {
		defer func() { <-w.sem }()
		w.uploadPart(number, data)
	})
	return nil
}

// ensureUpload 惰性创建分片上传任务，后续分片共享同一个 uploadID。
func (w *writer) ensureUpload() error {
	if w.uploadID != nil {
		return nil
	}
	input := &awss3.CreateMultipartUploadInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(w.key),
	}
	applyCreateOptions(input, w.opts)
	out, err := w.client.sdk.CreateMultipartUpload(w.ctx, input)
	if err != nil {
		return mapError(err)
	}
	w.uploadID = out.UploadId
	w.parts = make(map[int32]types.CompletedPart, 4)
	return nil
}

// uploadPart 上传一片；结果按分片号回收，乱序完成也能按序提交。
func (w *writer) uploadPart(number int32, data []byte) {
	out, err := w.client.sdk.UploadPart(w.ctx, &awss3.UploadPartInput{
		Bucket:     aws.String(w.bucket),
		Key:        aws.String(w.key),
		UploadId:   w.uploadID,
		PartNumber: aws.Int32(number),
		Body:       bytes.NewReader(data),
	})
	if err != nil {
		w.setError(mapError(err))
		return
	}
	w.mu.Lock()
	w.parts[number] = types.CompletedPart{
		ETag:       out.ETag,
		PartNumber: aws.Int32(number),
	}
	w.mu.Unlock()
}

// putSingle 用一次 PutObject 写完整个对象。
func (w *writer) putSingle() error {
	input := &awss3.PutObjectInput{
		Bucket: aws.String(w.bucket),
		Key:    aws.String(w.key),
		Body:   bytes.NewReader(w.buf.Bytes()),
	}
	applyWriterOptions(input, w.opts)
	if _, err := w.client.sdk.PutObject(w.ctx, input); err != nil {
		mapped := mapError(err)
		w.setError(mapped)
		return mapped
	}
	w.done = true
	return nil
}

// complete 提交分片上传；失败时 Abort。
func (w *writer) complete() error {
	input := &awss3.CompleteMultipartUploadInput{
		Bucket:          aws.String(w.bucket),
		Key:             aws.String(w.key),
		UploadId:        w.uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: w.completedParts()},
	}
	if _, err := w.client.sdk.CompleteMultipartUpload(w.ctx, input); err != nil {
		mapped := mapError(err)
		w.setError(mapped)
		_ = w.abortUpload()
		return mapped
	}
	w.done = true
	return nil
}

// completedParts 按分片号升序收集已完成的分片。
func (w *writer) completedParts() []types.CompletedPart {
	w.mu.Lock()
	defer w.mu.Unlock()
	parts := make([]types.CompletedPart, 0, len(w.parts))
	for number := int32(1); number <= w.partNumber; number++ {
		parts = append(parts, w.parts[number])
	}
	return parts
}

// abortUpload 尽力取消未完成的分片上传，保留可见性：已提交的对象不受影响。
func (w *writer) abortUpload() error {
	if w.uploadID == nil || w.done {
		return nil
	}
	// 用不带取消的 ctx：调用方取消后仍要能清理未完成的分片。
	_, err := w.client.sdk.AbortMultipartUpload(context.WithoutCancel(w.ctx), &awss3.AbortMultipartUploadInput{
		Bucket:   aws.String(w.bucket),
		Key:      aws.String(w.key),
		UploadId: w.uploadID,
	})
	if err == nil {
		w.uploadID = nil
	}
	return err
}

// setError 记录首个错误；后台协程与调用方都可能写入。
func (w *writer) setError(err error) {
	w.mu.Lock()
	if w.err == nil {
		w.err = err
	}
	w.mu.Unlock()
}

// loadError 读取首个错误。
func (w *writer) loadError() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.err
}

// applyCreateOptions 把写入参数映射到 CreateMultipartUpload 请求。
func applyCreateOptions(input *awss3.CreateMultipartUploadInput, opts storage.WriterOptions) {
	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}
	if opts.ContentDisposition != "" {
		input.ContentDisposition = aws.String(opts.ContentDisposition)
	}
	if opts.CacheControl != "" {
		input.CacheControl = aws.String(opts.CacheControl)
	}
	if len(opts.Metadata) > 0 {
		input.Metadata = lowerMetadata(opts.Metadata)
	}
}

// applyWriterOptions 把写入参数映射到 PutObject 请求。
func applyWriterOptions(input *awss3.PutObjectInput, opts storage.WriterOptions) {
	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}
	if opts.ContentDisposition != "" {
		input.ContentDisposition = aws.String(opts.ContentDisposition)
	}
	if opts.CacheControl != "" {
		input.CacheControl = aws.String(opts.CacheControl)
	}
	if len(opts.Metadata) > 0 {
		input.Metadata = lowerMetadata(opts.Metadata)
	}
}
