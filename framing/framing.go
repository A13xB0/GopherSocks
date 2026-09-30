// Package framing turns a byte stream into messages and back.
//
// Three codecs are provided:
//   - Legacy: payload | uint16 big-endian length | delimiter ("\n\n\n" by
//     default). This is the original GopherSocks QUIC format, still spoken by
//     existing clients.
//   - Uvarint: uvarint length | payload. Use it for new protocol versions.
//   - Uint32: uint32 big-endian length | payload. The GopherSocks TCP format.
//
// Decoders read from a bufio.Reader and return a freshly allocated payload
// the caller owns. Encoders append to a caller-supplied buffer so writers can
// batch frames without allocating.
package framing

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
)

var (
	// ErrFrameTooLarge is returned when a frame's length exceeds the limit.
	ErrFrameTooLarge = errors.New("framing: frame exceeds maximum length")
	// ErrCorruptFrame is returned when a Legacy stream can't be parsed. The
	// connection should be closed: there is no way to resynchronise.
	ErrCorruptFrame = errors.New("framing: corrupt frame")
	// ErrEmptyDelimiter is returned by NewLegacy for an empty delimiter.
	ErrEmptyDelimiter = errors.New("framing: empty delimiter")
)

// Codec encodes and decodes message frames.
type Codec interface {
	// Append appends payload as one frame to dst and returns the result.
	Append(dst, payload []byte) ([]byte, error)
	// Read reads the next frame's payload. max limits the payload length.
	Read(r *bufio.Reader, max int) ([]byte, error)
	// MaxPayload is the largest payload the format can express.
	MaxPayload() int
}

// DefaultDelimiter is the Legacy codec's frame terminator.
var DefaultDelimiter = []byte("\n\n\n")

// Legacy is the original QUIC framing with the default delimiter.
var Legacy Codec = legacy{delim: DefaultDelimiter}

// Uvarint is length-prefixed framing with a uvarint length.
var Uvarint Codec = uvarintCodec{}

// Uint32 is length-prefixed framing with a big-endian uint32 length.
var Uint32 Codec = uint32Codec{}

// NewLegacy returns the Legacy codec with a custom delimiter.
func NewLegacy(delimiter []byte) (Codec, error) {
	if len(delimiter) == 0 {
		return nil, ErrEmptyDelimiter
	}
	return legacy{delim: bytes.Clone(delimiter)}, nil
}

type legacy struct {
	delim []byte
}

func (c legacy) MaxPayload() int { return math.MaxUint16 }

func (c legacy) Append(dst, payload []byte) ([]byte, error) {
	if len(payload) > math.MaxUint16 {
		return dst, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, len(payload), math.MaxUint16)
	}
	dst = append(dst, payload...)
	dst = binary.BigEndian.AppendUint16(dst, uint16(len(payload)))
	return append(dst, c.delim...), nil
}

// Read decodes one Legacy frame.
//
// A frame starts exactly where the previous one ended, so the frame end is
// the first delimiter whose preceding two bytes encode the number of bytes
// before them. A delimiter that merely appears inside a payload (0x0A 0x0A
// 0x0A is common in protobuf) fails that check and is skipped, where the old
// decoder stalled forever. Empty frames are skipped, as before.
func (c legacy) Read(r *bufio.Reader, max int) ([]byte, error) {
	limit := min(max, math.MaxUint16)
	for {
		payload, err := c.readOne(r, limit)
		if err != nil || len(payload) > 0 {
			return payload, err
		}
	}
}

func (c legacy) readOne(r *bufio.Reader, limit int) ([]byte, error) {
	frameMax := limit + 2 + len(c.delim)
	var buf []byte
	for {
		// ReadSlice stops at the delimiter's last byte, so only the end of
		// buf ever needs checking: parsing is linear in the frame size.
		chunk, err := r.ReadSlice(c.delim[len(c.delim)-1])
		buf = append(buf, chunk...)
		if n, ok := c.frameEnd(buf); ok {
			return buf[:n], nil
		}
		if len(buf) > frameMax {
			return nil, fmt.Errorf("%w: no valid frame within %d bytes", ErrCorruptFrame, frameMax)
		}
		if err == nil || errors.Is(err, bufio.ErrBufferFull) {
			continue
		}
		if errors.Is(err, io.EOF) && len(buf) > 0 {
			return nil, io.ErrUnexpectedEOF
		}
		return nil, err
	}
}

// frameEnd reports whether buf ends with a delimiter whose preceding uint16
// equals the number of bytes before it, and returns that payload length.
func (c legacy) frameEnd(buf []byte) (int, bool) {
	d := len(buf) - len(c.delim)
	if d < 2 || !bytes.Equal(buf[d:], c.delim) {
		return 0, false
	}
	n := int(binary.BigEndian.Uint16(buf[d-2 : d]))
	return n, n == d-2
}

type uvarintCodec struct{}

func (uvarintCodec) MaxPayload() int { return math.MaxInt32 }

func (uvarintCodec) Append(dst, payload []byte) ([]byte, error) {
	dst = binary.AppendUvarint(dst, uint64(len(payload)))
	return append(dst, payload...), nil
}

func (uvarintCodec) Read(r *bufio.Reader, max int) ([]byte, error) {
	n, err := binary.ReadUvarint(r)
	if err != nil {
		return nil, unexpectedEOF(err, false)
	}
	return readPayload(r, n, max)
}

type uint32Codec struct{}

func (uint32Codec) MaxPayload() int { return math.MaxInt32 }

func (uint32Codec) Append(dst, payload []byte) ([]byte, error) {
	if uint64(len(payload)) > math.MaxUint32 {
		return dst, ErrFrameTooLarge
	}
	dst = binary.BigEndian.AppendUint32(dst, uint32(len(payload)))
	return append(dst, payload...), nil
}

func (uint32Codec) Read(r *bufio.Reader, max int) ([]byte, error) {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return nil, unexpectedEOF(err, false)
	}
	return readPayload(r, uint64(binary.BigEndian.Uint32(hdr[:])), max)
}

func readPayload(r *bufio.Reader, n uint64, max int) ([]byte, error) {
	if n > uint64(max) {
		return nil, fmt.Errorf("%w: %d > %d", ErrFrameTooLarge, n, max)
	}
	payload := make([]byte, n)
	if _, err := io.ReadFull(r, payload); err != nil {
		return nil, unexpectedEOF(err, true)
	}
	return payload, nil
}

// unexpectedEOF turns EOF mid-frame into io.ErrUnexpectedEOF. A clean EOF
// before a frame starts stays io.EOF.
func unexpectedEOF(err error, midFrame bool) error {
	if midFrame && errors.Is(err, io.EOF) {
		return io.ErrUnexpectedEOF
	}
	return err
}
