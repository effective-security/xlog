package xlog

import (
	"bufio"
	"io"
	"slices"
	"sync"
	"time"

	"github.com/cockroachdb/errors"
)

const (
	// DefaultSinkQueueBytes bounds the record bytes a sink keeps queued when
	// WithQueueBytes is not supplied.
	DefaultSinkQueueBytes = 8 << 20
	// DefaultSinkBatchBytes is how many record bytes a sink worker coalesces
	// into a single destination write.
	DefaultSinkBatchBytes = 64 << 10

	errSinkClosedSubmit = "unable to submit log record"
	errSinkWrite        = "unable to write queued log record"
	errSinkFlush        = "unable to flush queued log destination"
	errSinkTimeout      = "timed out waiting for log sink delivery"
	errSinkWriterPanic  = "log sink destination panicked: %v"
)

// OverflowPolicy selects what a sink does with a record that does not fit in
// its queue.
type OverflowPolicy int

const (
	// OverflowBlock makes the producer wait for queue space. It is the default
	// and never loses a record.
	OverflowBlock OverflowPolicy = iota
	// OverflowDropNewest discards the submitted record instead of blocking the
	// producer, and counts it in SinkStats.Dropped.
	OverflowDropNewest
)

// SinkStats reports queue occupancy and delivery counters for one sink.
// Counters are cumulative; the queue gauges are instantaneous.
type SinkStats struct {
	// Submitted counts records admitted to the queue.
	Submitted uint64
	// Written counts records handed to a destination buffer.
	Written uint64
	// Dropped counts records discarded by OverflowDropNewest.
	Dropped uint64
	// Oversize counts records rejected for exceeding WithMaxRecordBytes.
	Oversize uint64
	// QueuedRecords is the number of records waiting for delivery.
	QueuedRecords int
	// QueuedBytes is the number of record bytes waiting for delivery.
	QueuedBytes int
	// PeakRecords is the high-water mark of QueuedRecords.
	PeakRecords int
	// PeakBytes is the high-water mark of QueuedBytes.
	PeakBytes int
}

type sinkConfig struct {
	capacity       int
	queueBytes     int
	maxRecordBytes int
	batchBytes     int
	flushInterval  time.Duration
	overflow       OverflowPolicy
}

// SinkOption configures a Sink at construction.
type SinkOption func(*sinkConfig)

// WithQueueBytes bounds the record bytes a sink keeps queued, in addition to
// its record capacity. Zero or negative disables the byte bound and leaves only
// the record count, which does not bound memory. The default is
// DefaultSinkQueueBytes.
func WithQueueBytes(max int) SinkOption {
	return func(c *sinkConfig) { c.queueBytes = max }
}

// WithMaxRecordBytes rejects rendered records larger than max, counting them in
// SinkStats.Oversize and reporting the rejection through the formatter's Err.
// Zero or negative, the default, admits records of any size; a record larger
// than the byte budget is then queued alone.
func WithMaxRecordBytes(max int) SinkOption {
	return func(c *sinkConfig) { c.maxRecordBytes = max }
}

// WithBatchBytes sets how many record bytes the worker coalesces into one
// destination write. The worker also writes whenever its queue drains, so this
// bounds batching rather than delaying records. The default is
// DefaultSinkBatchBytes.
func WithBatchBytes(size int) SinkOption {
	return func(c *sinkConfig) { c.batchBytes = size }
}

// WithFlushInterval flushes bound destinations that support Flush() error at
// the given interval. Zero, the default, flushes only on barriers and shutdown.
func WithFlushInterval(interval time.Duration) SinkOption {
	return func(c *sinkConfig) { c.flushInterval = interval }
}

// WithOverflow selects the full-queue policy. The default is OverflowBlock.
func WithOverflow(policy OverflowPolicy) SinkOption {
	return func(c *sinkConfig) { c.overflow = policy }
}

// sinkTarget binds one formatter destination to a sink. The worker is the only
// goroutine that touches w and writes to dest; the destination stays owned by
// the application and is never closed here.
type sinkTarget struct {
	sink *Sink
	dest io.Writer
	w    *bufio.Writer
}

type sinkItem struct {
	target  *sinkTarget
	record  *RecordBuffer
	barrier chan error
}

// Sink delivers rendered log records to formatter destinations on one FIFO
// worker, so producers pay encoding but not destination latency. Attach it with
// WithSink; one sink can serve several formatters and destinations, and the
// worker coalesces consecutive records for the same destination into one write.
//
// The queue is bounded by record count and by retained bytes. A full queue
// blocks producers unless OverflowDropNewest is selected. Close stops the
// worker; without it the worker leaks. Destinations are flushed when supported
// and never closed. Do not copy a Sink.
type Sink struct {
	cfg   sinkConfig
	queue chan sinkItem

	// admission keeps the queue open while a producer that reserved capacity
	// completes its send.
	admission sync.RWMutex
	// mu guards accounting, closed, and the room condition.
	mu     sync.Mutex
	room   *sync.Cond
	closed bool
	stats  SinkStats

	// barrier serializes flush barriers so one reserved queue slot is enough.
	barrier sync.Mutex

	errMu sync.Mutex
	err   error

	targetsMu sync.Mutex
	targets   []*sinkTarget

	closeOnce sync.Once
	closeErr  error
	done      chan struct{}
}

// NewSink starts a delivery worker holding up to capacity pending records.
// capacity must be positive. Stop application producers, then Close the sink,
// to drain and flush; a sink that is never closed leaks its worker.
func NewSink(capacity int, opts ...SinkOption) (*Sink, error) {
	if capacity <= 0 {
		return nil, errors.New("log sink capacity must be positive")
	}
	cfg := sinkConfig{
		capacity:   capacity,
		queueBytes: DefaultSinkQueueBytes,
		batchBytes: DefaultSinkBatchBytes,
	}
	for _, opt := range opts {
		opt(&cfg)
	}
	if cfg.batchBytes <= 0 {
		cfg.batchBytes = DefaultSinkBatchBytes
	}
	if cfg.maxRecordBytes > 0 && cfg.queueBytes > 0 && cfg.maxRecordBytes > cfg.queueBytes {
		return nil, errors.Newf("log sink record limit %d exceeds its queue budget %d",
			cfg.maxRecordBytes, cfg.queueBytes)
	}
	s := &Sink{
		cfg: cfg,
		// One slot beyond capacity keeps a barrier from waiting behind a full
		// queue, so every admitted record sends without blocking.
		queue: make(chan sinkItem, capacity+1),
		done:  make(chan struct{}),
	}
	s.room = sync.NewCond(&s.mu)
	go s.run()
	return s, nil
}

// bind registers a formatter destination. Binding is a configuration step and
// must not race with logging.
func (s *Sink) bind(dest io.Writer) *sinkTarget {
	target := &sinkTarget{
		sink: s,
		dest: dest,
		w:    newTargetBuffer(dest, s.cfg.batchBytes),
	}
	s.targetsMu.Lock()
	s.targets = append(s.targets, target)
	s.targetsMu.Unlock()
	return target
}

func newTargetBuffer(dest io.Writer, size int) *bufio.Writer {
	if buffered, ok := dest.(*bufio.Writer); ok {
		return bufio.NewWriterSize(buffered, size)
	}
	// MultiWriter turns short writes without errors into io.ErrShortWrite,
	// so bufio cannot retry a zero-progress writer forever.
	return bufio.NewWriterSize(io.MultiWriter(dest), size)
}

// submit takes ownership of a rendered record and queues it for delivery. It
// returns an admission error only; delivery failures surface through Err.
func (s *Sink) submit(target *sinkTarget, record *RecordBuffer) error {
	size := record.Len()
	if s.cfg.maxRecordBytes > 0 && size > s.cfg.maxRecordBytes {
		s.mu.Lock()
		s.stats.Oversize++
		s.mu.Unlock()
		record.release()
		return errors.Newf("unable to queue log record of %d bytes, the limit is %d",
			size, s.cfg.maxRecordBytes)
	}
	s.admission.RLock()
	defer s.admission.RUnlock()
	s.mu.Lock()
	// hasRoom reports room once closed, so the wait ends and the check below
	// reports the rejection.
	for !s.hasRoom(size) {
		if s.cfg.overflow == OverflowDropNewest {
			s.stats.Dropped++
			s.mu.Unlock()
			record.release()
			return nil
		}
		s.room.Wait()
	}
	if s.closed {
		s.mu.Unlock()
		record.release()
		return errors.WithMessage(io.ErrClosedPipe, errSinkClosedSubmit)
	}
	s.stats.Submitted++
	s.stats.QueuedRecords++
	s.stats.QueuedBytes += size
	s.stats.PeakRecords = max(s.stats.PeakRecords, s.stats.QueuedRecords)
	s.stats.PeakBytes = max(s.stats.PeakBytes, s.stats.QueuedBytes)
	s.mu.Unlock()
	s.queue <- sinkItem{target: target, record: record}
	return nil
}

// hasRoom requires mu. A record larger than the whole byte budget is admitted
// alone, so a sink without a record limit cannot deadlock on one large record.
func (s *Sink) hasRoom(size int) bool {
	if s.closed {
		return true // let the caller observe closure instead of waiting
	}
	if s.stats.QueuedRecords >= s.cfg.capacity {
		return false
	}
	if s.cfg.queueBytes <= 0 || s.stats.QueuedBytes+size <= s.cfg.queueBytes {
		return true
	}
	return s.stats.QueuedRecords == 0
}

// delivered releases the accounting for one written record and wakes producers.
func (s *Sink) delivered(size int) {
	s.mu.Lock()
	s.stats.QueuedRecords--
	s.stats.QueuedBytes -= size
	s.stats.Written++
	s.mu.Unlock()
	s.room.Broadcast()
}

// Flush waits for every record admitted before the call to reach its
// destination, and flushes destinations that support Flush() error. It is a
// delivery barrier, not an fsync, and a stalled destination blocks it.
func (s *Sink) Flush() error { return s.FlushWithin(0) }

// FlushWithin is Flush bounded by limit, reporting a timeout instead of waiting
// for a stalled destination. A non-positive limit waits indefinitely. A barrier
// that times out is still completed by the worker.
func (s *Sink) FlushWithin(limit time.Duration) error {
	s.barrier.Lock()
	defer s.barrier.Unlock()
	var expired <-chan time.Time
	if limit > 0 {
		timer := time.NewTimer(limit)
		defer timer.Stop()
		expired = timer.C
	}
	s.admission.RLock()
	if s.isClosed() {
		s.admission.RUnlock()
		// Shutdown drains and flushes; report the shutdown result instead.
		if !waitFor(s.done, limit) {
			return errors.New(errSinkTimeout)
		}
		return s.Err()
	}
	done := make(chan error, 1)
	if limit <= 0 {
		s.queue <- sinkItem{barrier: done}
		s.admission.RUnlock()
		return <-done
	}
	// A barrier abandoned on timeout still holds its queue slot until the
	// worker reaches it, so bound the send as well as the wait.
	select {
	case s.queue <- sinkItem{barrier: done}:
		s.admission.RUnlock()
	case <-expired:
		s.admission.RUnlock()
		return errors.New(errSinkTimeout)
	}
	select {
	case err := <-done:
		return err
	case <-expired:
		return errors.New(errSinkTimeout)
	}
}

// Close rejects new records, wakes blocked producers, drains what was admitted,
// flushes destinations, and stops the worker. Concurrent and repeated calls
// wait for the same completion and return the same result. Remove installed
// formatters and stop producers first. Destinations are never closed here.
func (s *Sink) Close() error { return s.CloseWithin(0) }

// CloseWithin is Close bounded by limit. On timeout it returns an error and
// leaves the worker draining, so queued records may still be delivered. A
// non-positive limit waits indefinitely.
func (s *Sink) CloseWithin(limit time.Duration) error {
	s.closeOnce.Do(func() {
		s.mu.Lock()
		s.closed = true
		s.mu.Unlock()
		// Release producers blocked on a full queue before taking the queue
		// away from producers that already reserved capacity.
		s.room.Broadcast()
		s.admission.Lock()
		close(s.queue)
		s.admission.Unlock()
	})
	if !waitFor(s.done, limit) {
		return errors.New(errSinkTimeout)
	}
	return s.closeErr
}

// IsClosed reports whether shutdown has begun, not whether draining finished.
func (s *Sink) IsClosed() bool { return s.isClosed() }

func (s *Sink) isClosed() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.closed
}

// Stats returns a snapshot of queue occupancy and delivery counters.
func (s *Sink) Stats() SinkStats {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.stats
}

// Err returns the first write or flush failure observed by the worker. Records
// admitted successfully can still fail later, so a nil Err before Flush or
// Close does not mean everything was delivered.
func (s *Sink) Err() error {
	s.errMu.Lock()
	defer s.errMu.Unlock()
	return s.err
}

func (s *Sink) recordError(err error) {
	if err == nil {
		return
	}
	s.errMu.Lock()
	defer s.errMu.Unlock()
	if s.err == nil {
		s.err = err
	}
}

// waitFor reports whether done closed within limit; a non-positive limit waits.
func waitFor(done <-chan struct{}, limit time.Duration) bool {
	if limit <= 0 {
		<-done
		return true
	}
	timer := time.NewTimer(limit)
	defer timer.Stop()
	select {
	case <-done:
		return true
	case <-timer.C:
		return false
	}
}

// run owns every destination buffer. It batches consecutive records for one
// destination and writes whenever the batch is full or the queue drains, so
// batching never delays a record behind an idle queue.
func (s *Sink) run() {
	defer close(s.done)
	var ticks <-chan time.Time
	if s.cfg.flushInterval > 0 {
		ticker := time.NewTicker(s.cfg.flushInterval)
		defer ticker.Stop()
		ticks = ticker.C
	}
	var current *sinkTarget
	batched := 0
	for {
		select {
		case <-ticks:
			s.writeBatch(current)
			s.flushDestinations()
			batched = 0
		case item, ok := <-s.queue:
			if !ok {
				s.writeBatch(current)
				s.flushDestinations()
				s.closeErr = s.Err()
				return
			}
			if item.barrier != nil {
				s.writeBatch(current)
				s.flushDestinations()
				batched = 0
				item.barrier <- s.Err()
				continue
			}
			if item.target != current {
				s.writeBatch(current)
				current, batched = item.target, 0
			}
			batched += s.write(item)
			if batched >= s.cfg.batchBytes || len(s.queue) == 0 {
				s.writeBatch(current)
				batched = 0
			}
		}
	}
}

// write copies one record into its destination buffer and releases it. A
// panicking destination is recorded and the worker keeps draining.
func (s *Sink) write(item sinkItem) (size int) {
	size = item.record.Len()
	defer func() {
		item.record.release()
		s.delivered(size)
		if recovered := recover(); recovered != nil {
			s.recordError(errors.Newf(errSinkWriterPanic, recovered))
		}
	}()
	_, err := item.target.w.Write(item.record.Bytes())
	s.recordError(errors.WithMessage(err, errSinkWrite))
	return size
}

// writeBatch pushes the buffered batch to its destination. Only the current
// target can hold buffered bytes, because changing targets writes the previous
// batch first.
func (s *Sink) writeBatch(target *sinkTarget) {
	if target == nil {
		return
	}
	defer func() {
		if recovered := recover(); recovered != nil {
			s.recordError(errors.Newf(errSinkWriterPanic, recovered))
		}
	}()
	s.recordError(errors.WithMessage(target.w.Flush(), errSinkWrite))
}

// flushDestinations flushes bound destinations that support Flush() error,
// for barriers, the periodic interval, and shutdown.
func (s *Sink) flushDestinations() {
	s.targetsMu.Lock()
	targets := slices.Clone(s.targets)
	s.targetsMu.Unlock()
	for _, target := range targets {
		flusher, ok := target.dest.(interface{ Flush() error })
		if !ok {
			continue
		}
		s.recordError(errors.WithMessage(flusher.Flush(), errSinkFlush))
	}
}
