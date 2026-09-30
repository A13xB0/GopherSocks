package gophersocks_test

import (
	"context"
	"encoding/binary"
	"fmt"
	"net"
	"strconv"
	"sync"
	"testing"
	"time"

	gophersocks "github.com/A13xB0/GopherSocks"
	"github.com/A13xB0/GopherSocks/listener"
)

// These benchmarks only use the public API that has existed since v0.0.13,
// so the same file measures old and new implementations.

func freePort(tb testing.TB) uint16 {
	tb.Helper()
	l, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	defer l.Close()
	return uint16(l.LocalAddr().(*net.UDPAddr).Port)
}

func freeTCPPort(tb testing.TB) uint16 {
	tb.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		tb.Fatal(err)
	}
	defer l.Close()
	return uint16(l.Addr().(*net.TCPAddr).Port)
}

type quicFixture struct {
	sessions chan listener.Session
	stop     func()
	addr     string
}

func startQUIC(tb testing.TB) *quicFixture {
	tb.Helper()
	port := freePort(tb)
	l, err := listener.NewQUIC("127.0.0.1", port, context.Background(),
		listener.WithMaxConnections(2000), listener.WithBufferSize(4096))
	if err != nil {
		tb.Fatal(err)
	}
	f := &quicFixture{sessions: make(chan listener.Session, 2000), addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port)))}
	l.SetAnnounceNewSession(func(_ any, s listener.Session) { f.sessions <- s }, nil)
	if err := l.StartListener(); err != nil {
		tb.Fatal(err)
	}
	f.stop = func() { _ = l.StopListener() }
	return f
}

func dialQUIC(tb testing.TB, f *quicFixture) (gophersocks.Client, listener.Session) {
	tb.Helper()
	c, err := gophersocks.NewQUICClient(f.addr)
	if err != nil {
		tb.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := c.Connect(ctx); err != nil {
		tb.Fatal(err)
	}
	// QUIC streams become visible to the server on first write.
	if err := c.Send([]byte("hello")); err != nil {
		tb.Fatal(err)
	}
	select {
	case s := <-f.sessions:
		select {
		case <-s.Data(): // consume hello
		case <-time.After(5 * time.Second):
			tb.Fatal("no hello")
		}
		return c, s
	case <-time.After(5 * time.Second):
		tb.Fatal("no session")
	}
	return nil, nil
}

// BenchmarkQUICEcho measures one client→server→client round trip.
func BenchmarkQUICEcho(b *testing.B) {
	f := startQUIC(b)
	defer f.stop()
	c, s := dialQUIC(b, f)
	defer c.Close()
	go func() {
		for d := range s.Data() {
			_ = s.SendToClient(d)
		}
	}()
	msg := make([]byte, 64)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := c.Send(msg); err != nil {
			b.Fatal(err)
		}
		if _, err := c.Receive(); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkQUICInbound measures client→server throughput.
func BenchmarkQUICInbound(b *testing.B) {
	for _, size := range []int{64, 1024, 8192} {
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) { benchQUICInbound(b, size) })
	}
}

func benchQUICInbound(b *testing.B, size int) {
	f := startQUIC(b)
	defer f.stop()
	c, s := dialQUIC(b, f)
	defer c.Close()
	done := countMessages(s, int64(b.N))
	msg := make([]byte, size)
	b.SetBytes(int64(size))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := c.Send(msg); err != nil {
			b.Fatal(err)
		}
	}
	waitDone(b, done)
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msgs/s")
}

// countMessages closes the returned channel once n messages have arrived.
func countMessages(s listener.Session, n int64) <-chan struct{} {
	done := make(chan struct{})
	go func() {
		var got int64
		for range s.Data() {
			if got++; got == n {
				close(done)
				return
			}
		}
	}()
	return done
}

func waitDone(b *testing.B, done <-chan struct{}) {
	b.Helper()
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		b.Fatal("timed out waiting for messages")
	}
}

// BenchmarkQUICOutbound measures server→client throughput.
func BenchmarkQUICOutbound(b *testing.B) {
	for _, size := range []int{64, 1024, 8192} {
		b.Run(fmt.Sprintf("%dB", size), func(b *testing.B) { benchQUICOutbound(b, size) })
	}
}

func benchQUICOutbound(b *testing.B, size int) {
	f := startQUIC(b)
	defer f.stop()
	c, s := dialQUIC(b, f)
	defer c.Close()
	done := receiveN(c, b.N)
	msg := make([]byte, size)
	b.SetBytes(int64(size))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if err := s.SendToClient(msg); err != nil {
			b.Fatal(err)
		}
	}
	if err := <-done; err != nil {
		b.Fatal(err)
	}
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msgs/s")
}

// receiveN reads n messages on the client and reports the first error.
func receiveN(c gophersocks.Client, n int) <-chan error {
	done := make(chan error, 1)
	go func() {
		for range n {
			if _, err := c.Receive(); err != nil {
				done <- err
				return
			}
		}
		done <- nil
	}()
	return done
}

// BenchmarkQUICFanout: one goroutine sends each message to 100 clients
// (the shape of a world-chat broadcast). Reported per delivered message.
func BenchmarkQUICFanout(b *testing.B) {
	const clients = 100
	f := startQUIC(b)
	defer f.stop()
	cs := make([]gophersocks.Client, clients)
	ss := make([]listener.Session, clients)
	for i := range clients {
		cs[i], ss[i] = dialQUIC(b, f)
		defer cs[i].Close()
	}
	per := b.N/clients + 1
	var wg sync.WaitGroup
	for _, c := range cs {
		wg.Go(func() { <-receiveN(c, per) })
	}
	msg := make([]byte, 256)
	b.ReportAllocs()
	b.ResetTimer()
	for range per {
		for _, s := range ss {
			if err := s.SendToClient(msg); err != nil {
				b.Fatal(err)
			}
		}
	}
	wg.Wait()
	b.ReportMetric(float64(per*clients)/b.Elapsed().Seconds(), "msgs/s")
}

// BenchmarkTCPInbound measures client→server throughput with the uint32 length prefix.
func BenchmarkTCPInbound(b *testing.B) {
	port := freeTCPPort(b)
	l, err := listener.NewTCP("127.0.0.1", port, context.Background(), listener.WithBufferSize(4096))
	if err != nil {
		b.Fatal(err)
	}
	sessions := make(chan listener.Session, 1)
	l.SetAnnounceNewSession(func(_ any, s listener.Session) { sessions <- s }, nil)
	if err := l.StartListener(); err != nil {
		b.Fatal(err)
	}
	defer l.StopListener()
	conn, err := net.Dial("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(int(port))))
	if err != nil {
		b.Fatal(err)
	}
	defer conn.Close()
	s := <-sessions
	done := countMessages(s, int64(b.N))
	payload := make([]byte, 256)
	frame := binary.BigEndian.AppendUint32(nil, uint32(len(payload)))
	frame = append(frame, payload...)
	b.SetBytes(int64(len(payload)))
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		if _, err := conn.Write(frame); err != nil {
			b.Fatal(err)
		}
	}
	waitDone(b, done)
	b.ReportMetric(float64(b.N)/b.Elapsed().Seconds(), "msgs/s")
}
