package framing

import (
	"bufio"
	"bytes"
	"errors"
	"io"
	"math"
	"strings"
	"testing"
	"testing/iotest"
)

var codecs = map[string]Codec{"legacy": Legacy, "uvarint": Uvarint, "uint32": Uint32}

func encodeAll(t testing.TB, c Codec, payloads ...[]byte) []byte {
	t.Helper()
	var out []byte
	for _, p := range payloads {
		var err error
		if out, err = c.Append(out, p); err != nil {
			t.Fatal(err)
		}
	}
	return out
}

func decodeAll(t testing.TB, c Codec, r io.Reader, max int) ([][]byte, error) {
	t.Helper()
	br := bufio.NewReaderSize(r, 16)
	var out [][]byte
	for {
		p, err := c.Read(br, max)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return out, nil
			}
			return out, err
		}
		out = append(out, p)
	}
}

func TestRoundTrip(t *testing.T) {
	payloads := [][]byte{
		[]byte("a"),
		[]byte("hello world"),
		bytes.Repeat([]byte{0x0A}, 40), // all delimiter bytes
		bytes.Repeat([]byte("x"), 5000),
		{0x08, 0x01, 0x0A, 0x0A, 0x0A, 0x12, 0x03, 'a', 'b', 'c'},
	}
	for name, c := range codecs {
		t.Run(name, func(t *testing.T) {
			stream := encodeAll(t, c, payloads...)
			// OneByteReader makes every frame arrive in pieces.
			got, err := decodeAll(t, c, iotest.OneByteReader(bytes.NewReader(stream)), 1<<20)
			if err != nil {
				t.Fatal(err)
			}
			if len(got) != len(payloads) {
				t.Fatalf("decoded %d of %d frames", len(got), len(payloads))
			}
			for i := range payloads {
				if !bytes.Equal(got[i], payloads[i]) {
					t.Fatalf("frame %d: got %x want %x", i, got[i], payloads[i])
				}
			}
		})
	}
}

// Regression for review finding GS-1: a payload containing 0A 0A 0A
// (protobuf field 1, length 10, field 1) made the old decoder wait forever,
// losing that message and every message after it.
func TestLegacyPayloadContainingDelimiterDoesNotWedge(t *testing.T) {
	wedge := []byte{0x08, 0x01, 0x0A, 0x0A, 0x0A, 0x12, 0x03, 'a', 'b', 'c'}
	stream := encodeAll(t, Legacy, wedge, []byte("second"))
	got, err := decodeAll(t, Legacy, bytes.NewReader(stream), math.MaxUint16)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || !bytes.Equal(got[0], wedge) || string(got[1]) != "second" {
		t.Fatalf("got %q", got)
	}
}

// Old clients framed a payload whose length bytes are themselves 0x0A.
func TestLegacyLengthBytesThatLookLikeDelimiter(t *testing.T) {
	p := bytes.Repeat([]byte{0x0A}, 0x0A0A) // length 2570 = 0x0A0A
	stream := encodeAll(t, Legacy, p, []byte("next"))
	got, err := decodeAll(t, Legacy, bytes.NewReader(stream), math.MaxUint16)
	if err != nil || len(got) != 2 || !bytes.Equal(got[0], p) || string(got[1]) != "next" {
		t.Fatalf("got %d frames, err %v", len(got), err)
	}
}

func TestLegacySkipsEmptyFrames(t *testing.T) {
	stream := encodeAll(t, Legacy, nil, []byte("x"))
	got, err := decodeAll(t, Legacy, bytes.NewReader(stream), 100)
	if err != nil || len(got) != 1 || string(got[0]) != "x" {
		t.Fatalf("got %q, err %v", got, err)
	}
}

// The ambiguous case the fuzzer found: payload b holds a complete frame of
// its own. The decoder splits it and then fails with an error; it never stalls.
func TestLegacyAmbiguousPayloadErrorsInsteadOfStalling(t *testing.T) {
	stream := encodeAll(t, Legacy, []byte("0"), []byte("0000000000\x00\n\n\n\n"))
	_, err := decodeAll(t, Legacy, bytes.NewReader(stream), math.MaxUint16)
	if err == nil {
		t.Fatal("expected an error for the ambiguous stream")
	}
}

// Garbage must end in an error (so the connection closes), never a hang.
func TestLegacyCorruptStreamErrors(t *testing.T) {
	garbage := append(bytes.Repeat([]byte("zz\n\n\n"), 50), 'q')
	_, err := decodeAll(t, Legacy, bytes.NewReader(garbage), 64)
	if !errors.Is(err, ErrCorruptFrame) {
		t.Fatalf("want ErrCorruptFrame, got %v", err)
	}
}

func TestTruncatedFrameIsUnexpectedEOF(t *testing.T) {
	for name, c := range codecs {
		t.Run(name, func(t *testing.T) {
			stream := encodeAll(t, c, []byte("hello"))
			_, err := decodeAll(t, c, bytes.NewReader(stream[:len(stream)-1]), 100)
			if !errors.Is(err, io.ErrUnexpectedEOF) {
				t.Fatalf("want io.ErrUnexpectedEOF, got %v", err)
			}
		})
	}
}

func TestMaxLengthEnforced(t *testing.T) {
	for name, c := range codecs {
		t.Run(name, func(t *testing.T) {
			stream := encodeAll(t, c, bytes.Repeat([]byte("x"), 200))
			_, err := decodeAll(t, c, bytes.NewReader(stream), 100)
			if err == nil {
				t.Fatal("a frame over max should fail")
			}
		})
	}
}

func TestLegacyLargestFrame(t *testing.T) {
	p := bytes.Repeat([]byte("y"), math.MaxUint16)
	got, err := decodeAll(t, Legacy, bytes.NewReader(encodeAll(t, Legacy, p)), math.MaxUint16)
	if err != nil || len(got) != 1 || len(got[0]) != math.MaxUint16 {
		t.Fatalf("err %v", err)
	}
	if _, err := Legacy.Append(nil, make([]byte, math.MaxUint16+1)); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("want ErrFrameTooLarge, got %v", err)
	}
}

func TestCustomDelimiter(t *testing.T) {
	c, err := NewLegacy([]byte("||"))
	if err != nil {
		t.Fatal(err)
	}
	stream := encodeAll(t, c, []byte("a||b"), []byte("c"))
	got, err := decodeAll(t, c, bytes.NewReader(stream), 100)
	if err != nil || len(got) != 2 || string(got[0]) != "a||b" || string(got[1]) != "c" {
		t.Fatalf("got %q, err %v", got, err)
	}
	if _, err := NewLegacy(nil); !errors.Is(err, ErrEmptyDelimiter) {
		t.Fatalf("want ErrEmptyDelimiter, got %v", err)
	}
}

func TestMaxPayload(t *testing.T) {
	if Legacy.MaxPayload() != math.MaxUint16 || Uvarint.MaxPayload() != math.MaxInt32 || Uint32.MaxPayload() != math.MaxInt32 {
		t.Fatal("unexpected MaxPayload")
	}
}

// FuzzLegacyRoundTrip: any sequence of payloads survives encode/decode in
// order. Delimiters inside payloads are the interesting case.
func FuzzLegacyRoundTrip(f *testing.F) {
	f.Add([]byte{0x08, 0x01, 0x0A, 0x0A, 0x0A}, []byte("x"))
	f.Add([]byte("\n\n\n\n"), []byte("\n"))
	f.Add([]byte{0x0A, 0x0A}, []byte{})
	f.Fuzz(func(t *testing.T, a, b []byte) {
		if len(a) == 0 || len(b) == 0 || len(a) > 4096 || len(b) > 4096 {
			return // empty frames are skipped by design
		}
		// The format can't tell a payload that starts with a valid frame of
		// its own from two frames; skip those rare inputs. (The decoder then
		// fails the connection with an error; it never stalls.)
		lc := Legacy.(legacy)
		if _, ok := lc.frameEndAnywhere(a); ok {
			return
		}
		if _, ok := lc.frameEndAnywhere(b); ok {
			return
		}
		stream := encodeAll(t, Legacy, a, b)
		got, err := decodeAll(t, Legacy, bytes.NewReader(stream), math.MaxUint16)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 2 || !bytes.Equal(got[0], a) || !bytes.Equal(got[1], b) {
			t.Fatalf("round trip changed frames: %q", got)
		}
	})
}

func BenchmarkRead(b *testing.B) {
	payload := []byte(strings.Repeat("p", 256))
	for name, c := range codecs {
		b.Run(name, func(b *testing.B) {
			frame, _ := c.Append(nil, payload)
			stream := bytes.Repeat(frame, 1000)
			b.SetBytes(int64(len(payload)))
			b.ReportAllocs()
			for b.Loop() {
				br := bufio.NewReader(bytes.NewReader(stream))
				for range 1000 {
					if _, err := c.Read(br, 1<<16); err != nil {
						b.Fatal(err)
					}
				}
			}
		})
	}
}

// frameEndAnywhere reports whether the encoding of p contains a valid frame
// end before its real one: the one ambiguity the Legacy format can't avoid
// (a payload that itself starts with a complete, correctly sized frame).
func (c legacy) frameEndAnywhere(p []byte) (int, bool) {
	enc, _ := c.Append(nil, p)
	for end := len(c.delim) + 2; end < len(enc); end++ {
		if n, ok := c.frameEnd(enc[:end]); ok {
			return n, true
		}
	}
	return 0, false
}
