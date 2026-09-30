package gophersocks_test

import (
	"context"
	"net"
	"strconv"
	"testing"
	"time"

	gophersocks "github.com/A13xB0/GopherSocks"
	"github.com/A13xB0/GopherSocks/framing"
	"github.com/gorilla/websocket"
)

// Regression for the review's quirk 3b: top-level protocol options were
// silently dropped (WithWebSocketPath did nothing).
func TestTopLevelWebSocketPathOptionApplies(t *testing.T) {
	port := freeTCPPort(t)
	l, err := gophersocks.NewWebSocketListener("127.0.0.1", port, gophersocks.WithWebSocketPath("/game"))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.StartListener(); err != nil {
		t.Fatal(err)
	}
	defer l.StopListener()
	base := "ws://" + net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))
	c, _, err := websocket.DefaultDialer.Dial(base+"/game", nil)
	if err != nil {
		t.Fatal("custom path should accept connections:", err)
	}
	_ = c.Close()
	if c, _, err := websocket.DefaultDialer.Dial(base+"/ws", nil); err == nil {
		_ = c.Close()
		t.Fatal("default path should not be served")
	}
}

// NewQUICListener exists at the top level, with the codec option, and the
// top-level client can talk to it.
func TestTopLevelQUICListenerAndClient(t *testing.T) {
	port := freePort(t)
	l, err := gophersocks.NewQUICListener("127.0.0.1", port,
		gophersocks.WithQUICCodec("nixie/2", framing.Uvarint),
		gophersocks.WithMaxConnections(10))
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Listen(); err != nil {
		t.Fatal(err)
	}
	served := make(chan error, 1)
	ctx, cancel := context.WithCancel(t.Context())
	go func() {
		served <- l.Serve(ctx, func(_ context.Context, s gophersocks.Session) {
			for m := range s.Messages() {
				_ = s.SendToClient(m)
			}
		})
	}()
	defer func() {
		cancel()
		_ = l.StopListener()
		<-served
	}()

	c, err := gophersocks.NewQUICClient(net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))),
		gophersocks.WithClientQUICCodec("nixie/2", framing.Uvarint),
		gophersocks.WithClientMaxLength(1<<20))
	if err != nil {
		t.Fatal(err)
	}
	dctx, dcancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer dcancel()
	if err := c.Connect(dctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Send([]byte("hi")); err != nil {
		t.Fatal(err)
	}
	got, err := c.Receive()
	if err != nil || string(got) != "hi" {
		t.Fatalf("got %q, %v", got, err)
	}
}
