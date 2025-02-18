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
	tcpHost   = "127.0.0.1"
	tcpHostV6 = "::1" // IPv6 loopback address
	tcpPort   = 9000
	tcpPortV6 = 9010
)

// setupTCPListener creates and starts a TCP listener with the given options
func setupTCPListener(t *testing.T, host string, port uint16, opts ...ServerOption) (Listener, chan Session, chan error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	// Create server
	tListener, err := NewTCP(host, port, ctx, opts...)
	if err != nil {
		t.Fatal(err)
	}

	// Set up session announcement
	newSessionChan := make(chan Session)
	tListener.SetAnnounceNewSession(utilityGetSessionTCP, newSessionChan)

	// Start server
	serverErrChan := make(chan error, 1)
	go func() {
		if err := tListener.StartListener(); err != nil {
			serverErrChan <- err
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)

	// Check for startup errors
	select {
	case err := <-serverErrChan:
		t.Fatalf("server startup error: %v", err)
	default:
		// Server started successfully
	}

	return tListener, newSessionChan, serverErrChan
}

func TestTCPListenerIPv4(t *testing.T) {
	t.Run("Basic message exchange", func(t *testing.T) {
		tListener, newSessionChan, _ := setupTCPListener(t, tcpHost, tcpPort,
			WithMaxLength(1024),
			WithBufferSize(100),
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		defer tListener.StopListener()

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
		var session Session
		select {
		case session = <-newSessionChan:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for session")
		}

		select {
		case got := <-session.Data():
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("want: %s, got: %s", want, got)
			}
		case <-time.After(100 * time.Millisecond):
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

	t.Run("Session cleanup", func(t *testing.T) {
		tListener, newSessionChan, _ := setupTCPListener(t, tcpHost, tcpPort+3,
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		defer tListener.StopListener()

		// Create and immediately close a connection
		conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", tcpHost, tcpPort+3))
		if err != nil {
			t.Fatal(err)
		}

		// Wait for session to be created
		select {
		case <-newSessionChan:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for session")
		}

		// Close connection and wait for cleanup
		conn.Close()
		time.Sleep(50 * time.Millisecond)

		// Verify session was cleaned up
		if len(tListener.GetActiveSessions()) != 0 {
			t.Fatal("session not cleaned up after connection close")
		}

		if err := tListener.StopListener(); err != nil {
			t.Fatal("stop:", err)
		}
	})

	t.Run("Multiple sessions", func(t *testing.T) {
		tListener, newSessionChan, _ := setupTCPListener(t, tcpHost, tcpPort+4,
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		defer tListener.StopListener()

		// Create multiple clients
		numClients := 3
		conns := make([]net.Conn, numClients)
		for i := 0; i < numClients; i++ {
			conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", tcpHost, tcpPort+4))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conns[i] = conn

			// Send message to create session
			msg := []byte(fmt.Sprintf("hello from client %d", i))
			if err := binary.Write(conn, binary.BigEndian, uint32(len(msg))); err != nil {
				t.Fatal(err)
			}
			if _, err := conn.Write(msg); err != nil {
				t.Fatal(err)
			}

			// Wait for session
			var session Session
			select {
			case session = <-newSessionChan:
			case <-time.After(100 * time.Millisecond):
				t.Fatal("timeout waiting for session")
			}

			// Read initial message
			select {
			case <-session.Data():
			case <-time.After(100 * time.Millisecond):
				t.Fatal("timeout waiting for message")
			}
		}

		// Verify all sessions are active
		if len(tListener.GetActiveSessions()) != numClients {
			t.Fatalf("expected %d active sessions, got %d", numClients, len(tListener.GetActiveSessions()))
		}
	})

	t.Run("Invalid session type handling", func(t *testing.T) {
		tListener, newSessionChan, _ := setupTCPListener(t, tcpHost, tcpPort+5,
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		defer tListener.StopListener()

		// Connect client
		conn, err := net.Dial("tcp", fmt.Sprintf("%s:%d", tcpHost, tcpPort+5))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Send message to create session
		msg := []byte("hello")
		if err := binary.Write(conn, binary.BigEndian, uint32(len(msg))); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(msg); err != nil {
			t.Fatal(err)
		}

		// Get session
		var session Session
		select {
		case session = <-newSessionChan:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for session")
		}

		// Verify session is of correct type
		if _, ok := session.(*TCPSession); !ok {
			t.Fatal("session is not of type *TCPSession")
		}

		// Read initial message
		select {
		case <-session.Data():
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for message")
		}
	})
}

func TestTCPListenerIPv6(t *testing.T) {
	t.Run("Basic IPv6 message exchange", func(t *testing.T) {
		tListener, newSessionChan, _ := setupTCPListener(t, fmt.Sprintf("[%s]", tcpHostV6), tcpPortV6,
			WithMaxLength(1024),
			WithBufferSize(100),
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		defer tListener.StopListener()

		// Connect client
		conn, err := net.Dial("tcp6", fmt.Sprintf("[%s]:%d", tcpHostV6, tcpPortV6))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Test message exchange
		want := []byte("Hello IPv6 World!")
		length := uint32(len(want))
		if err := binary.Write(conn, binary.BigEndian, length); err != nil {
			t.Fatal(err)
		}
		if _, err := conn.Write(want); err != nil {
			t.Fatal(err)
		}

		// Get session and verify received message
		var session Session
		select {
		case session = <-newSessionChan:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for session")
		}

		select {
		case got := <-session.Data():
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("want: %s, got: %s", want, got)
			}
		case <-time.After(100 * time.Millisecond):
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
}

func utilityGetSessionTCP(options any, session Session) {
	options.(chan Session) <- session
}
