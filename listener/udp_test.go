package listener

import (
	"fmt"
	"net"
	"reflect"
	"testing"
	"time"

	"golang.org/x/net/context"
)

const (
	udpHost   = "127.0.0.1"
	udpHostV6 = "[::1]"
	udpPort   = 9001
	udpPortV6 = 9002
)

// setupUDPListener creates and starts a UDP listener with the given options
func setupUDPListener(t *testing.T, host string, port uint16, opts ...ServerOption) (Listener, chan Session, chan error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	// Create server
	tListener, err := NewUDP(host, port, ctx, opts...)
	if err != nil {
		t.Fatal(err)
	}

	// Set up session announcement
	newSessionChan := make(chan Session)
	tListener.SetAnnounceNewSession(utilityGetSessionUDP, newSessionChan)

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

func TestUDPListenerIPv4(t *testing.T) {
	t.Run("Basic message exchange", func(t *testing.T) {
		tListener, newSessionChan, _ := setupUDPListener(t, udpHost, udpPort,
			WithMaxLength(1024),
			WithBufferSize(100),
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		defer tListener.StopListener()

		// Connect client
		conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", udpHost, udpPort))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Test message exchange
		want := []byte("Hello World!")
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

		// Clean up
		if err := tListener.StopListener(); err != nil {
			t.Fatal("stop:", err)
		}
	})

	t.Run("Multiple sessions", func(t *testing.T) {
		tListener, newSessionChan, _ := setupUDPListener(t, udpHost, udpPort+3,
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		defer tListener.StopListener()

		// Create multiple clients
		numClients := 3
		conns := make([]net.Conn, numClients)
		for i := 0; i < numClients; i++ {
			conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", udpHost, udpPort+3))
			if err != nil {
				t.Fatal(err)
			}
			defer conn.Close()
			conns[i] = conn

			// Send message to create session
			msg := []byte(fmt.Sprintf("hello from client %d", i))
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

		if err := tListener.StopListener(); err != nil {
			t.Fatal("stop:", err)
		}
	})

	t.Run("Invalid session type handling", func(t *testing.T) {
		tListener, newSessionChan, _ := setupUDPListener(t, udpHost, udpPort+4,
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		defer tListener.StopListener()

		// Connect client
		conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", udpHost, udpPort+4))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Send message to create session
		if _, err := conn.Write([]byte("hello")); err != nil {
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
		if _, ok := session.(*UDPSession); !ok {
			t.Fatal("session is not of type *UDPSession")
		}

		// Read initial message
		select {
		case <-session.Data():
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for message")
		}
	})
}

func TestUDPListenerIPv6(t *testing.T) {
	t.Run("Basic IPv6 message exchange", func(t *testing.T) {
		tListener, newSessionChan, _ := setupUDPListener(t, udpHostV6, udpPortV6,
			WithMaxLength(1024),
			WithBufferSize(100),
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		defer tListener.StopListener()

		// Connect client
		conn, err := net.Dial("udp6", fmt.Sprintf("%s:%d", udpHostV6, udpPortV6))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Test message exchange
		want := []byte("Hello IPv6 World!")
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

		// Clean up
		if err := tListener.StopListener(); err != nil {
			t.Fatal("stop:", err)
		}
	})
}

func utilityGetSessionUDP(options any, session Session) {
	options.(chan Session) <- session
}
