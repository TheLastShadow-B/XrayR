package limiter

import (
	"context"
	"io"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"golang.org/x/time/rate"
)

type Writer struct {
	writer  buf.Writer
	limiter *rate.Limiter
	w       io.Writer
}

func (l *Limiter) RateWriter(writer buf.Writer, limiter *rate.Limiter) buf.Writer {
	w := &Writer{
		writer:  writer,
		limiter: limiter,
	}
	if iow, ok := writer.(io.Writer); ok {
		w.w = iow
	}
	return w
}

func (w *Writer) Close() error {
	return common.Close(w.writer)
}

func (w *Writer) WriteMultiBuffer(mb buf.MultiBuffer) error {
	ctx := context.Background()
	w.limiter.WaitN(ctx, int(mb.Len()))
	return w.writer.WriteMultiBuffer(mb)
}

func (w *Writer) Write(p []byte) (n int, err error) {
	ctx := context.Background()
	w.limiter.WaitN(ctx, len(p))
	if w.w != nil {
		return w.w.Write(p)
	}
	// Fallback if underlying writer is not io.Writer
	return len(p), nil
}
