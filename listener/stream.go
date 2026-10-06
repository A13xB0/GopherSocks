package listener

import (
	"bufio"
	"io"
	"sync"
	"time"

	"github.com/A13xB0/GopherSocks/framing"
)

// defaultReadBufferSize is the bufio size for stream reads unless
// WithReadBufferSize says otherwise. Every session holds one for as long as
// it's connected: a server with many clients sending small messages can
// choose a smaller one (a larger message is still read whole, in more reads).
const defaultReadBufferSize = 32 * 1024

// maxPooledWriteBuffer: write buffers up to this size go back to the pool
// after use; larger ones (a rare burst) are left to the garbage collector.
const maxPooledWriteBuffer = 1 << 20

// writeBuffers are shared by every session: a session takes one only while
// it writes a batch, so an idle session holds none, and one that once sent a
// burst doesn't keep the burst's buffer for the rest of its life.
var writeBuffers = sync.Pool{New: func() any { b := make([]byte, 0, 4096); return &b }}

// streamTransport frames messages on a byte stream (TCP, QUIC).
type streamTransport struct {
	r                *bufio.Reader
	w                io.Writer
	codec            framing.Codec
	max              int
	readTimeout      time.Duration
	writeTimeout     time.Duration
	setReadDeadline  func(time.Time) error // nil if the stream has its own idle timeout
	setWriteDeadline func(time.Time) error
	closeFn          func(cause error)
	datagram         func([]byte) error   // nil without QUIC datagrams
	rtt              func() time.Duration // QUIC only
}

func (t *streamTransport) readMessage() ([]byte, error) {
	// The deadline is an idle timeout, so it only needs moving when the next
	// frame isn't already buffered (moving it costs a timer update).
	if t.readTimeout > 0 && t.setReadDeadline != nil && t.r.Buffered() == 0 {
		if err := t.setReadDeadline(time.Now().Add(t.readTimeout)); err != nil {
			return nil, err
		}
	}
	return t.codec.Read(t.r, t.max)
}

// writeMessages encodes the batch into one buffer and writes it in one call,
// so frames from one batch can't interleave with anything and small
// messages share a packet.
func (t *streamTransport) writeMessages(msgs [][]byte) error {
	pb := writeBuffers.Get().(*[]byte)
	buf := (*pb)[:0]
	defer func() {
		if cap(buf) <= maxPooledWriteBuffer {
			*pb = buf[:0]
			writeBuffers.Put(pb)
		}
	}()
	for _, m := range msgs {
		var err error
		if buf, err = t.codec.Append(buf, m); err != nil {
			return err
		}
	}
	if t.writeTimeout > 0 {
		if err := t.setWriteDeadline(time.Now().Add(t.writeTimeout)); err != nil {
			return err
		}
	}
	_, err := t.w.Write(buf)
	return err
}

func (t *streamTransport) close(cause error) { t.closeFn(cause) }

func newReader(r io.Reader, size int) *bufio.Reader {
	if size <= 0 {
		size = defaultReadBufferSize
	}
	return bufio.NewReaderSize(r, size)
}
