package gzip

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	kgzip "github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

const (
	testCompressResponse = "Compress Test Response "
)

func newCompressServer() *gin.Engine {
	router := gin.New()
	router.Use(Compress(DefaultCompression, ZstdSpeedDefault))
	router.GET("/", func(c *gin.Context) {
		c.Header("Content-Length", strconv.Itoa(len(testCompressResponse)))
		c.String(200, testCompressResponse)
	})
	return router
}

func TestCompressPreferZstd(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.Header.Add("Accept-Encoding", "gzip, zstd, br")

	w := httptest.NewRecorder()
	r := newCompressServer()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "zstd", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", w.Header().Get("Vary"))
	assert.NotEqual(t, "0", w.Header().Get("Content-Length"))
	assert.Equal(t, fmt.Sprint(w.Body.Len()), w.Header().Get("Content-Length"))

	dec, err := zstd.NewReader(w.Body)
	assert.NoError(t, err)
	defer dec.Close()

	body, _ := io.ReadAll(dec)
	assert.Equal(t, testCompressResponse, string(body))
}

func TestCompressFallbackGzip(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.Header.Add("Accept-Encoding", "gzip, br")

	w := httptest.NewRecorder()
	r := newCompressServer()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "gzip", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", w.Header().Get("Vary"))
	assert.NotEqual(t, "0", w.Header().Get("Content-Length"))
	assert.Equal(t, fmt.Sprint(w.Body.Len()), w.Header().Get("Content-Length"))

	gr, err := kgzip.NewReader(w.Body)
	assert.NoError(t, err)
	defer gr.Close()

	body, _ := io.ReadAll(gr)
	assert.Equal(t, testCompressResponse, string(body))
}

func TestCompressNoEncoding(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)

	w := httptest.NewRecorder()
	r := newCompressServer()
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	assert.Equal(t, strconv.Itoa(len(testCompressResponse)), w.Header().Get("Content-Length"))
	assert.Equal(t, testCompressResponse, w.Body.String())
}

// TestCompressMiddlewareChainContinuation verifies that the middleware chain
// continues correctly when no compression encoding is accepted by the client.
func TestCompressMiddlewareChainContinuation(t *testing.T) {
	middlewareCalled := false
	handlerCalled := false

	router := gin.New()
	router.Use(Compress(DefaultCompression, ZstdSpeedDefault))
	// Add another middleware after Compress to verify chain continuation
	router.Use(func(c *gin.Context) {
		middlewareCalled = true
		c.Next()
	})
	router.GET("/", func(c *gin.Context) {
		handlerCalled = true
		c.String(200, "ok")
	})

	// Request with unsupported encoding (only br, no gzip or zstd)
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.Header.Add("Accept-Encoding", "br")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "ok", w.Body.String())
	assert.True(t, middlewareCalled, "middleware after Compress should be called")
	assert.True(t, handlerCalled, "handler should be called")
}

func TestCompressExcludedPaths(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/api/data", nil)
	req.Header.Add("Accept-Encoding", "gzip, zstd")

	router := gin.New()
	router.Use(Compress(DefaultCompression, ZstdSpeedDefault, WithExcludedPaths([]string{"/api/"})))
	router.GET("/api/data", func(c *gin.Context) {
		c.String(200, "api data")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "api data", w.Body.String())
}

func TestCompressExcludedExtensions(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/image.png", nil)
	req.Header.Add("Accept-Encoding", "gzip, zstd")

	router := gin.New()
	router.Use(Compress(DefaultCompression, ZstdSpeedDefault))
	router.GET("/image.png", func(c *gin.Context) {
		c.String(200, "this is a PNG!")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "this is a PNG!", w.Body.String())
}

func TestCompressDecompressGzip(t *testing.T) {
	buf := &bytes.Buffer{}
	gz, _ := kgzip.NewWriterLevel(buf, kgzip.DefaultCompression)
	if _, err := gz.Write([]byte(testCompressResponse)); err != nil {
		gz.Close()
		t.Fatal(err)
	}
	gz.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/", buf)
	req.Header.Add("Content-Encoding", "gzip")

	router := gin.New()
	router.Use(Compress(DefaultCompression, ZstdSpeedDefault, WithDecompressFn(DefaultCompressDecompressHandle)))
	router.POST("/", func(c *gin.Context) {
		if v := c.Request.Header.Get("Content-Encoding"); v != "" {
			t.Errorf("unexpected `Content-Encoding`: %s header", v)
		}
		if v := c.Request.Header.Get("Content-Length"); v != "" {
			t.Errorf("unexpected `Content-Length`: %s header", v)
		}
		data, err := c.GetRawData()
		if err != nil {
			t.Fatal(err)
		}
		c.Data(200, "text/plain", data)
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	assert.Equal(t, testCompressResponse, w.Body.String())
}

func TestCompressDecompressZstd(t *testing.T) {
	buf := &bytes.Buffer{}
	enc, _ := zstd.NewWriter(buf, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if _, err := enc.Write([]byte(testCompressResponse)); err != nil {
		enc.Close()
		t.Fatal(err)
	}
	enc.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/", buf)
	req.Header.Add("Content-Encoding", "zstd")

	router := gin.New()
	router.Use(Compress(DefaultCompression, ZstdSpeedDefault, WithDecompressFn(DefaultCompressDecompressHandle)))
	router.POST("/", func(c *gin.Context) {
		if v := c.Request.Header.Get("Content-Encoding"); v != "" {
			t.Errorf("unexpected `Content-Encoding`: %s header", v)
		}
		if v := c.Request.Header.Get("Content-Length"); v != "" {
			t.Errorf("unexpected `Content-Length`: %s header", v)
		}
		data, err := c.GetRawData()
		if err != nil {
			t.Fatal(err)
		}
		c.Data(200, "text/plain", data)
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	assert.Equal(t, testCompressResponse, w.Body.String())
}

func TestCompressDecompressWithIncorrectGzipData(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/", bytes.NewReader([]byte("invalid data")))
	req.Header.Add("Content-Encoding", "gzip")

	router := gin.New()
	router.Use(Compress(DefaultCompression, ZstdSpeedDefault, WithDecompressFn(DefaultCompressDecompressHandle)))
	router.POST("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

// TestCompressDecompressWithIncorrectZstdData tests that invalid zstd data causes an error
// when the handler reads the body (streaming decompression behavior).
func TestCompressDecompressWithIncorrectZstdData(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/", bytes.NewReader([]byte("invalid data")))
	req.Header.Add("Content-Encoding", "zstd")

	router := gin.New()
	router.Use(Compress(DefaultCompression, ZstdSpeedDefault, WithDecompressFn(DefaultCompressDecompressHandle)))
	router.POST("/", func(c *gin.Context) {
		// With streaming decompression, the error occurs when reading the body
		_, err := c.GetRawData()
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "error")
}
