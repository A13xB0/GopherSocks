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
	udpHost = "127.0.0.1"
	udpPort = 9001
)

func TestUDPListener(t *testing.T) {
	t.Run("Basic message exchange", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		// Create server with custom configuration
		tListener, err := NewUDP(udpHost, udpPort, ctx,
			WithMaxLength(1024),
			WithBufferSize(100),
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
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

	t.Run("Max message length", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		maxLength := uint32(10) // Very small max length for testing
		tListener, err := NewUDP(udpHost, udpPort+1, ctx,
			WithMaxLength(maxLength),
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		if err != nil {
			t.Fatal(err)
		}

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

		conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", udpHost, udpPort+1))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Send message larger than max length
		largeMsg := make([]byte, maxLength+1)
		if _, err := conn.Write(largeMsg); err != nil {
			t.Fatal(err)
		}

		// Message should be dropped, channel should be empty
		var session Session
		select {
		case session = <-newSessionChan:
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for session")
		}

		select {
		case <-session.Data():
			t.Fatal("received message larger than max length")
		case <-time.After(50 * time.Millisecond):
			// Expected timeout
		}

		if err := tListener.StopListener(); err != nil {
			t.Fatal("stop:", err)
		}
	})

	t.Run("Session timeout", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		tListener, err := NewUDP(udpHost, udpPort+2, ctx,
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		if err != nil {
			t.Fatal(err)
		}

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

		// Create a session
		conn, err := net.Dial("udp", fmt.Sprintf("%s:%d", udpHost, udpPort+2))
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()

		// Send initial message
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

		// Read initial message
		select {
		case <-session.Data():
		case <-time.After(100 * time.Millisecond):
			t.Fatal("timeout waiting for message")
		}

		// Wait for session timeout
		time.Sleep(200 * time.Millisecond)

		// Verify session was cleaned up
		if len(tListener.GetActiveSessions()) != 0 {
			t.Fatal("session not cleaned up after timeout")
		}

		if err := tListener.StopListener(); err != nil {
			t.Fatal("stop:", err)
		}
	})

	t.Run("Multiple sessions", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		tListener, err := NewUDP(udpHost, udpPort+3, ctx,
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		if err != nil {
			t.Fatal(err)
		}

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
}

func utilityGetSessionUDP(options any, session Session) {
	options.(chan Session) <- session
}
