package gzip

import (
	"io"
	"sync"

	"github.com/gin-gonic/gin"
	"github.com/klauspost/compress/gzip"
)

type gzipHandler struct {
	*Options
	gzPool sync.Pool
}

func newGzipHandler(level int, options ...Option) *gzipHandler {
	// Create a copy of DefaultOptions to avoid mutating the shared instance
	opts := &Options{
		ExcludedExtensions:   DefaultOptions.ExcludedExtensions,
		ExcludedPaths:        DefaultOptions.ExcludedPaths,
		ExcludedPathesRegexs: DefaultOptions.ExcludedPathesRegexs,
		DecompressFn:         DefaultOptions.DecompressFn,
	}
	handler := &gzipHandler{
		Options: opts,
		gzPool: sync.Pool{
			New: func() interface{} {
				gz, err := gzip.NewWriterLevel(io.Discard, level)
				if err != nil {
					panic(err)
				}
				return gz
			},
		},
	}
	for _, setter := range options {
		setter(handler.Options)
	}
	return handler
}

func (g *gzipHandler) Handle(c *gin.Context) {
	if fn := g.DecompressFn; fn != nil && c.Request.Header.Get("Content-Encoding") == "gzip" {
		fn(c)
	}

	if !g.Options.shouldCompress(c.Request, "gzip") {
		return
	}

	gz := g.gzPool.Get().(*gzip.Writer)
	defer g.gzPool.Put(gz)
	defer gz.Reset(io.Discard)
	gz.Reset(c.Writer)

	c.Header("Content-Encoding", "gzip")
	c.Header("Vary", "Accept-Encoding")
	c.Writer = &gzipWriter{c.Writer, gz}
	defer finishEncoding(c, gz.Close)
	c.Next()
}
