package xlog

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	errRecordWrite       = "unable to write log record"
	errDestinationFlush  = "unable to flush log destination"
	errRecordBufferFlush = "unable to flush log record buffer"
)

// RecordBuffer holds one fully rendered log record, including its trailing
// newline. Formatters acquire a buffer with Output.Buffer, render the complete
// record into it, and pass ownership to Output.Emit, which writes it inline or
// hands it to a Sink worker. Buffers are pooled and reused: do not retain a
// buffer, its Bytes, or its Encoder after Emit returns.
type RecordBuffer struct {
	buf     bytes.Buffer
	encoder *json.Encoder
}

var recordBufferPool = sync.Pool{
	New: func() any {
		r := &RecordBuffer{}
		r.encoder = json.NewEncoder(&r.buf)
		r.encoder.SetEscapeHTML(false)
		return r
	},
}

func acquireRecordBuffer() *RecordBuffer {
	r := recordBufferPool.Get().(*RecordBuffer)
	r.buf.Reset()
	return r
}

// release returns the buffer to the pool without retaining oversized capacity.
func (r *RecordBuffer) release() {
	if r.buf.Cap() > maxPooledBufferSize {
		return
	}
	recordBufferPool.Put(r)
}

// Buffer returns the record's byte buffer for direct rendering. Writes to it
// never fail, so text formatters can ignore their write results.
func (r *RecordBuffer) Buffer() *bytes.Buffer { return &r.buf }

// Encoder returns a pooled JSON encoder appending to this record. HTML escaping
// is disabled and Encode appends the record's newline terminator.
func (r *RecordBuffer) Encoder() *json.Encoder { return r.encoder }

// Bytes returns the rendered record. It is valid until the record is emitted.
func (r *RecordBuffer) Bytes() []byte { return r.buf.Bytes() }

// Len returns the rendered size of the record in bytes.
func (r *RecordBuffer) Len() int { return r.buf.Len() }

// Output owns the destination side of a formatter: it hands out record buffers,
// delivers rendered records, retains the first delivery error, and implements
// the Err/Flush/FlushError half of ErrorFormatter. Formatters embed it, so
// third-party formatters gain Sink support by rendering into a RecordBuffer
// instead of writing to a destination themselves.
//
// Without a Sink, records are written inline and the caller must serialize
// Emit, Flush, and FlushError, exactly as before. With a Sink, the sink worker
// owns the destination and its buffer, and Emit is safe for concurrent use.
// Do not copy an Output after first use.
type Output struct {
	errMu sync.Mutex
	err   error

	// dest is the caller-owned destination. It is never closed here.
	dest io.Writer
	// w buffers inline writes. It is nil when a sink owns the destination.
	w *bufio.Writer
	// target is this formatter's binding in a sink, or nil for inline delivery.
	target *sinkTarget
}

// Bind selects the destination and delivery mode. A nil sink writes records
// inline through an owned buffer; otherwise the sink worker owns the
// destination. Constructors and Options call Bind; it must not run concurrently
// with formatting, and it neither flushes nor closes a previous destination.
func (o *Output) Bind(dest io.Writer, sink *Sink) {
	o.dest = dest
	if sink == nil {
		o.target = nil
		o.w = newFormatterBuffer(dest)
		return
	}
	o.w = nil
	o.target = sink.bind(dest)
}

// Rebind reapplies the bound destination when options change the sink. Records
// already admitted to a previous sink are still delivered by that sink, which
// can write them after the new binding starts. Flush the formatter before
// changing its sink so the two never interleave at one destination.
func (o *Output) Rebind(sink *Sink) {
	if o.dest == nil {
		return
	}
	var current *Sink
	if o.target != nil {
		current = o.target.sink
	}
	if current == sink {
		return
	}
	o.Bind(o.dest, sink)
}

// Dest returns the bound destination, which remains owned by the caller.
func (o *Output) Dest() io.Writer { return o.dest }

// Buffer returns a pooled buffer to render one record into.
func (o *Output) Buffer() *RecordBuffer { return acquireRecordBuffer() }

// Emit takes ownership of a rendered record and delivers it. Inline delivery
// writes and flushes the record buffer; sink delivery hands the buffer to the
// worker, which releases it after the write. Delivery failures are retained and
// reported by Err, not returned: enqueueing is not delivery.
func (o *Output) Emit(record *RecordBuffer) {
	if o.target != nil {
		o.RecordError(o.target.sink.submit(o.target, record))
		return
	}
	_, err := o.w.Write(record.Bytes())
	record.release()
	o.RecordError(errors.WithMessage(err, errRecordWrite))
	o.flushBuffer()
}

// Discard releases a record buffer without delivering it. A formatter that
// cannot render a complete record discards it instead of emitting a partial one.
func (*Output) Discard(record *RecordBuffer) {
	record.release()
}

// RecordError retains the first error observed by this formatter. Later
// successful records never clear it. Nil is ignored.
func (o *Output) RecordError(err error) {
	if err == nil {
		return
	}
	o.errMu.Lock()
	defer o.errMu.Unlock()
	if o.err == nil {
		o.err = err
	}
}

// Err returns the first formatter error, or the bound sink's first delivery
// error. It is safe to call concurrently with formatting.
func (o *Output) Err() error {
	o.errMu.Lock()
	err := o.err
	o.errMu.Unlock()
	if err != nil {
		return err
	}
	if o.target != nil {
		return o.target.sink.Err()
	}
	return nil
}

// Flush waits for delivery of previously emitted records. Use FlushError to
// observe failures.
func (o *Output) Flush() { _ = o.FlushError() }

// FlushError is an ordered delivery barrier: it flushes this formatter's buffer
// and supported downstream buffers, or waits for the sink worker to drain and
// flush every record admitted before the call. It is not an fsync, and a
// stalled destination can block it indefinitely. See FlushWithin for a bound.
func (o *Output) FlushError() error {
	if o.target != nil {
		o.RecordError(o.target.sink.Flush())
		return o.Err()
	}
	o.flushBuffer()
	o.flushDestination()
	return o.Err()
}

// FlushWithin bounds a sink barrier, reporting a timeout instead of waiting for
// a stalled destination. A non-positive limit waits indefinitely. Inline
// delivery has no queue to drain, so it behaves like FlushError.
func (o *Output) FlushWithin(limit time.Duration) error {
	if o.target != nil {
		o.RecordError(o.target.sink.FlushWithin(limit))
		return o.Err()
	}
	return o.FlushError()
}

// Concurrent reports whether rendering and emitting records is safe without
// external serialization. It is true exactly when a sink owns the destination.
func (o *Output) Concurrent() bool { return o.target != nil }

func (o *Output) flushBuffer() {
	if o.w == nil {
		return
	}
	o.RecordError(errors.WithMessage(o.w.Flush(), errRecordBufferFlush))
}

func (o *Output) flushDestination() {
	if flusher, ok := o.dest.(interface{ Flush() error }); ok {
		o.RecordError(errors.WithMessage(flusher.Flush(), errDestinationFlush))
	}
}
