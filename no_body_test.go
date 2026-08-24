package gzip

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"github.com/gin-gonic/gin"
	kgzip "github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
	"github.com/stretchr/testify/assert"
)

func decodeBody(t *testing.T, encoding string, body io.Reader) string {
	t.Helper()

	var r io.Reader
	switch encoding {
	case "gzip":
		gr, err := kgzip.NewReader(body)
		assert.NoError(t, err)
		defer gr.Close()
		r = gr
	case "zstd":
		dec, err := zstd.NewReader(body)
		assert.NoError(t, err)
		defer dec.Close()
		r = dec
	default:
		t.Fatalf("unexpected encoding %q", encoding)
	}

	out, err := io.ReadAll(r)
	assert.NoError(t, err)
	return string(out)
}

// Responses without a body (1xx/204/304) must not carry Content-Length or a
// Content-Encoding: RFC 7230 forbids a Content-Length on them, and strict
// clients (e.g. okhttp) fail the whole response with a ProtocolException.
//
// Every assertion runs against w.Result().Header, the snapshot taken when the
// headers were written. w.Header() is the live map and keeps reflecting edits
// made after gin has already flushed the headers -- which is exactly what
// happens on the AbortWithStatus and Render paths, so asserting on it would
// report a pass for headers that still went out on the wire.
func testNoBodyResponse(t *testing.T, mw gin.HandlerFunc, acceptEncoding string, status int) {
	t.Helper()

	router := gin.New()
	router.Use(mw)
	// gin flushes the headers immediately for the abort and render paths, and
	// only at the end of the chain for a bare Status, so all three are covered.
	router.GET("/status", func(c *gin.Context) {
		c.Status(status)
	})
	router.GET("/abort", func(c *gin.Context) {
		c.AbortWithStatus(status)
	})
	router.GET("/json", func(c *gin.Context) {
		c.JSON(status, gin.H{})
	})
	// gin drops the payload for a no-body status, so this must come out empty too.
	router.GET("/json-with-body", func(c *gin.Context) {
		c.JSON(status, gin.H{"debug": true})
	})

	for _, path := range []string{"/status", "/abort", "/json", "/json-with-body"} {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", path, nil)
		req.Header.Add("Accept-Encoding", acceptEncoding)

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		header := w.Result().Header
		assert.Equal(t, status, w.Code, path)
		assert.Empty(t, header.Get("Content-Length"), path)
		assert.Empty(t, header.Get("Content-Encoding"), path)
		assert.Zero(t, w.Body.Len(), path)
		// Vary stays: RFC 7232 section 4.1 requires it on a 304.
		assert.Equal(t, "Accept-Encoding", header.Get("Vary"), path)
	}
}

// A no-body request must not leave the pooled encoder in a state that corrupts
// the next response that does carry a body.
func testEncoderReuseAfterNoBody(t *testing.T, mw gin.HandlerFunc, acceptEncoding, wantEncoding string) {
	t.Helper()

	router := gin.New()
	router.Use(mw)
	router.GET("/no-content", func(c *gin.Context) {
		c.AbortWithStatus(http.StatusNoContent)
	})
	router.GET("/body", func(c *gin.Context) {
		c.String(http.StatusOK, testResponse)
	})

	for i := 0; i < 2; i++ {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", "/no-content", nil)
		req.Header.Add("Accept-Encoding", acceptEncoding)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, http.StatusNoContent, w.Code)
		assert.Empty(t, w.Result().Header.Get("Content-Length"))
	}

	req, _ := http.NewRequestWithContext(context.Background(), "GET", "/body", nil)
	req.Header.Add("Accept-Encoding", acceptEncoding)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	header := w.Result().Header
	assert.Equal(t, http.StatusOK, w.Code)
	assert.Equal(t, wantEncoding, header.Get("Content-Encoding"))
	assert.Equal(t, "Accept-Encoding", header.Get("Vary"))
	// Content-Length is asserted on the live map, not the snapshot: the
	// middleware only knows the encoded size once the encoder is closed in the
	// deferred cleanup, which runs after the headers have been written. net/http
	// computes its own Content-Length for a buffered response; adapters that
	// serialize the header map after the handler returns pick this one up.
	assert.Equal(t, strconv.Itoa(w.Body.Len()), w.Header().Get("Content-Length"))
	assert.Equal(t, testResponse, decodeBody(t, wantEncoding, w.Body))
}

func TestGzipNoBodyResponse(t *testing.T) {
	testNoBodyResponse(t, Gzip(DefaultCompression), "gzip", http.StatusNoContent)
	testNoBodyResponse(t, Gzip(DefaultCompression), "gzip", http.StatusNotModified)
}

func TestZstdNoBodyResponse(t *testing.T) {
	testNoBodyResponse(t, Zstd(ZstdSpeedDefault), "zstd", http.StatusNoContent)
	testNoBodyResponse(t, Zstd(ZstdSpeedDefault), "zstd", http.StatusNotModified)
}

func TestCompressNoBodyResponse(t *testing.T) {
	for _, status := range []int{http.StatusNoContent, http.StatusNotModified} {
		testNoBodyResponse(t, Compress(DefaultCompression, ZstdSpeedDefault), "gzip, zstd", status)
		testNoBodyResponse(t, Compress(DefaultCompression, ZstdSpeedDefault), "gzip", status)
	}
}

func TestGzipEncoderReuseAfterNoBodyResponse(t *testing.T) {
	testEncoderReuseAfterNoBody(t, Gzip(DefaultCompression), "gzip", "gzip")
}

func TestZstdEncoderReuseAfterNoBodyResponse(t *testing.T) {
	testEncoderReuseAfterNoBody(t, Zstd(ZstdSpeedDefault), "zstd", "zstd")
}

func TestCompressEncoderReuseAfterNoBodyResponse(t *testing.T) {
	testEncoderReuseAfterNoBody(t, Compress(DefaultCompression, ZstdSpeedDefault), "gzip, zstd", "zstd")
	testEncoderReuseAfterNoBody(t, Compress(DefaultCompression, ZstdSpeedDefault), "gzip", "gzip")
}

// Vary is not ours alone to delete: CORS middleware adds "Vary: Origin" and a
// preflight is typically a 204, so dropping the header on a no-body response
// would silently break cache keying for other middleware.
func TestNoBodyResponseKeepsForeignVary(t *testing.T) {
	router := gin.New()
	router.Use(Gzip(DefaultCompression))
	router.Use(func(c *gin.Context) {
		c.Writer.Header().Add("Vary", "Origin")
		c.Next()
	})
	router.GET("/preflight", func(c *gin.Context) {
		c.Status(http.StatusNoContent)
	})
	router.GET("/not-modified", func(c *gin.Context) {
		c.Status(http.StatusNotModified)
	})

	for _, path := range []string{"/preflight", "/not-modified"} {
		req, _ := http.NewRequestWithContext(context.Background(), "GET", path, nil)
		req.Header.Add("Accept-Encoding", "gzip")

		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, []string{"Accept-Encoding", "Origin"}, w.Result().Header.Values("Vary"), path)
	}
}
