package listener

import (
	"bufio"
	"io"
	"time"

	"github.com/A13xB0/GopherSocks/framing"
)

// readBufferSize is the bufio size for stream reads.
const readBufferSize = 32 * 1024

// maxRetainedWriteBuffer: larger write buffers are dropped after use so a
// burst doesn't pin memory.
const maxRetainedWriteBuffer = 1 << 20

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
	buf              []byte
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
	buf := t.buf[:0]
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
	if cap(buf) <= maxRetainedWriteBuffer {
		t.buf = buf
	} else {
		t.buf = nil
	}
	return err
}

func (t *streamTransport) close(cause error) { t.closeFn(cause) }

func newReader(r io.Reader) *bufio.Reader { return bufio.NewReaderSize(r, readBufferSize) }
