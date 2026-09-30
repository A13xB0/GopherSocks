package listener

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"log/slog"
	"net"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func serveListener(t *testing.T, l Listener, h Handler) {
	t.Helper()
	if err := l.Listen(); err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	go func() { served <- l.Serve(t.Context(), h) }()
	t.Cleanup(func() {
		_ = l.StopListener()
		select {
		case <-served:
		case <-time.After(waitLong):
			t.Error("Serve did not return after StopListener")
		}
	})
}

func writeTCPFrame(t *testing.T, c net.Conn, payload []byte) {
	t.Helper()
	frame := binary.BigEndian.AppendUint32(nil, uint32(len(payload)))
	if _, err := c.Write(append(frame, payload...)); err != nil {
		t.Fatal(err)
	}
}

func readTCPFrame(c net.Conn) ([]byte, error) {
	_ = c.SetReadDeadline(time.Now().Add(waitLong))
	var hdr [4]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return nil, err
	}
	p := make([]byte, binary.BigEndian.Uint32(hdr[:]))
	_, err := io.ReadFull(c, p)
	return p, err
}

func tcpEchoServer(t *testing.T, opts ...ServerOption) string {
	t.Helper()
	port := freeTCPPort(t)
	l, err := NewTCP("127.0.0.1", port, t.Context(), opts...)
	if err != nil {
		t.Fatal(err)
	}
	serveListener(t, l, echo)
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
}

func dialTCP(t *testing.T, addr string) net.Conn {
	t.Helper()
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

func tcpRoundTrip(t *testing.T, c net.Conn, msg string) {
	t.Helper()
	writeTCPFrame(t, c, []byte(msg))
	got, err := readTCPFrame(c)
	if err != nil || string(got) != msg {
		t.Fatalf("echo %q: got %q, %v", msg, got, err)
	}
}

func TestTCPServeEcho(t *testing.T) {
	c := dialTCP(t, tcpEchoServer(t))
	tcpRoundTrip(t, c, "hello")
	tcpRoundTrip(t, c, strings.Repeat("x", 100000))
}

// Regression for review finding GS-5: one connection over MaxConnections
// cancelled the server context, stopping it and every session.
func TestTCPMaxConnectionsRejectsOneAndKeepsServing(t *testing.T) {
	addr := tcpEchoServer(t, WithMaxConnections(1))
	first := dialTCP(t, addr)
	tcpRoundTrip(t, first, "first")

	second := dialTCP(t, addr)
	if _, err := readTCPFrame(second); err == nil {
		t.Fatal("the connection over the limit should be closed")
	}
	tcpRoundTrip(t, first, "still served")

	_ = first.Close()
	time.Sleep(100 * time.Millisecond)
	third := dialTCP(t, addr)
	tcpRoundTrip(t, third, "slot freed")
}

// Regression for GS-7: an oversized frame was skipped without consuming its
// body, so the next "length" was payload bytes. Now the connection closes.
func TestTCPOversizedFrameClosesConnection(t *testing.T) {
	c := dialTCP(t, tcpEchoServer(t, WithMaxLength(64)))
	writeTCPFrame(t, c, bytes.Repeat([]byte("z"), 65))
	if _, err := readTCPFrame(c); err == nil {
		t.Fatal("server should close a connection that sends an oversized frame")
	}
}

// Regression for GS-3: the UDP read buffer was the channel size (100), so
// datagrams were cut to 100 bytes.
func TestUDPLargeDatagramsArriveWhole(t *testing.T) {
	port := freeUDPPort(t)
	l, err := NewUDP("127.0.0.1", port, t.Context())
	if err != nil {
		t.Fatal(err)
	}
	serveListener(t, l, echo)
	c, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	buf := make([]byte, 65536)
	for _, size := range []int{500, 1400, 60000} {
		msg := bytes.Repeat([]byte("u"), size)
		if _, err := c.Write(msg); err != nil {
			t.Fatal(err)
		}
		_ = c.SetReadDeadline(time.Now().Add(waitLong))
		n, err := c.Read(buf)
		if err != nil || n != size {
			t.Fatalf("sent %d bytes, echo was %d (%v)", size, n, err)
		}
	}
}

// UDP sessions now expire after ReadTimeout, as the README always said.
func TestUDPIdleSessionIsClosed(t *testing.T) {
	port := freeUDPPort(t)
	l, err := NewUDP("127.0.0.1", port, t.Context(), WithTimeouts(150*time.Millisecond, time.Second))
	if err != nil {
		t.Fatal(err)
	}
	opened := make(chan Session, 1)
	serveListener(t, l, func(ctx context.Context, s Session) {
		opened <- s
		<-ctx.Done()
	})
	c, err := net.Dial("udp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if _, err := c.Write([]byte("ping")); err != nil {
		t.Fatal(err)
	}
	s := <-opened
	select {
	case <-s.Context().Done():
	case <-time.After(waitLong):
		t.Fatal("idle UDP session was not closed")
	}
}

func wsURL(port uint16, path string) string {
	u := url.URL{Scheme: "ws", Host: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), Path: path}
	return u.String()
}

func TestWebSocketServeEcho(t *testing.T) {
	port := freeTCPPort(t)
	l, err := NewWebSocket("127.0.0.1", port, t.Context(), WithWebSocketPath("/game"))
	if err != nil {
		t.Fatal(err)
	}
	serveListener(t, l, echo)
	c, _, err := websocket.DefaultDialer.Dial(wsURL(port, "/game"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	for _, msg := range [][]byte{[]byte("hello"), bytes.Repeat([]byte{0x0A}, 5000)} {
		if err := c.WriteMessage(websocket.BinaryMessage, msg); err != nil {
			t.Fatal(err)
		}
		_ = c.SetReadDeadline(time.Now().Add(waitLong))
		_, got, err := c.ReadMessage()
		if err != nil || !bytes.Equal(got, msg) {
			t.Fatalf("echo: %d bytes, %v", len(got), err)
		}
	}
}

// StartListener used to sleep 100 ms and hope; bind errors are now returned.
func TestWebSocketStartListenerReturnsBindError(t *testing.T) {
	taken, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer taken.Close()
	port := uint16(taken.Addr().(*net.TCPAddr).Port)
	l, err := NewWebSocket("127.0.0.1", port, context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if err := l.StartListener(); err == nil {
		_ = l.StopListener()
		t.Fatal("StartListener should report the port is in use")
	}
}

func TestHostPortAcceptsBracketedIPv6(t *testing.T) {
	for _, host := range []string{"::1", "[::1]"} {
		if got := hostPort(host, 9000); got != "[::1]:9000" {
			t.Fatalf("hostPort(%q) = %q", host, got)
		}
	}
	if got := hostPort("127.0.0.1", 9000); got != "127.0.0.1:9000" {
		t.Fatalf("got %q", got)
	}
}

func TestSlogLogger(t *testing.T) {
	var buf bytes.Buffer
	l := NewSlogLogger(slog.New(slog.NewTextHandler(&buf, &slog.HandlerOptions{Level: slog.LevelDebug})))
	l.Debug("d", "k", 1)
	l.Info("i")
	l.Warn("w")
	l.Error("e")
	for _, want := range []string{"msg=d k=1", "msg=i", "msg=w", "msg=e"} {
		if !strings.Contains(buf.String(), want) {
			t.Fatalf("log missing %q:\n%s", want, buf.String())
		}
	}
	var d DefaultLogger
	d.Debug("x")
	d.Info("x")
	d.Warn("x")
	d.Error("x")
}
