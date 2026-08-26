package gzip

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"strconv"
	"testing"

	"github.com/klauspost/compress/zstd"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
)

const (
	testZstdResponse        = "Zstd Test Response "
	testZstdReverseResponse = "Zstd Test Reverse Response "
)

type zstdRServer struct{}

func (s *zstdRServer) ServeHTTP(rw http.ResponseWriter, req *http.Request) {
	fmt.Fprint(rw, testZstdReverseResponse)
}

func newZstdServer(t *testing.T) *gin.Engine {
	// init reverse proxy server
	rServer := httptest.NewServer(new(zstdRServer))
	t.Cleanup(rServer.Close)
	target, _ := url.Parse(rServer.URL)
	rp := httputil.NewSingleHostReverseProxy(target)

	router := gin.New()
	router.Use(Zstd(ZstdSpeedDefault))
	router.GET("/", func(c *gin.Context) {
		c.Header("Content-Length", strconv.Itoa(len(testZstdResponse)))
		c.String(200, testZstdResponse)
	})
	router.Any("/reverse", func(c *gin.Context) {
		rp.ServeHTTP(c.Writer, c.Request)
	})
	return router
}

func TestZstd(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)
	req.Header.Add("Accept-Encoding", "zstd")

	w := httptest.NewRecorder()
	r := newZstdServer(t)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "zstd", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", w.Header().Get("Vary"))
	assert.NotEqual(t, "0", w.Header().Get("Content-Length"))
	assert.NotEqual(t, len(testZstdResponse), w.Body.Len())
	assert.Equal(t, fmt.Sprint(w.Body.Len()), w.Header().Get("Content-Length"))

	dec, err := zstd.NewReader(w.Body)
	assert.NoError(t, err)
	defer dec.Close()

	body, _ := io.ReadAll(dec)
	assert.Equal(t, testZstdResponse, string(body))
}

func TestZstdPNG(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/image.png", nil)
	req.Header.Add("Accept-Encoding", "zstd")

	router := gin.New()
	router.Use(Zstd(ZstdSpeedDefault))
	router.GET("/image.png", func(c *gin.Context) {
		c.String(200, "this is a PNG!")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "", w.Header().Get("Vary"))
	assert.Equal(t, "this is a PNG!", w.Body.String())
}

func TestExcludedExtensionsZstd(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/index.html", nil)
	req.Header.Add("Accept-Encoding", "zstd")

	router := gin.New()
	router.Use(Zstd(ZstdSpeedDefault, WithExcludedExtensions([]string{".html"})))
	router.GET("/index.html", func(c *gin.Context) {
		c.String(200, "this is a HTML!")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "", w.Header().Get("Vary"))
	assert.Equal(t, "this is a HTML!", w.Body.String())
	assert.Equal(t, "", w.Header().Get("Content-Length"))
}

func TestExcludedPathsZstd(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/api/books", nil)
	req.Header.Add("Accept-Encoding", "zstd")

	router := gin.New()
	router.Use(Zstd(ZstdSpeedDefault, WithExcludedPaths([]string{"/api/"})))
	router.GET("/api/books", func(c *gin.Context) {
		c.String(200, "this is books!")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "", w.Header().Get("Vary"))
	assert.Equal(t, "this is books!", w.Body.String())
	assert.Equal(t, "", w.Header().Get("Content-Length"))
}

func TestNoZstd(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/", nil)

	w := httptest.NewRecorder()
	r := newZstdServer(t)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	assert.Equal(t, strconv.Itoa(len(testZstdResponse)), w.Header().Get("Content-Length"))
	assert.Equal(t, testZstdResponse, w.Body.String())
}

func TestZstdWithReverseProxy(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/reverse", nil)
	req.Header.Add("Accept-Encoding", "zstd")

	w := newCloseNotifyingRecorder()
	r := newZstdServer(t)
	r.ServeHTTP(w, req)

	assert.Equal(t, 200, w.Code)
	assert.Equal(t, "zstd", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", w.Header().Get("Vary"))
	assert.NotEqual(t, "0", w.Header().Get("Content-Length"))
	assert.NotEqual(t, len(testZstdReverseResponse), w.Body.Len())
	assert.Equal(t, fmt.Sprint(w.Body.Len()), w.Header().Get("Content-Length"))

	dec, err := zstd.NewReader(w.Body)
	assert.NoError(t, err)
	defer dec.Close()

	body, _ := io.ReadAll(dec)
	assert.Equal(t, testZstdReverseResponse, string(body))
}

func TestDecompressZstd(t *testing.T) {
	buf := &bytes.Buffer{}
	enc, _ := zstd.NewWriter(buf, zstd.WithEncoderLevel(zstd.SpeedDefault))
	if _, err := enc.Write([]byte(testZstdResponse)); err != nil {
		enc.Close()
		t.Fatal(err)
	}
	enc.Close()

	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/", buf)
	req.Header.Add("Content-Encoding", "zstd")

	router := gin.New()
	router.Use(Zstd(ZstdSpeedDefault, WithDecompressFn(DefaultZstdDecompressHandle)))
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
	assert.Equal(t, "", w.Header().Get("Vary"))
	assert.Equal(t, testZstdResponse, w.Body.String())
	assert.Equal(t, "", w.Header().Get("Content-Length"))
}

func TestDecompressZstdRejectsLargeWindow(t *testing.T) {
	payload := bytes.Repeat([]byte("large-window-payload-"), 20<<10)
	var compressed bytes.Buffer
	enc, err := zstd.NewWriter(
		&compressed,
		zstd.WithEncoderLevel(zstd.SpeedFastest),
		zstd.WithEncoderConcurrency(1),
		zstd.WithWindowSize(ZstdWindowSize*2),
	)
	assert.NoError(t, err)
	_, err = enc.Write(payload)
	assert.NoError(t, err)
	assert.NoError(t, enc.Close())

	req, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "/", &compressed)
	req.Header.Set("Content-Encoding", "zstd")

	router := gin.New()
	router.Use(Zstd(ZstdSpeedDefault, WithDecompressFn(DefaultZstdDecompressHandle)))
	router.POST("/", func(c *gin.Context) {
		_, readErr := io.Copy(io.Discard, c.Request.Body)
		assert.Error(t, readErr)
		c.Status(http.StatusBadRequest)
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestDecompressZstdWithEmptyBody(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/", nil)
	req.Header.Add("Content-Encoding", "zstd")

	router := gin.New()
	router.Use(Zstd(ZstdSpeedDefault, WithDecompressFn(DefaultZstdDecompressHandle)))
	router.POST("/", func(c *gin.Context) {
		c.String(200, "ok")
	})

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, "", w.Header().Get("Content-Encoding"))
	assert.Equal(t, "", w.Header().Get("Vary"))
	assert.Equal(t, "ok", w.Body.String())
	assert.Equal(t, "", w.Header().Get("Content-Length"))
}

// TestDecompressZstdWithIncorrectData tests that invalid zstd data causes an error
// when the handler reads the body (streaming decompression behavior).
func TestDecompressZstdWithIncorrectData(t *testing.T) {
	req, _ := http.NewRequestWithContext(context.Background(), "POST", "/", bytes.NewReader([]byte(testZstdResponse)))
	req.Header.Add("Content-Encoding", "zstd")

	router := gin.New()
	router.Use(Zstd(ZstdSpeedDefault, WithDecompressFn(DefaultZstdDecompressHandle)))
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
