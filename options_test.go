package gophersocks_test

import (
	"bytes"
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"strconv"
	"testing"
	"time"

	gophersocks "github.com/A13xB0/GopherSocks"
	"github.com/A13xB0/GopherSocks/client"
	"github.com/A13xB0/GopherSocks/framing"
	"github.com/A13xB0/GopherSocks/listener"
	"github.com/quic-go/quic-go"
)

// Every top-level server option must reach the listener config (the review
// found protocol options silently dropped).
func TestServerOptionsApply(t *testing.T) {
	checkOrigin := func(*http.Request) bool { return false }
	tlsConf := &tls.Config{MinVersion: tls.VersionTLS13}
	qConf := &quic.Config{}

	ws := &listener.ServerConfig{ProtocolConfig: &listener.WebSocketConfig{}}
	for _, o := range []gophersocks.ServerOptFunc{
		gophersocks.WithMaxLength(7),
		gophersocks.WithBufferSize(8),
		gophersocks.WithSendQueueSize(9),
		gophersocks.WithTimeouts(time.Second, 2*time.Second),
		gophersocks.WithMaxConnections(10),
		gophersocks.WithLogger(&listener.DefaultLogger{}),
		gophersocks.WithWebSocketBufferSizes(11, 12),
		gophersocks.WithWebSocketPath("/p"),
		gophersocks.WithWebSocketCheckOrigin(checkOrigin),
	} {
		o(ws)
	}
	wsc := ws.ProtocolConfig.(*listener.WebSocketConfig)
	if ws.MaxLength != 7 || ws.BufferSize != 8 || ws.SendQueueSize != 9 || ws.ReadTimeout != time.Second ||
		ws.WriteTimeout != 2*time.Second || ws.MaxConnections != 10 || ws.Logger == nil ||
		wsc.ReadBufferSize != 11 || wsc.WriteBufferSize != 12 || wsc.Path != "/p" || wsc.CheckOrigin == nil {
		t.Fatalf("WebSocket options not applied: %+v %+v", ws, wsc)
	}

	q := &listener.ServerConfig{ProtocolConfig: &listener.QUICConfig{ALPN: []string{listener.DefaultALPN}}}
	for _, o := range []gophersocks.ServerOptFunc{
		gophersocks.WithTLSConfig(tlsConf),
		gophersocks.WithQUICConfig(qConf),
		gophersocks.WithQUICCodec("nixie/2", framing.Uvarint),
		gophersocks.ServerOptFunc(listener.WithQUICDelimiter([]byte("||"))),
	} {
		o(q)
	}
	qc := q.ProtocolConfig.(*listener.QUICConfig)
	if qc.TLSConfig != tlsConf || qc.QUICConfig != qConf || qc.ALPN[0] != "nixie/2" || qc.Codecs["nixie/2"] == nil || string(qc.Delimiter) != "||" {
		t.Fatalf("QUIC options not applied: %+v", qc)
	}
	// Protocol options for another protocol are ignored, not a panic.
	gophersocks.WithWebSocketPath("/x")(q)
	gophersocks.WithTLSConfig(tlsConf)(ws)
}

func TestClientOptionsApply(t *testing.T) {
	cfg := gophersocks.NewClientConfig(
		gophersocks.WithDelimiter([]byte("##")),
		gophersocks.WithClientTimeouts(time.Second, 2*time.Second),
		gophersocks.WithClientBufferSize(4096),
		gophersocks.WithClientMaxLength(99),
		gophersocks.WithQUICInsecureSkipVerify(false),
		gophersocks.WithQUICNextProtos([]string{"a"}),
		gophersocks.WithQUICMinVersion(tls.VersionTLS13),
		gophersocks.WithClientQUICCodec("b", framing.Uvarint),
	)
	q := cfg.ProtocolConfig.(*client.QUICConfig)
	if string(cfg.Delimiter) != "##" || cfg.ReadTimeout != time.Second || cfg.WriteTimeout != 2*time.Second ||
		cfg.BufferSize != 4096 || cfg.MaxLength != 99 || q.InsecureSkipVerify || q.MinVersion != tls.VersionTLS13 ||
		len(q.NextProtos) != 2 || q.NextProtos[0] != "b" || q.Codecs["b"] == nil {
		t.Fatalf("client options not applied: %+v %+v", cfg, q)
	}
	if _, err := gophersocks.NewQUICClient(""); err == nil {
		t.Fatal("empty address should fail")
	}
	if _, err := gophersocks.NewUDPClient(""); err == nil {
		t.Fatal("empty address should fail")
	}
}

// echoServe serves l with an echo handler until the test ends and reports
// each new session on the returned channel.
func echoServe(t *testing.T, l gophersocks.Listener) <-chan gophersocks.Session {
	t.Helper()
	if err := l.Listen(); err != nil {
		t.Fatal(err)
	}
	sessions := make(chan gophersocks.Session, 8)
	served := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		served <- l.Serve(ctx, func(_ context.Context, s gophersocks.Session) {
			sessions <- s
			for m := range s.Messages() {
				_ = s.SendToClient(m)
			}
		})
	}()
	t.Cleanup(func() {
		cancel()
		_ = l.StopListener()
		<-served
	})
	return sessions
}

func mustConnect(t *testing.T, c gophersocks.Client) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
}

func roundTrip(t *testing.T, c gophersocks.Client, msg []byte) {
	t.Helper()
	if err := c.Send(msg); err != nil {
		t.Fatal(err)
	}
	got, err := c.Receive()
	if err != nil || !bytes.Equal(got, msg) {
		t.Fatalf("round trip: got %d bytes, %v", len(got), err)
	}
}

func TestTopLevelTCPListener(t *testing.T) {
	tcp, err := gophersocks.NewTCPListener("127.0.0.1", freeTCPPort(t))
	if err != nil {
		t.Fatal(err)
	}
	if err := tcp.StartListener(); err != nil {
		t.Fatal(err)
	}
	if err := tcp.StopListener(); err != nil {
		t.Fatal(err)
	}
}

func TestTopLevelUDPListenerAndClient(t *testing.T) {
	port := freePort(t)
	udp, err := gophersocks.NewUDPListener("127.0.0.1", port)
	if err != nil {
		t.Fatal(err)
	}
	sessions := echoServe(t, udp)
	c, err := gophersocks.NewUDPClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	if err != nil {
		t.Fatal(err)
	}
	mustConnect(t, c)
	roundTrip(t, c, bytes.Repeat([]byte("d"), 3000)) // bigger than the old 1 KB client buffer
	if c.LocalAddr() == nil || c.RemoteAddr() == nil {
		t.Fatal("client addresses unset")
	}
	sess := <-sessions
	if sess.GetSessionID() == "" || udp.GetSession(sess.GetClientAddr().String()) == nil {
		t.Fatal("session lookup by address failed")
	}
	if udp.GetSession("203.0.113.1:1") != nil {
		t.Fatal("unknown address should have no session")
	}
}

func TestQUICClientBeforeConnect(t *testing.T) {
	c, err := gophersocks.NewQUICClient("127.0.0.1:1")
	if err != nil {
		t.Fatal(err)
	}
	if c.LocalAddr() != nil || c.RemoteAddr() != nil {
		t.Fatal("addresses should be nil before Connect")
	}
	if _, err := c.Receive(); err == nil {
		t.Fatal("Receive before Connect should fail")
	}
	if err := c.Send([]byte("x")); err == nil {
		t.Fatal("Send before Connect should fail")
	}
}

func TestQUICCustomDelimiter(t *testing.T) {
	port := freePort(t)
	l, err := listener.NewQUIC("127.0.0.1", port, context.Background(), listener.WithQUICDelimiter([]byte("||")))
	if err != nil {
		t.Fatal(err)
	}
	echoServe(t, l)
	c, err := gophersocks.NewQUICClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), gophersocks.WithDelimiter([]byte("||")))
	if err != nil {
		t.Fatal(err)
	}
	mustConnect(t, c)
	if c.LocalAddr() == nil || c.RemoteAddr() == nil {
		t.Fatal("addresses unset after Connect")
	}
	roundTrip(t, c, []byte("a||b"))
}
