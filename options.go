package gzip

import (
	"net/http"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/gzip"
	"github.com/klauspost/compress/zstd"
)

var (
	DefaultExcludedExtentions = NewExcludedExtensions([]string{
		".png", ".gif", ".jpeg", ".jpg",
	})
	DefaultOptions = &Options{
		ExcludedExtensions: DefaultExcludedExtentions,
	}
)

type Options struct {
	ExcludedExtensions   ExcludedExtensions
	ExcludedPaths        ExcludedPaths
	ExcludedPathesRegexs ExcludedPathesRegexs
	DecompressFn         func(c *gin.Context)
}

type Option func(*Options)

// shouldCompress checks if the request should be compressed with the given encoding.
// This is shared logic used by both gzip and zstd handlers.
func (o *Options) shouldCompress(req *http.Request, encoding string) bool {
	if !strings.Contains(req.Header.Get("Accept-Encoding"), encoding) ||
		strings.Contains(req.Header.Get("Connection"), "Upgrade") ||
		strings.Contains(req.Header.Get("Accept"), "text/event-stream") {
		return false
	}

	extension := filepath.Ext(req.URL.Path)
	if o.ExcludedExtensions.Contains(extension) {
		return false
	}

	if o.ExcludedPaths.Contains(req.URL.Path) {
		return false
	}
	if o.ExcludedPathesRegexs.Contains(req.URL.Path) {
		return false
	}

	return true
}

// bodyAllowedForStatus reports whether a response body is allowed for the
// given status code, mirroring net/http.bodyAllowedForStatus.
func bodyAllowedForStatus(status int) bool {
	switch {
	case status >= 100 && status <= 199:
		return false
	case status == http.StatusNoContent:
		return false
	case status == http.StatusNotModified:
		return false
	}
	return true
}

// finishEncoding closes out the response once the handler chain has run.
//
// For statuses that cannot carry a body (1xx/204/304) the encoded stream is
// dropped rather than closed: closing flushes an empty compressed stream (20
// bytes for gzip) into the response, which is then reported as Content-Length.
// RFC 7230 forbids a Content-Length on those statuses and strict clients reject
// the response outright -- okhttp fails the exchange with
// "java.net.ProtocolException: HTTP 204 had non-zero Content-Length". The
// encoder itself is discarded by the caller's deferred Reset, which runs after
// this.
//
// Content-Encoding is dropped in gzipWriter/zstdWriter.WriteHeader rather than
// here: gin's AbortWithStatus and Render flush the headers for a no-body status
// before this cleanup runs, so a change made here would never reach the wire.
//
// Vary is deliberately left in place. RFC 7232 section 4.1 requires it on a 304,
// and deleting it would also discard values contributed by other middleware
// (CORS adds "Vary: Origin", and a preflight is typically a 204).
func finishEncoding(c *gin.Context, closeEncoder func() error) {
	if !bodyAllowedForStatus(c.Writer.Status()) {
		return
	}
	_ = closeEncoder()
	c.Header("Content-Length", strconv.Itoa(c.Writer.Size()))
}

func WithExcludedExtensions(args []string) Option {
	return func(o *Options) {
		o.ExcludedExtensions = NewExcludedExtensions(args)
	}
}

func WithExcludedPaths(args []string) Option {
	return func(o *Options) {
		o.ExcludedPaths = NewExcludedPaths(args)
	}
}

func WithExcludedPathsRegexs(args []string) Option {
	return func(o *Options) {
		o.ExcludedPathesRegexs = NewExcludedPathesRegexs(args)
	}
}

func WithDecompressFn(decompressFn func(c *gin.Context)) Option {
	return func(o *Options) {
		o.DecompressFn = decompressFn
	}
}

// Using map for better lookup performance
type ExcludedExtensions map[string]bool

func NewExcludedExtensions(extensions []string) ExcludedExtensions {
	res := make(ExcludedExtensions)
	for _, e := range extensions {
		res[e] = true
	}
	return res
}

func (e ExcludedExtensions) Contains(target string) bool {
	_, ok := e[target]
	return ok
}

type ExcludedPaths []string

func NewExcludedPaths(paths []string) ExcludedPaths {
	return ExcludedPaths(paths)
}

func (e ExcludedPaths) Contains(requestURI string) bool {
	for _, path := range e {
		if strings.HasPrefix(requestURI, path) {
			return true
		}
	}
	return false
}

type ExcludedPathesRegexs []*regexp.Regexp

func NewExcludedPathesRegexs(regexs []string) ExcludedPathesRegexs {
	result := make([]*regexp.Regexp, len(regexs))
	for i, reg := range regexs {
		result[i] = regexp.MustCompile(reg)
	}
	return result
}

func (e ExcludedPathesRegexs) Contains(requestURI string) bool {
	for _, reg := range e {
		if reg.MatchString(requestURI) {
			return true
		}
	}
	return false
}

// DefaultDecompressHandle decompresses gzip-encoded request bodies.
// Use this with WithDecompressFn when using the Gzip middleware to handle
// compressed request bodies from clients.
//
// If decompression fails (e.g., invalid gzip data), the request is aborted
// with HTTP 400 Bad Request.
func DefaultDecompressHandle(c *gin.Context) {
	if c.Request.Body == nil {
		return
	}
	r, err := gzip.NewReader(c.Request.Body)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	c.Request.Header.Del("Content-Encoding")
	c.Request.Header.Del("Content-Length")
	c.Request.Body = r
}

// DefaultZstdDecompressHandle decompresses zstd-encoded request bodies.
// Use this with WithDecompressFn when using the Zstd middleware to handle
// compressed request bodies from clients.
//
// Note: Unlike gzip, zstd uses streaming decompression. The zstd.NewReader
// doesn't validate data on creation, so invalid data errors will occur when
// the handler reads the body. Handlers should handle read errors appropriately.
func DefaultZstdDecompressHandle(c *gin.Context) {
	if c.Request.Body == nil {
		return
	}
	r, err := zstd.NewReader(c.Request.Body)
	if err != nil {
		_ = c.AbortWithError(http.StatusBadRequest, err)
		return
	}
	c.Request.Header.Del("Content-Encoding")
	c.Request.Header.Del("Content-Length")
	c.Request.Body = &zstdReadCloser{Decoder: r, underlying: c.Request.Body}
}
