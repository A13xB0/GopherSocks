package listenerprotocols

import (
	"fmt"
	"golang.org/x/net/context"
	"net"
	"reflect"
	"testing"
)

const (
	tcpHost = "127.0.0.1"
	tcpPort = 9000
)

func TestTCPListenerReceiveSingleMessage(t *testing.T) {
	//Setup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tListener, err := NewTCP(tcpHost, tcpPort, ctx, TCPConfig{})
	if err != nil {
		t.Fatal(err)
	}
	newSessionChan := make(chan Session)
	tListener.SetAnnounceNewSession(utilityGetSessionTCP, newSessionChan)
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
	conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", tcpHost, tcpPort))
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

func utilityGetSessionTCP(options any, session Session) {
	options.(chan Session) <- session
}
