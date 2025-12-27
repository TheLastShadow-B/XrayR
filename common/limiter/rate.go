package limiter

import (
	"context"
	"io"

	"github.com/xtls/xray-core/common"
	"github.com/xtls/xray-core/common/buf"
	"golang.org/x/time/rate"
)

type Writer struct {
	ctx     context.Context
	writer  buf.Writer
	limiter *rate.Limiter
	w       io.Writer
}

func (l *Limiter) RateWriter(ctx context.Context, writer buf.Writer, limiter *rate.Limiter) buf.Writer {
	w := &Writer{
		ctx:     ctx,
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
	burst := w.limiter.Burst()
	length := int(mb.Len())

	for length > 0 {
		n := length
		if n > burst {
			n = burst
		}

		if err := w.limiter.WaitN(w.ctx, n); err != nil {
			return err
		}
		length -= n
	}
	return w.writer.WriteMultiBuffer(mb)
}

func (w *Writer) Write(p []byte) (n int, err error) {
	burst := w.limiter.Burst()
	length := len(p)

	for length > 0 {
		chunk := length
		if chunk > burst {
			chunk = burst
		}

		if err := w.limiter.WaitN(w.ctx, chunk); err != nil {
			return 0, err
		}
		length -= chunk
	}

	if w.w != nil {
		return w.w.Write(p)
	}
	// Fallback if underlying writer is not io.Writer
	return len(p), nil
}
