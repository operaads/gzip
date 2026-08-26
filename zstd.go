package gzip

import (
	"github.com/klauspost/compress/zstd"

	"github.com/gin-gonic/gin"
)

// Zstd compression levels for use with the Zstd middleware.
// These map directly to github.com/klauspost/compress/zstd encoder levels.
const (
	ZstdSpeedFastest           = zstd.SpeedFastest           // Fastest compression, lower ratio
	ZstdSpeedDefault           = zstd.SpeedDefault           // Default balance of speed and compression
	ZstdSpeedBetterCompression = zstd.SpeedBetterCompression // Better compression, slower
	ZstdSpeedBestCompression   = zstd.SpeedBestCompression   // Best compression, slowest
)

// Zstd returns a middleware that compresses HTTP responses using zstd encoding
// for clients that support it via the Accept-Encoding header.
//
// The level parameter controls the compression level (use constants like
// ZstdSpeedDefault, ZstdSpeedFastest, ZstdSpeedBetterCompression, or ZstdSpeedBestCompression).
//
// Use WithDecompressFn(DefaultZstdDecompressHandle) to also decompress zstd-encoded
// request bodies.
func Zstd(level zstd.EncoderLevel, options ...Option) gin.HandlerFunc {
	return newZstdHandler(level, options...).Handle
}

func newZstdEncoder(level zstd.EncoderLevel) *zstd.Encoder {
	enc, err := zstd.NewWriter(
		nil,
		zstd.WithEncoderLevel(level),
		zstd.WithEncoderConcurrency(1),
		zstd.WithWindowSize(128<<10),
		zstd.WithLowerEncoderMem(true),
	)
	if err != nil {
		panic(err)
	}
	return enc
}

type zstdWriter struct {
	gin.ResponseWriter
	writer *zstd.Encoder
}

func (z *zstdWriter) WriteString(s string) (int, error) {
	z.Header().Del("Content-Length")
	return z.writer.Write([]byte(s))
}

func (z *zstdWriter) Write(data []byte) (int, error) {
	z.Header().Del("Content-Length")
	return z.writer.Write(data)
}

// See gzipWriter.WriteHeader for why Content-Encoding is adjusted here.
func (z *zstdWriter) WriteHeader(code int) {
	z.Header().Del("Content-Length")
	if bodyAllowedForStatus(code) {
		z.Header().Set("Content-Encoding", "zstd")
	} else {
		z.Header().Del("Content-Encoding")
	}
	z.ResponseWriter.WriteHeader(code)
}
