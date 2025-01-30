package listener

import (
	"encoding/binary"
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"golang.org/x/net/context"
)

const (
	tcpHost = "127.0.0.1"
	tcpPort = 9000
)

func TestTCPListener(t *testing.T) {
	t.Run("Basic message exchange", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Create server with custom configuration
		tListener, err := NewTCP(tcpHost, tcpPort, ctx,
			WithMaxLength(1024),
			WithBufferSize(100),
			WithTimeouts(time.Second, time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}

		// Set up session announcement
		newSessionChan := make(chan Session)
		tListener.SetAnnounceNewSession(utilityGetSessionTCP, newSessionChan)

		// Start server
		go func() {
			if err := tListener.StartListener(); err != nil {
				t.Errorf("StartListener error: %v", err)
			}
		}()

		// Wait for server to start
		time.Sleep(time.Second)

		// Connect client
		conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", tcpHost, tcpPort))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Test message exchange
		want := []byte("Hello World!")
		length := uint32(len(want))
		if err := binary.Write(conn, binary.BigEndian, length); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(want); err != nil {
			t.Fatal(err)
		}

		// Get session and verify received message
		session := <-newSessionChan
		select {
		case got := <-session.Data():
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("want: %s, got: %s", want, got)
			}
		case <-time.After(time.Second):
			t.Fatal("timeout waiting for message")
		}

		// Test echo
		if err := session.SendToClient(want); err != nil {
			t.Fatal("echo:", err)
		}

		// Verify echo received
		var gotLength uint32
		if err := binary.Read(conn, binary.BigEndian, &gotLength); err != nil {
			t.Fatal("read length:", err)
		}
		if gotLength != length {
			t.Fatalf("length mismatch: want %d, got %d", length, gotLength)
		}
		got := make([]byte, gotLength)
		if _, err := conn.Read(got); err != nil {
			t.Fatal("read data:", err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("echo want: %s, got: %s", want, got)
		}

		// Clean up
		if err := tListener.StopListener(); err != nil {
			t.Fatal("stop:", err)
		}
	})

	t.Run("Max message length", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		maxLength := uint32(10) // Very small max length for testing
		tListener, err := NewTCP(tcpHost, tcpPort+1, ctx,
			WithMaxLength(maxLength),
			WithTimeouts(time.Second, time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}

		newSessionChan := make(chan Session)
		tListener.SetAnnounceNewSession(utilityGetSessionTCP, newSessionChan)

		go func() {
			if err := tListener.StartListener(); err != nil {
				t.Errorf("StartListener error: %v", err)
			}
		}()

		// Wait for server to start
		time.Sleep(time.Second)

		conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", tcpHost, tcpPort+1))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Send message larger than max length
		largeMsg := make([]byte, maxLength+1)
		if err := binary.Write(conn, binary.BigEndian, maxLength+1); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(largeMsg); err != nil {
			t.Fatal(err)
		}

		// Message should be dropped, channel should be empty
		session := <-newSessionChan
		select {
		case <-session.Data():
			t.Fatal("received message larger than max length")
		case <-time.After(100 * time.Millisecond):
			// Expected timeout
		}

		if err := tListener.StopListener(); err != nil {
			t.Fatal("stop:", err)
		}
	})

	t.Run("Max connections", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		maxConns := 2
		tListener, err := NewTCP(tcpHost, tcpPort+2, ctx,
			WithMaxConnections(maxConns),
			WithTimeouts(time.Second, time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}

		newSessionChan := make(chan Session)
		tListener.SetAnnounceNewSession(utilityGetSessionTCP, newSessionChan)

		go func() {
			if err := tListener.StartListener(); err != nil {
				t.Errorf("StartListener error: %v", err)
			}
		}()

		// Wait for server to start
		time.Sleep(time.Second)

		// Create maxConns connections
		conns := make([]net.Conn, maxConns)
		for i := 0; i < maxConns; i++ {
			conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", tcpHost, tcpPort+2))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conns[i] = conn
			<-newSessionChan // Wait for session announcement
		}

		// Try to create one more connection
		_, err = net.Dial("tcp", fmt.Sprintf("%s:%d", tcpHost, tcpPort+2))
		if err == nil {
			t.Fatal("expected connection to be rejected")
		}

		if err := tListener.StopListener(); err != nil {
			t.Fatal("stop:", err)
		}
	})

	t.Run("Session cleanup", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		tListener, err := NewTCP(tcpHost, tcpPort+3, ctx,
			WithTimeouts(time.Second, time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}

		newSessionChan := make(chan Session)
		tListener.SetAnnounceNewSession(utilityGetSessionTCP, newSessionChan)

		go func() {
			if err := tListener.StartListener(); err != nil {
				t.Errorf("StartListener error: %v", err)
			}
		}()

		// Wait for server to start
		time.Sleep(time.Second)

		// Create and immediately close a connection
		conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", tcpHost, tcpPort+3))
		if err != nil {
			t.Fatal(err)
		}

		// Wait for session to be created
		<-newSessionChan

		// Close connection and wait for cleanup
		conn.Close()
		time.Sleep(100 * time.Millisecond)

		// Verify session was cleaned up
		if len(tListener.GetActiveSessions()) != 0 {
			t.Fatal("session not cleaned up after connection close")
		}

		if err := tListener.StopListener(); err != nil {
			t.Fatal("stop:", err)
		}
	})
}

func utilityGetSessionTCP(options any, session Session) {
	options.(chan Session) <- session
}
