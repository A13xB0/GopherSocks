package listener

import (
	"context"
	"errors"
	"io"
	"iter"
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// transport is what a session needs from a connection.
type transport interface {
	// readMessage blocks until the next message from the client arrives.
	readMessage() ([]byte, error)
	// writeMessages writes a batch of messages to the client.
	writeMessages(msgs [][]byte) error
	// close closes the connection; it must unblock readMessage and writeMessages.
	close(cause error)
}

// maxBatch caps how many queued messages one write carries.
const maxBatch = 128

// session implements Session on top of a transport.
//
// Exactly one goroutine writes (writeLoop) and reads happen only when the
// consumer pulls (Messages), so a busy client can't delay others and a slow
// consumer slows its client instead of buffering without bound. Nothing
// closes a channel that another goroutine sends on.
type session struct {
	id     string
	addr   net.Addr
	t      transport
	ctx    context.Context
	cancel context.CancelCauseFunc
	out    chan []byte
	last   atomic.Int64
	maxLen int
	// sendWait is how long SendToClient waits for queue space (0 = until closed).
	sendWait time.Duration
	bufSize  int
	onClose  func(*session)

	closeOnce sync.Once
	dataOnce  sync.Once
	data      chan []byte
}

func newSession(parent context.Context, addr net.Addr, t transport, cfg *ServerConfig, onClose func(*session)) *session {
	ctx, cancel := context.WithCancelCause(parent)
	queue := cfg.SendQueueSize
	if queue < 1 {
		queue = defaultSendQueueSize
	}
	s := &session{
		id:       uuid.NewString(),
		addr:     addr,
		t:        t,
		ctx:      ctx,
		cancel:   cancel,
		out:      make(chan []byte, queue),
		maxLen:   int(cfg.MaxLength),
		sendWait: cfg.WriteTimeout,
		bufSize:  max(cfg.BufferSize, 1),
		onClose:  onClose,
	}
	s.touch()
	// If the parent ends (listener stopped), close the connection too so a
	// blocked read or write returns.
	context.AfterFunc(ctx, func() { s.closeWith(context.Cause(ctx)) })
	go s.writeLoop()
	return s
}

func (s *session) touch() { s.last.Store(time.Now().UnixNano()) }

// GetSessionID returns the session's unique ID.
func (s *session) GetSessionID() string { return s.id }

// GetClientAddr returns the client's address.
func (s *session) GetClientAddr() net.Addr { return s.addr }

// GetLastRecieved returns when the last message arrived.
func (s *session) GetLastRecieved() time.Time { return time.Unix(0, s.last.Load()) }

// Context ends when the session closes; context.Cause tells why.
func (s *session) Context() context.Context { return s.ctx }

// CloseSession closes the session.
func (s *session) CloseSession() { s.closeWith(ErrSessionClosed) }

// SendDatagram sends data as one unreliable QUIC datagram. It never waits:
// quic-go queues it, and drops it if the queue is full.
func (s *session) SendDatagram(data []byte) error {
	st, ok := s.t.(*streamTransport)
	if !ok || st.datagram == nil {
		return ErrDatagramsUnsupported
	}
	if s.ctx.Err() != nil {
		return NewSessionError("session closed", context.Cause(s.ctx))
	}
	return st.datagram(data)
}

// DatagramsEnabled reports whether both ends enabled datagrams.
func (s *session) DatagramsEnabled() bool {
	st, ok := s.t.(*streamTransport)
	return ok && st.datagram != nil
}

// RTT is the connection's smoothed round-trip time; 0 when the transport
// doesn't measure one.
func (s *session) RTT() time.Duration {
	if st, ok := s.t.(*streamTransport); ok && st.rtt != nil {
		return st.rtt()
	}
	return 0
}

// Messages yields client messages, reading the next one only on demand.
func (s *session) Messages() iter.Seq[[]byte] {
	return func(yield func([]byte) bool) {
		for {
			m, err := s.t.readMessage()
			if err != nil {
				s.closeWith(readCause(err))
				return
			}
			s.touch()
			if !yield(m) {
				return
			}
		}
	}
}

// Data returns a channel of client messages, closed when the session ends.
// The channel's only sender is the goroutine started here, which also closes it.
func (s *session) Data() chan []byte {
	s.dataOnce.Do(func() {
		s.data = make(chan []byte, s.bufSize)
		go s.pumpData()
	})
	return s.data
}

func (s *session) pumpData() {
	defer close(s.data)
	for m := range s.Messages() {
		select {
		case s.data <- m:
		case <-s.ctx.Done():
			return
		}
	}
}

// SendToClient queues data for the writer goroutine. It returns at once
// while the queue has room; when it's full it waits for space for up to
// WriteTimeout, then closes the session as a slow consumer.
func (s *session) SendToClient(data []byte) error {
	if len(data) > s.maxLen {
		return NewProtocolError("message exceeds maximum length", ErrMessageTooLarge)
	}
	select {
	case <-s.ctx.Done():
		return NewSessionError("session closed", context.Cause(s.ctx))
	default:
	}
	select {
	case s.out <- data:
		return nil
	default:
	}
	return s.waitToSend(data)
}

// waitToSend is SendToClient's slow path: the client is behind.
func (s *session) waitToSend(data []byte) error {
	var timeout <-chan time.Time
	if s.sendWait > 0 {
		t := time.NewTimer(s.sendWait)
		defer t.Stop()
		timeout = t.C
	}
	select {
	case s.out <- data:
		return nil
	case <-s.ctx.Done():
		return NewSessionError("session closed", context.Cause(s.ctx))
	case <-timeout:
		s.closeWith(ErrSlowConsumer)
		return ErrSlowConsumer
	}
}

// writeLoop is the session's only writer. It sends queued messages in
// batches: one transport write for everything waiting.
func (s *session) writeLoop() {
	batch := make([][]byte, 0, maxBatch)
	done := s.ctx.Done()
	for {
		select {
		case <-done:
			return
		case m := <-s.out:
			batch = s.fillBatch(append(batch[:0], m))
			if err := s.t.writeMessages(batch); err != nil {
				s.closeWith(err)
				return
			}
			clear(batch)
		}
	}
}

// fillBatch adds whatever else is already queued, without waiting.
func (s *session) fillBatch(batch [][]byte) [][]byte {
	for len(batch) < maxBatch {
		select {
		case m := <-s.out:
			batch = append(batch, m)
		default:
			return batch
		}
	}
	return batch
}

// closeWith closes the session once, recording why. The context is
// cancelled last, so anyone woken by Context().Done() sees the session
// already unregistered and its connection closed.
func (s *session) closeWith(cause error) {
	s.closeOnce.Do(func() {
		if cause == nil {
			cause = ErrSessionClosed
		}
		if s.onClose != nil {
			s.onClose(s)
		}
		s.t.close(cause)
		s.cancel(cause)
	})
}

// readCause turns a read error into a close cause.
func readCause(err error) error {
	if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
		return ErrSessionClosed
	}
	return err
}
