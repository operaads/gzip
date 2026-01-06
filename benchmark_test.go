package gzip

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	kgzip "github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"

	"github.com/gin-gonic/gin"
)

func init() {
	gin.SetMode(gin.ReleaseMode)
}

// Test data of various sizes
var (
	smallPayload  = strings.Repeat("Hello, World! ", 10)                  // ~140 bytes
	mediumPayload = strings.Repeat("The quick brown fox jumps. ", 1000)   // ~27KB
	largePayload  = strings.Repeat("Lorem ipsum dolor sit amet. ", 10000) // ~280KB
)

// ============================================================================
// Response Compression Benchmarks
// ============================================================================

func BenchmarkGzipCompression_Small(b *testing.B) {
	benchmarkGzipCompression(b, smallPayload)
}

func BenchmarkZstdCompression_Small(b *testing.B) {
	benchmarkZstdCompression(b, smallPayload)
}

func BenchmarkCompressAuto_Small(b *testing.B) {
	benchmarkCompressAuto(b, smallPayload, "zstd, gzip")
}

func BenchmarkGzipCompression_Medium(b *testing.B) {
	benchmarkGzipCompression(b, mediumPayload)
}

func BenchmarkZstdCompression_Medium(b *testing.B) {
	benchmarkZstdCompression(b, mediumPayload)
}

func BenchmarkCompressAuto_Medium(b *testing.B) {
	benchmarkCompressAuto(b, mediumPayload, "zstd, gzip")
}

func BenchmarkGzipCompression_Large(b *testing.B) {
	benchmarkGzipCompression(b, largePayload)
}

func BenchmarkZstdCompression_Large(b *testing.B) {
	benchmarkZstdCompression(b, largePayload)
}

func BenchmarkCompressAuto_Large(b *testing.B) {
	benchmarkCompressAuto(b, largePayload, "zstd, gzip")
}

func benchmarkGzipCompression(b *testing.B, payload string) {
	router := gin.New()
	router.Use(Gzip(DefaultCompression))
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, payload)
	})

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func benchmarkZstdCompression(b *testing.B, payload string) {
	router := gin.New()
	router.Use(Zstd(ZstdSpeedDefault))
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, payload)
	})

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.Header.Set("Accept-Encoding", "zstd")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func benchmarkCompressAuto(b *testing.B, payload string, acceptEncoding string) {
	router := gin.New()
	router.Use(Compress(DefaultCompression, ZstdSpeedDefault))
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, payload)
	})

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.Header.Set("Accept-Encoding", acceptEncoding)

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// ============================================================================
// Request Decompression Benchmarks
// ============================================================================

func BenchmarkGzipDecompression_Small(b *testing.B) {
	benchmarkGzipDecompression(b, smallPayload)
}

func BenchmarkZstdDecompression_Small(b *testing.B) {
	benchmarkZstdDecompression(b, smallPayload)
}

func BenchmarkGzipDecompression_Medium(b *testing.B) {
	benchmarkGzipDecompression(b, mediumPayload)
}

func BenchmarkZstdDecompression_Medium(b *testing.B) {
	benchmarkZstdDecompression(b, mediumPayload)
}

func BenchmarkGzipDecompression_Large(b *testing.B) {
	benchmarkGzipDecompression(b, largePayload)
}

func BenchmarkZstdDecompression_Large(b *testing.B) {
	benchmarkZstdDecompression(b, largePayload)
}

func benchmarkGzipDecompression(b *testing.B, payload string) {
	// Pre-compress the payload
	buf := &bytes.Buffer{}
	gz, _ := kgzip.NewWriterLevel(buf, kgzip.DefaultCompression)
	gz.Write([]byte(payload))
	gz.Close()
	compressed := buf.Bytes()

	router := gin.New()
	router.Use(Gzip(DefaultCompression, WithDecompressFn(DefaultDecompressHandle)))
	router.POST("/", func(c *gin.Context) {
		io.Copy(io.Discard, c.Request.Body)
		c.String(http.StatusOK, "ok")
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "/", bytes.NewReader(compressed))
		req.Header.Set("Content-Encoding", "gzip")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func benchmarkZstdDecompression(b *testing.B, payload string) {
	// Pre-compress the payload
	buf := &bytes.Buffer{}
	enc, _ := zstd.NewWriter(buf, zstd.WithEncoderLevel(zstd.SpeedDefault))
	enc.Write([]byte(payload))
	enc.Close()
	compressed := buf.Bytes()

	router := gin.New()
	router.Use(Zstd(ZstdSpeedDefault, WithDecompressFn(DefaultZstdDecompressHandle)))
	router.POST("/", func(c *gin.Context) {
		io.Copy(io.Discard, c.Request.Body)
		c.String(http.StatusOK, "ok")
	})

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), "POST", "/", bytes.NewReader(compressed))
		req.Header.Set("Content-Encoding", "zstd")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// ============================================================================
// Compression Level Benchmarks
// ============================================================================

func BenchmarkGzip_BestSpeed(b *testing.B) {
	benchmarkGzipLevel(b, BestSpeed, mediumPayload)
}

func BenchmarkGzip_DefaultCompression(b *testing.B) {
	benchmarkGzipLevel(b, DefaultCompression, mediumPayload)
}

func BenchmarkGzip_BestCompression(b *testing.B) {
	benchmarkGzipLevel(b, BestCompression, mediumPayload)
}

func BenchmarkZstd_SpeedFastest(b *testing.B) {
	benchmarkZstdLevel(b, ZstdSpeedFastest, mediumPayload)
}

func BenchmarkZstd_SpeedDefault(b *testing.B) {
	benchmarkZstdLevel(b, ZstdSpeedDefault, mediumPayload)
}

func BenchmarkZstd_SpeedBetterCompression(b *testing.B) {
	benchmarkZstdLevel(b, ZstdSpeedBetterCompression, mediumPayload)
}

func BenchmarkZstd_SpeedBestCompression(b *testing.B) {
	benchmarkZstdLevel(b, ZstdSpeedBestCompression, mediumPayload)
}

func benchmarkGzipLevel(b *testing.B, level int, payload string) {
	router := gin.New()
	router.Use(Gzip(level))
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, payload)
	})

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.Header.Set("Accept-Encoding", "gzip")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

func benchmarkZstdLevel(b *testing.B, level zstd.EncoderLevel, payload string) {
	router := gin.New()
	router.Use(Zstd(level))
	router.GET("/", func(c *gin.Context) {
		c.String(http.StatusOK, payload)
	})

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.Header.Set("Accept-Encoding", "zstd")

	b.ResetTimer()
	b.ReportAllocs()

	for i := 0; i < b.N; i++ {
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
	}
}

// ============================================================================
// Compression Ratio Comparison (not a benchmark, but useful for comparison)
// ============================================================================

func BenchmarkCompressionRatio(b *testing.B) {
	payloads := map[string]string{
		"small":  smallPayload,
		"medium": mediumPayload,
		"large":  largePayload,
	}

	for name, payload := range payloads {
		// Gzip compression
		gzipBuf := &bytes.Buffer{}
		gz, _ := kgzip.NewWriterLevel(gzipBuf, kgzip.DefaultCompression)
		gz.Write([]byte(payload))
		gz.Close()

		// Zstd compression
		zstdBuf := &bytes.Buffer{}
		enc, _ := zstd.NewWriter(zstdBuf, zstd.WithEncoderLevel(zstd.SpeedDefault))
		enc.Write([]byte(payload))
		enc.Close()

		b.Logf("%s payload: original=%d, gzip=%d (%.1f%%), zstd=%d (%.1f%%)",
			name,
			len(payload),
			gzipBuf.Len(), float64(gzipBuf.Len())/float64(len(payload))*100,
			zstdBuf.Len(), float64(zstdBuf.Len())/float64(len(payload))*100,
		)
	}
}
