package gzip

import (
	"fmt"
	"io"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/zstd"
)

type zstdHandler struct {
	*Options
	zstdPool sync.Pool
}

func newZstdHandler(level zstd.EncoderLevel, options ...Option) *zstdHandler {
	// Create a copy of DefaultOptions to avoid mutating the shared instance
	opts := &Options{
		ExcludedExtensions:   DefaultOptions.ExcludedExtensions,
		ExcludedPaths:        DefaultOptions.ExcludedPaths,
		ExcludedPathesRegexs: DefaultOptions.ExcludedPathesRegexs,
		DecompressFn:         DefaultOptions.DecompressFn,
	}
	handler := &zstdHandler{
		Options: opts,
		zstdPool: sync.Pool{
			New: func() interface{} {
				enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(level))
				if err != nil {
					panic(err)
				}
				return enc
			},
		},
	}
	for _, setter := range options {
		setter(handler.Options)
	}
	return handler
}

func (z *zstdHandler) Handle(c *gin.Context) {
	if fn := z.DecompressFn; fn != nil && c.Request.Header.Get("Content-Encoding") == "zstd" {
		fn(c)
	}

	if !z.Options.shouldCompress(c.Request, "zstd") {
		return
	}

	enc := z.zstdPool.Get().(*zstd.Encoder)
	defer z.zstdPool.Put(enc)
	defer enc.Reset(nil)
	enc.Reset(c.Writer)

	c.Header("Content-Encoding", "zstd")
	c.Header("Vary", "Accept-Encoding")
	c.Writer = &zstdWriter{c.Writer, enc}
	defer func() {
		enc.Close()
		c.Header("Content-Length", fmt.Sprint(c.Writer.Size()))
	}()
	c.Next()
}

// zstdReadCloser wraps a zstd.Decoder to implement io.ReadCloser
type zstdReadCloser struct {
	*zstd.Decoder
	underlying io.Closer
}

func (z *zstdReadCloser) Close() error {
	z.Decoder.Close()
	if z.underlying != nil {
		return z.underlying.Close()
	}
	return nil
}
