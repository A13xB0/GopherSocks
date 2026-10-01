package gophersocks_test

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"testing"
	"time"

	gophersocks "github.com/A13xB0/GopherSocks"
	"github.com/A13xB0/GopherSocks/listener"
)

// startDatagramEcho serves sessions that answer each stream message with a
// datagram of the same bytes, reporting SendDatagram's error on errs.
func startDatagramEcho(t *testing.T, opts ...gophersocks.ServerOptFunc) (string, chan error) {
	t.Helper()
	port := freePort(t)
	l, err := gophersocks.NewQUICListener("127.0.0.1", port, opts...)
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Listen(); err != nil {
		t.Fatal(err)
	}
	errs := make(chan error, 8)
	ctx, cancel := context.WithCancel(t.Context())
	served := make(chan struct{})
	go func() {
		_ = l.Serve(ctx, func(_ context.Context, s gophersocks.Session) {
			for m := range s.Messages() {
				ds, ok := s.(listener.DatagramSender)
				if !ok {
					errs <- errors.New("session is not a DatagramSender")
					continue
				}
				err := ds.SendDatagram(m)
				if ds.RTT() <= 0 {
					err = fmt.Errorf("RTT %v after the handshake, want > 0", ds.RTT())
				} else if (err == nil) != ds.DatagramsEnabled() {
					err = fmt.Errorf("DatagramsEnabled %v but SendDatagram returned %v", ds.DatagramsEnabled(), err)
				}
				errs <- err
			}
		})
		close(served)
	}()
	t.Cleanup(func() {
		cancel()
		_ = l.StopListener()
		<-served
	})
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))), errs
}

// With datagrams enabled on both ends, a session's SendDatagram reaches the
// client's ReceiveDatagram.
func TestQUICDatagrams(t *testing.T) {
	addr, errs := startDatagramEcho(t, gophersocks.WithQUICDatagrams())
	c, err := gophersocks.NewQUICClient(addr, gophersocks.WithClientQUICDatagrams())
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	dr, ok := c.(gophersocks.DatagramReceiver)
	if !ok || !dr.DatagramsSupported() {
		t.Fatal("client should support datagrams")
	}
	if err := c.Send([]byte("position")); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; err != nil {
		t.Fatal("SendDatagram:", err)
	}
	got, err := dr.ReceiveDatagram(ctx)
	if err != nil || string(got) != "position" {
		t.Fatalf("got %q, %v", got, err)
	}
}

// A client that doesn't ask for datagrams gets none: SendDatagram says so.
func TestQUICDatagramsNeedBothEnds(t *testing.T) {
	addr, errs := startDatagramEcho(t, gophersocks.WithQUICDatagrams())
	c, err := gophersocks.NewQUICClient(addr)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := c.Send([]byte("x")); err != nil {
		t.Fatal(err)
	}
	if err := <-errs; !errors.Is(err, listener.ErrDatagramsUnsupported) {
		t.Fatalf("SendDatagram error %v, want ErrDatagramsUnsupported", err)
	}
}
