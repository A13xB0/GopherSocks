package gophersocks_test

import (
	"runtime"
	"testing"
	"time"

	gophersocks "github.com/A13xB0/GopherSocks"
	"github.com/A13xB0/GopherSocks/listener"
)

// BenchmarkQUICSessionMemory reports what a connected session holds: idle,
// and after a burst of 64 KB was sent to it (a game client entering a busy
// map, say). Both ends are in this process, so a client's own buffers are
// counted too; what changes between versions is the server's share.
func BenchmarkQUICSessionMemory(b *testing.B) {
	b.Run("default", func(b *testing.B) { benchSessionMemory(b) })
	b.Run("read-4KB", func(b *testing.B) { benchSessionMemory(b, listener.WithReadBufferSize(4096)) })
}

func benchSessionMemory(b *testing.B, opts ...listener.ServerOption) {
	const sessions = 200
	for range b.N {
		f := startQUIC(b, opts...)
		base := heapInUse()
		cs := make([]gophersocks.Client, sessions)
		ss := make([]listener.Session, sessions)
		for i := range sessions {
			cs[i], ss[i] = dialQUIC(b, f)
		}
		idle := heapInUse()
		burst := make([]byte, 4096)
		for i, s := range ss {
			done := receiveN(cs[i], 16)
			for range 16 {
				if err := s.SendToClient(burst); err != nil {
					b.Fatal(err)
				}
			}
			if err := <-done; err != nil {
				b.Fatal(err)
			}
		}
		after := heapInUse()
		b.ReportMetric(float64(idle-base)/sessions, "idle-B/session")
		b.ReportMetric(float64(after-base)/sessions, "after-burst-B/session")
		for _, c := range cs {
			_ = c.Close()
		}
		f.stop()
	}
}

func heapInUse() int64 {
	var m runtime.MemStats
	for range 3 {
		runtime.GC()
		time.Sleep(50 * time.Millisecond)
	}
	runtime.ReadMemStats(&m)
	return int64(m.HeapInuse)
}
