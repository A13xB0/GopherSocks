package listener

import (
	"bytes"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"runtime"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/A13xB0/GopherSocks/client"
	"github.com/A13xB0/GopherSocks/framing"
	"github.com/quic-go/quic-go"
)

const waitLong = 5 * time.Second

// quicServer runs Serve with a handler until the test ends.
type quicServer struct {
	l        Listener
	addr     string
	sessions chan Session
}

func serveQUIC(t *testing.T, h Handler, opts ...ServerOption) *quicServer {
	t.Helper()
	port := freeUDPPort(t)
	l, err := NewQUIC("127.0.0.1", port, t.Context(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	qs := &quicServer{l: l, addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), sessions: make(chan Session, 64)}
	if err := l.Listen(); err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() {
		served <- l.Serve(t.Context(), func(ctx context.Context, s Session) {
			qs.sessions <- s
			h(ctx, s)
		})
	}()
	t.Cleanup(func() {
		_ = l.StopListener()
		select {
		case <-served:
		case <-time.After(waitLong):
			t.Error("Serve did not return after StopListener")
		}
	})
	return qs
}

// echo sends every message straight back.
func echo(_ context.Context, s Session) {
	for m := range s.Messages() {
		if s.SendToClient(m) != nil {
			return
		}
	}
}

// hold keeps the session open without reading.
func hold(ctx context.Context, _ Session) { <-ctx.Done() }

func dialQUIC(t *testing.T, addr string, opts ...func(*client.ClientConfig)) *client.QUICClient {
	t.Helper()
	cfg := client.DefaultConfig()
	for _, o := range opts {
		o(cfg)
	}
	c, err := client.NewQUICClient(addr, cfg)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), waitLong)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func receive(t *testing.T, c *client.QUICClient) []byte {
	t.Helper()
	type result struct {
		m   []byte
		err error
	}
	ch := make(chan result, 1)
	go func() {
		m, err := c.Receive()
		ch <- result{m, err}
	}()
	select {
	case r := <-ch:
		if r.err != nil {
			t.Fatal(r.err)
		}
		return r.m
	case <-time.After(waitLong):
		t.Fatal("timed out waiting for a message")
		return nil
	}
}

func nextSession(t *testing.T, qs *quicServer) Session {
	t.Helper()
	select {
	case s := <-qs.sessions:
		return s
	case <-time.After(waitLong):
		t.Fatal("no session")
		return nil
	}
}

// Regression for review finding GS-1, end to end: 0A 0A 0A inside a
// protobuf payload wedged the stream and lost every later message.
func TestQUICPayloadContainingDelimiter(t *testing.T) {
	qs := serveQUIC(t, echo)
	c := dialQUIC(t, qs.addr)
	wedge := []byte{0x08, 0x01, 0x0A, 0x0A, 0x0A, 0x12, 0x03, 'a', 'b', 'c'}
	for _, m := range [][]byte{wedge, []byte("second")} {
		if err := c.Send(m); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, c); !bytes.Equal(got, m) {
			t.Fatalf("got %x, want %x", got, m)
		}
	}
}

// Regression for GS-2: a hard-coded 10,000-byte cap ignored WithMaxLength.
func TestQUICLargeMessage(t *testing.T) {
	qs := serveQUIC(t, echo)
	c := dialQUIC(t, qs.addr)
	big := bytes.Repeat([]byte("n"), 60000)
	if err := c.Send(big); err != nil {
		t.Fatal(err)
	}
	if got := receive(t, c); !bytes.Equal(got, big) {
		t.Fatalf("got %d bytes back, want %d", len(got), len(big))
	}
}

// Regression for GS-4: AcceptStream ran inside the accept loop, so a client
// that connected but never opened a stream blocked every later client.
func TestQUICIdleClientDoesNotBlockAccept(t *testing.T) {
	qs := serveQUIC(t, echo)
	ctx, cancel := context.WithTimeout(t.Context(), waitLong)
	defer cancel()
	idle, err := quic.DialAddr(ctx, qs.addr, &tls.Config{InsecureSkipVerify: true, NextProtos: []string{DefaultALPN}}, nil) //nolint:gosec // test against a self-signed server
	if err != nil {
		t.Fatal(err)
	}
	defer idle.CloseWithError(0, "")

	start := time.Now()
	c := dialQUIC(t, qs.addr)
	if err := c.Send([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	if string(receive(t, c)) != "hi" {
		t.Fatal("echo failed")
	}
	if d := time.Since(start); d > 2*time.Second {
		t.Fatalf("second client waited %v behind the idle one", d)
	}
}

// Regression for GS-4: after StopListener the accept loop spun forever.
func TestQUICStopListenerReleasesGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()
	port := freeUDPPort(t)
	l, err := NewQUIC("127.0.0.1", port, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := l.StartListener(); err != nil {
		t.Fatal(err)
	}
	c := dialQUIC(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	_ = c.Send([]byte("x"))
	time.Sleep(100 * time.Millisecond)
	if err := l.StopListener(); err != nil {
		t.Fatal(err)
	}
	_ = c.Close()
	deadline := time.Now().Add(waitLong)
	for runtime.NumGoroutine() > before+2 {
		if time.Now().After(deadline) {
			t.Fatalf("%d goroutines left running (started with %d)", runtime.NumGoroutine(), before)
		}
		time.Sleep(20 * time.Millisecond)
	}
}

// Regression for GS-6: closing a session closed DataChannel while the reader
// could still send on it ("send on closed channel"). Run with -race.
func TestQUICCloseWhileReadingDoesNotPanic(t *testing.T) {
	for range 10 {
		func() {
			port := freeUDPPort(t)
			l, err := NewQUIC("127.0.0.1", port, context.Background())
			if err != nil {
				t.Fatal(err)
			}
			got := make(chan Session, 1)
			l.SetAnnounceNewSession(func(_ any, s Session) { got <- s }, nil)
			if err := l.StartListener(); err != nil {
				t.Fatal(err)
			}
			defer l.StopListener()
			c := dialQUIC(t, net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
			stop := make(chan struct{})
			go func() {
				for {
					select {
					case <-stop:
						return
					default:
						if c.Send([]byte("flood")) != nil {
							return
						}
					}
				}
			}()
			s := <-got
			go func() {
				for range s.Data() {
					// Drain until the session closes.
				}
			}()
			time.Sleep(10 * time.Millisecond)
			s.CloseSession()
			_ = s.SendToClient([]byte("after close"))
			close(stop)
		}()
	}
}

// Regression for GS-7: concurrent SendToClient calls could interleave frames.
func TestConcurrentSendsArriveIntact(t *testing.T) {
	qs := serveQUIC(t, hold)
	c := dialQUIC(t, qs.addr)
	_ = c.Send([]byte("hello"))
	s := nextSession(t, qs)
	const senders, each = 8, 200
	var wg sync.WaitGroup
	for g := range senders {
		wg.Go(func() {
			for i := range each {
				if err := s.SendToClient(fmt.Appendf(nil, "%d:%d:%s", g, i, bytes.Repeat([]byte("p"), 300))); err != nil {
					t.Error(err)
					return
				}
			}
		})
	}
	next := make([]int, senders)
	for range senders * each {
		var g, i int
		if _, err := fmt.Sscanf(string(receive(t, c)), "%d:%d:", &g, &i); err != nil {
			t.Fatal("corrupt frame:", err)
		}
		if i != next[g] {
			t.Fatalf("sender %d: got message %d, want %d", g, i, next[g])
		}
		next[g]++
	}
	wg.Wait()
}

// Messages is pulled: while the handler isn't reading, QUIC flow control
// stops the client instead of the server buffering without bound.
func TestQUICBackpressure(t *testing.T) {
	release := make(chan struct{})
	var received int
	done := make(chan struct{})
	qs := serveQUIC(t, func(_ context.Context, s Session) {
		<-release
		for range s.Messages() {
			if received++; received == 5000 {
				close(done)
				return
			}
		}
	})
	c := dialQUIC(t, qs.addr)
	sent := make(chan int, 1)
	go func() {
		payload := bytes.Repeat([]byte("b"), 10000)
		n := 0
		for range 5000 { // 50 MB
			if c.Send(payload) != nil {
				break
			}
			n++
		}
		sent <- n
	}()
	select {
	case n := <-sent:
		t.Fatalf("client sent all %d messages while the server wasn't reading", n)
	case <-time.After(500 * time.Millisecond):
	}
	close(release)
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatalf("server received %d of 5000", received)
	}
}

// A client that stops reading is closed once its send queue stays full for
// WriteTimeout.
func TestSlowConsumerIsClosed(t *testing.T) {
	qs := serveQUIC(t, hold, WithSendQueueSize(4), WithTimeouts(30*time.Second, 200*time.Millisecond))
	c := dialQUIC(t, qs.addr)
	_ = c.Send([]byte("hello")) // then never read
	s := nextSession(t, qs)
	payload := bytes.Repeat([]byte("s"), 60000)
	var err error
	for range 10000 {
		if err = s.SendToClient(payload); err != nil {
			break
		}
	}
	if !errors.Is(err, ErrSlowConsumer) && !errors.Is(context.Cause(s.Context()), ErrSlowConsumer) {
		t.Fatalf("want ErrSlowConsumer, got %v (cause %v)", err, context.Cause(s.Context()))
	}
}

// Servers can offer a new framing by ALPN and still accept old clients.
func TestQUICCodecIsChosenByALPN(t *testing.T) {
	qs := serveQUIC(t, echo, WithQUICCodec("nixie/2", framing.Uvarint))

	newClient := dialQUIC(t, qs.addr, func(cfg *client.ClientConfig) {
		q := cfg.ProtocolConfig.(*client.QUICConfig)
		q.NextProtos = []string{"nixie/2", client.DefaultALPN}
		q.Codecs = map[string]framing.Codec{"nixie/2": framing.Uvarint}
	})
	if p := newClient.NegotiatedProtocol(); p != "nixie/2" {
		t.Fatalf("negotiated %q, want nixie/2", p)
	}
	oldClient := dialQUIC(t, qs.addr)
	if p := oldClient.NegotiatedProtocol(); p != DefaultALPN {
		t.Fatalf("negotiated %q, want %s", p, DefaultALPN)
	}
	for _, c := range []*client.QUICClient{newClient, oldClient} {
		msg := []byte{0x0A, 0x0A, 0x0A, 0x00, 0x03}
		if err := c.Send(msg); err != nil {
			t.Fatal(err)
		}
		if got := receive(t, c); !bytes.Equal(got, msg) {
			t.Fatalf("%s: got %x", c.NegotiatedProtocol(), got)
		}
	}
}

func TestHandlerReturnClosesSession(t *testing.T) {
	var sess Session
	returned := make(chan struct{})
	qs := serveQUIC(t, func(_ context.Context, s Session) {
		sess = s
		for range s.Messages() {
			break
		}
		close(returned)
	})
	c := dialQUIC(t, qs.addr)
	_ = c.Send([]byte("one"))
	<-returned
	select {
	case <-sess.Context().Done():
	case <-time.After(waitLong):
		t.Fatal("session still open after its handler returned")
	}
	if len(qs.l.GetActiveSessions()) != 0 {
		t.Fatal("closed session still listed")
	}
}

func TestServeReturnsWhenContextEnds(t *testing.T) {
	l, err := NewQUIC("127.0.0.1", freeUDPPort(t), context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer l.StopListener()
	ctx, cancel := context.WithCancel(t.Context())
	served := make(chan error, 1)
	go func() { served <- l.Serve(ctx, hold) }()
	time.Sleep(50 * time.Millisecond)
	cancel()
	select {
	case err := <-served:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(waitLong):
		t.Fatal("Serve ignored its context")
	}
}

func TestQUICUsesProvidedTLSConfig(t *testing.T) {
	cert, err := selfSignedCert()
	if err != nil {
		t.Fatal(err)
	}
	qs := serveQUIC(t, echo, WithTLSConfig(&tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS13}))
	c := dialQUIC(t, qs.addr)
	if c.NegotiatedProtocol() != DefaultALPN {
		t.Fatal("NextProtos was not filled in for a TLS config without them")
	}
}

func TestQUICRejectsUnknownALPNCodec(t *testing.T) {
	l, err := NewQUIC("127.0.0.1", freeUDPPort(t), context.Background(), func(c *ServerConfig) {
		c.ProtocolConfig.(*QUICConfig).ALPN = []string{"nothing"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := l.StartListener(); err == nil {
		_ = l.StopListener()
		t.Fatal("a protocol without a codec should fail to start")
	}
}
