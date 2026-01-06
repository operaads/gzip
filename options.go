package gzip

import (
	"net/http"
	"path/filepath"
	"regexp"
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
