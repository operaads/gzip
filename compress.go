package gzip

import (
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"strings"
	"sync"

	kgzip "github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"

	"github.com/gin-gonic/gin"
)

// Compress returns a middleware that automatically selects the best compression
// algorithm based on the client's Accept-Encoding header.
//
// The selection priority is: zstd > gzip. If the client supports both,
// zstd is preferred due to its better compression ratio and speed.
//
// Parameters:
//   - gzipLevel: compression level for gzip (use gzip constants like DefaultCompression)
//   - zstdLevel: compression level for zstd (use ZstdSpeedDefault, etc.)
//
// Use WithDecompressFn(DefaultCompressDecompressHandle) to also decompress
// both gzip and zstd encoded request bodies.
func Compress(gzipLevel int, zstdLevel zstd.EncoderLevel, options ...Option) gin.HandlerFunc {
	return newCompressHandler(gzipLevel, zstdLevel, options...).Handle
}

type compressHandler struct {
	*Options
	gzipPool sync.Pool
	zstdPool sync.Pool
}

func newCompressHandler(gzipLevel int, zstdLevel zstd.EncoderLevel, options ...Option) *compressHandler {
	// Create a copy of DefaultOptions to avoid mutating the shared instance
	opts := &Options{
		ExcludedExtensions:   DefaultOptions.ExcludedExtensions,
		ExcludedPaths:        DefaultOptions.ExcludedPaths,
		ExcludedPathesRegexs: DefaultOptions.ExcludedPathesRegexs,
		DecompressFn:         DefaultOptions.DecompressFn,
	}
	handler := &compressHandler{
		Options: opts,
		gzipPool: sync.Pool{
			New: func() interface{} {
				gz, err := kgzip.NewWriterLevel(io.Discard, gzipLevel)
				if err != nil {
					panic(err)
				}
				return gz
			},
		},
		zstdPool: sync.Pool{
			New: func() interface{} {
				enc, err := zstd.NewWriter(nil, zstd.WithEncoderLevel(zstdLevel))
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

func (h *compressHandler) Handle(c *gin.Context) {
	// Handle decompression for incoming requests
	if fn := h.DecompressFn; fn != nil {
		contentEncoding := c.Request.Header.Get("Content-Encoding")
		if contentEncoding == "zstd" || contentEncoding == "gzip" {
			fn(c)
		}
	}

	encoding := h.selectEncoding(c.Request)
	if encoding == "" {
		return
	}

	switch encoding {
	case "zstd":
		h.handleZstd(c)
	case "gzip":
		h.handleGzip(c)
	}
}

func (h *compressHandler) handleZstd(c *gin.Context) {
	enc := h.zstdPool.Get().(*zstd.Encoder)
	defer h.zstdPool.Put(enc)
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

func (h *compressHandler) handleGzip(c *gin.Context) {
	gz := h.gzipPool.Get().(*kgzip.Writer)
	defer h.gzipPool.Put(gz)
	defer gz.Reset(io.Discard)
	gz.Reset(c.Writer)

	c.Header("Content-Encoding", "gzip")
	c.Header("Vary", "Accept-Encoding")
	c.Writer = &gzipWriter{c.Writer, gz}
	defer func() {
		gz.Close()
		c.Header("Content-Length", fmt.Sprint(c.Writer.Size()))
	}()
	c.Next()
}

func (h *compressHandler) selectEncoding(req *http.Request) string {
	if strings.Contains(req.Header.Get("Connection"), "Upgrade") ||
		strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		return ""
	}

	extension := filepath.Ext(req.URL.Path)
	if h.ExcludedExtensions.Contains(extension) {
		return ""
	}

	if h.ExcludedPaths.Contains(req.URL.Path) {
		return ""
	}
	if h.ExcludedPathesRegexs.Contains(req.URL.Path) {
		return ""
	}

	acceptEncoding := req.Header.Get("Accept-Encoding")

	// Prefer zstd over gzip
	if strings.Contains(acceptEncoding, "zstd") {
		return "zstd"
	}
	if strings.Contains(acceptEncoding, "gzip") {
		return "gzip"
	}

	return ""
}

// DefaultCompressDecompressHandle handles both gzip and zstd request decompression using streaming.
// Note: For zstd, invalid data errors will occur when the handler reads the body.
// Handlers should handle read errors appropriately.
func DefaultCompressDecompressHandle(c *gin.Context) {
	if c.Request.Body == nil {
		return
	}

	contentEncoding := c.Request.Header.Get("Content-Encoding")

	switch contentEncoding {
	case "zstd":
		r, err := zstd.NewReader(c.Request.Body)
		if err != nil {
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}
		c.Request.Header.Del("Content-Encoding")
		c.Request.Header.Del("Content-Length")
		c.Request.Body = &zstdReadCloser{Decoder: r, underlying: c.Request.Body}

	case "gzip":
		r, err := kgzip.NewReader(c.Request.Body)
		if err != nil {
			_ = c.AbortWithError(http.StatusBadRequest, err)
			return
		}
		c.Request.Header.Del("Content-Encoding")
		c.Request.Header.Del("Content-Length")
		c.Request.Body = r
	}
}
