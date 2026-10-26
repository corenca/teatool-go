package s3

import (
	"bytes"
	"context"
	"fmt"

	"github.com/aws/aws-sdk-go-v2/aws"
	awss3 "github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"

	"github.com/corenca/teatool-go/storage"
)

// writer 是 Put 返回的流式写入器：数据先入缓冲区，满分片后转分片上传。
type writer struct {
	client   *Client
	ctx      context.Context
	bucket   string
	key      string
	opts     storage.WriterOptions
	partSize int

	buf        bytes.Buffer
	uploadID   *string
	parts      []types.CompletedPart
	partNumber int32

	closed bool
	err    error
}

// Write 追加数据；缓冲区放不下时立即上传已缓存的整片。
func (w *writer) Write(p []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("%w: 写入器已关闭", storage.ErrInvalidArgument)
	}
	if w.err != nil {
		return 0, w.err
	}
	if w.buf.Len()+len(p) <= w.partSize {
		w.buf.Write(p)
		return len(p), nil
	}

	written := 0
	// 先用本批数据填满缓冲区并上传。
	if w.buf.Len() > 0 {
		need := w.partSize - w.buf.Len()
		w.buf.Write(p[:need])
		p = p[need:]
		written += need
		if err := w.uploadPart(w.buf.Bytes()); err != nil {
			return written, err
		}
		w.buf.Reset()
	}
	// 剩余数据按整片上传，尾部落回缓冲区。
	for len(p) >= w.partSize {
		if err := w.uploadPart(p[:w.partSize]); err != nil {
			return written, err
		}
		p = p[w.partSize:]
		written += w.partSize
	}
	w.buf.Write(p)
	written += len(p)
	return written, nil
}

// Close 完成上传。未超过一个分片时走单次 PutObject，否则完成分片上传；
// 失败时尽力 Abort 未完成的分片。
func (w *writer) Close() error {
	if w.closed {
		return w.err
	}
	w.closed = true
	if w.err != nil {
		w.abort()
		return w.err
	}
	if w.uploadID == nil {
		return w.putSingle()
	}
	if w.buf.Len() > 0 {
		if err := w.uploadPart(w.buf.Bytes()); err != nil {
			return err
		}
	}
	return w.complete()
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
		w.err = err
		return err
	}
	return nil
}

// uploadPart 上传一片；首片时惰性创建分片上传任务。
func (w *writer) uploadPart(data []byte) error {
	sdkClient := w.client.sdk
	if w.uploadID == nil {
		input := &awss3.CreateMultipartUploadInput{
			Bucket: aws.String(w.bucket),
			Key:    aws.String(w.key),
		}
		if w.opts.ContentType != "" {
			input.ContentType = aws.String(w.opts.ContentType)
		}
		if w.opts.ContentDisposition != "" {
			input.ContentDisposition = aws.String(w.opts.ContentDisposition)
		}
		if len(w.opts.Metadata) > 0 {
			input.Metadata = lowerMetadata(w.opts.Metadata)
		}
		out, err := sdkClient.CreateMultipartUpload(w.ctx, input)
		if err != nil {
			w.err = err
			return err
		}
		w.uploadID = out.UploadId
	}

	w.partNumber++
	out, err := sdkClient.UploadPart(w.ctx, &awss3.UploadPartInput{
		Bucket:     aws.String(w.bucket),
		Key:        aws.String(w.key),
		UploadId:   w.uploadID,
		PartNumber: aws.Int32(w.partNumber),
		Body:       bytes.NewReader(data),
	})
	if err != nil {
		w.err = err
		return err
	}
	w.parts = append(w.parts, types.CompletedPart{
		ETag:       out.ETag,
		PartNumber: aws.Int32(w.partNumber),
	})
	return nil
}

// complete 提交分片上传；失败时 Abort。
func (w *writer) complete() error {
	_, err := w.client.sdk.CompleteMultipartUpload(w.ctx, &awss3.CompleteMultipartUploadInput{
		Bucket:          aws.String(w.bucket),
		Key:             aws.String(w.key),
		UploadId:        w.uploadID,
		MultipartUpload: &types.CompletedMultipartUpload{Parts: w.parts},
	})
	if err != nil {
		w.err = err
		w.abort()
		return err
	}
	return nil
}

// abort 尽力取消未完成的分片上传，忽略自身错误。
func (w *writer) abort() {
	if w.uploadID == nil {
		return
	}
	// 用不带取消的 ctx：调用方取消后仍要能清理未完成的分片。
	_, _ = w.client.sdk.AbortMultipartUpload(context.WithoutCancel(w.ctx), &awss3.AbortMultipartUploadInput{
		Bucket:   aws.String(w.bucket),
		Key:      aws.String(w.key),
		UploadId: w.uploadID,
	})
}

// applyWriterOptions 把写入参数映射到 PutObject 请求。
func applyWriterOptions(input *awss3.PutObjectInput, opts storage.WriterOptions) {
	if opts.ContentType != "" {
		input.ContentType = aws.String(opts.ContentType)
	}
	if opts.ContentDisposition != "" {
		input.ContentDisposition = aws.String(opts.ContentDisposition)
	}
	if len(opts.Metadata) > 0 {
		input.Metadata = lowerMetadata(opts.Metadata)
	}
}
