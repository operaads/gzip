package gzip

import (
	"github.com/klauspost/compress/gzip"

	"github.com/gin-gonic/gin"
)

const (
	BestCompression    = gzip.BestCompression
	BestSpeed          = gzip.BestSpeed
	DefaultCompression = gzip.DefaultCompression
	NoCompression      = gzip.NoCompression
)

// Gzip returns a middleware that compresses HTTP responses using gzip encoding
// for clients that support it via the Accept-Encoding header.
//
// The level parameter controls the compression level (use constants like
// DefaultCompression, BestSpeed, BestCompression, or NoCompression).
//
// Use WithDecompressFn(DefaultDecompressHandle) to also decompress gzip-encoded
// request bodies.
func Gzip(level int, options ...Option) gin.HandlerFunc {
	return newGzipHandler(level, options...).Handle
}

type gzipWriter struct {
	gin.ResponseWriter
	writer *gzip.Writer
}

func (g *gzipWriter) WriteString(s string) (int, error) {
	g.Header().Del("Content-Length")
	return g.writer.Write([]byte(s))
}

func (g *gzipWriter) Write(data []byte) (int, error) {
	g.Header().Del("Content-Length")
	return g.writer.Write(data)
}

// Fix: https://github.com/mholt/caddy/issues/38
func (g *gzipWriter) WriteHeader(code int) {
	g.Header().Del("Content-Length")
	// A status that cannot carry a body must not advertise an encoding either.
	// This has to happen here rather than in the middleware's deferred cleanup:
	// gin's AbortWithStatus and Render flush the headers for a no-body status
	// straight away, so by the time the cleanup runs they are already on the wire.
	if bodyAllowedForStatus(code) {
		g.Header().Set("Content-Encoding", "gzip")
	} else {
		g.Header().Del("Content-Encoding")
	}
	g.ResponseWriter.WriteHeader(code)
}
