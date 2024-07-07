package listenerprotocols

import (
	"fmt"
	"golang.org/x/net/context"
	"net"
	"reflect"
	"testing"
)

const (
	udpHost = "127.0.0.1"
	udpPort = 9001
)

func TestUDPListenerReceiveSingleMessage(t *testing.T) {
	//Setup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tListener, err := NewUDP(udpHost, udpPort, ctx, UDPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	newSessionChan := make(chan Session)
	tListener.SetAnnounceNewSession(utilityGetSessionUDP, newSessionChan)
	errChan := make(chan error)
	go func() {
		errChan <- tListener.StartReceiveStream()
	}()
	if len(errChan) > 0 {
		t.Fatal(<-errChan)
	}
	// Test
	var want []byte = []byte("Hello World!")
	var got []byte
	//Send Packet
	conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", udpHost, udpPort))
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_, err = conn.Write(want)
	if err != nil {
		t.Fatal(err)
	}
	// Receive Packet
	session := <-newSessionChan

	got = <-session.Data()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want: %s, got: %s", want, got)
	}
}

func utilityGetSessionUDP(options any, session Session) {
	options.(chan Session) <- session
}
