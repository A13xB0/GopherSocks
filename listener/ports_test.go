package listener

import (
	"fmt"
	"math/rand/v2"
	"net"
)

// testBasePort is the start of a block of 64 ports that are free for both
// TCP and UDP when the test binary starts, so tests never collide with
// services already running on the machine (e.g. something on :9000).
var testBasePort = findFreePortBlock(64)

func findFreePortBlock(size uint16) uint16 {
	for range 200 {
		base := uint16(20000 + rand.IntN(40000))
		if blockFree(base, size) {
			return base
		}
	}
	panic("no free port block found")
}

func blockFree(base, size uint16) bool {
	for p := base; p < base+size; p++ {
		addr := fmt.Sprintf("127.0.0.1:%d", p)
		tl, err := net.Listen("tcp", addr)
		if err != nil {
			return false
		}
		ul, err := net.ListenPacket("udp", addr)
		_ = tl.Close()
		if err != nil {
			return false
		}
		_ = ul.Close()
	}
	return true
}
