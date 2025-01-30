package listener

import (
	"fmt"
	"net/url"
	"reflect"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"golang.org/x/net/context"
)

const (
	wsHost = "127.0.0.1"
	wsPort = 9002
)

func TestWSListener(t *testing.T) {
	t.Run("Basic message exchange", func(t *testing.T) {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		// Create server with custom configuration
		wsConfig := &WebSocketConfig{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			Path:            "/ws",
		}

		config := defaultConfig()
		config.ProtocolConfig = wsConfig
		config.MaxLength = 1024 * 1024 // 1MB
		config.BufferSize = 100

		tListener, err := NewWebSocket(wsHost, wsPort, ctx,
			WithMaxLength(config.MaxLength),
			WithBufferSize(config.BufferSize),
			WithTimeouts(time.Second, time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}

		// Set up session announcement
		newSessionChan := make(chan Session)
		tListener.SetAnnounceNewSession(utilityGetSessionWS, newSessionChan)

		// Start server
		go func() {
			if err := tListener.StartListener(); err != nil {
				t.Errorf("StartListener error: %v", err)
			}
		}()

		// Wait for server to start
		time.Sleep(time.Second)

		// Connect client
		u := url.URL{
			Scheme: "ws",
			Host:   fmt.Sprintf("%s:%d", wsHost, wsPort),
			Path:   wsConfig.Path,
		}
		c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			t.Fatal("dial:", err)
		}
		defer c.Close()

		// Test message exchange
		want := []byte("Hello World!")
		if err := c.WriteMessage(websocket.BinaryMessage, want); err != nil {
			t.Fatal("write:", err)
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
		_, got, err := c.ReadMessage()
		if err != nil {
			t.Fatal("read:", err)
		}
		if !reflect.DeepEqual(want, got) {
			t.Fatalf("echo want: %s, got: %s", want, got)
		}

		// Test session management
		if len(tListener.GetActiveSessions()) != 1 {
			t.Fatal("expected 1 active session")
		}

		// Test session cleanup
		c.Close()
		time.Sleep(100 * time.Millisecond) // Give server time to clean up
		if len(tListener.GetActiveSessions()) != 0 {
			t.Fatal("expected 0 active sessions after close")
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
		tListener, err := NewWebSocket(wsHost, wsPort+1, ctx,
			WithMaxLength(maxLength),
			WithTimeouts(time.Second, time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}

		newSessionChan := make(chan Session)
		tListener.SetAnnounceNewSession(utilityGetSessionWS, newSessionChan)

		go func() {
			if err := tListener.StartListener(); err != nil {
				t.Errorf("StartListener error: %v", err)
			}
		}()

		// Wait for server to start
		time.Sleep(time.Second)

		u := url.URL{
			Scheme: "ws",
			Host:   fmt.Sprintf("%s:%d", wsHost, wsPort+1),
			Path:   "/ws",
		}
		c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
		if err != nil {
			t.Fatal("dial:", err)
		}
		defer c.Close()

		// Send message larger than max length
		largeMsg := make([]byte, maxLength+1)
		if err := c.WriteMessage(websocket.BinaryMessage, largeMsg); err != nil {
			t.Fatal("write:", err)
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
		tListener, err := NewWebSocket(wsHost, wsPort+2, ctx,
			WithMaxConnections(maxConns),
			WithTimeouts(time.Second, time.Second),
		)
		if err != nil {
			t.Fatal(err)
		}

		newSessionChan := make(chan Session)
		tListener.SetAnnounceNewSession(utilityGetSessionWS, newSessionChan)

		go func() {
			if err := tListener.StartListener(); err != nil {
				t.Errorf("StartListener error: %v", err)
			}
		}()

		// Wait for server to start
		time.Sleep(time.Second)

		// Create maxConns connections
		conns := make([]*websocket.Conn, maxConns)
		for i := 0; i < maxConns; i++ {
			u := url.URL{
				Scheme: "ws",
				Host:   fmt.Sprintf("%s:%d", wsHost, wsPort+2),
				Path:   "/ws",
			}
			c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
			if err != nil {
				t.Fatal("dial:", err)
			}
			defer c.Close()
			conns[i] = c
			<-newSessionChan // Wait for session announcement
		}

		// Try to create one more connection
		u := url.URL{
			Scheme: "ws",
			Host:   fmt.Sprintf("%s:%d", wsHost, wsPort+2),
			Path:   "/ws",
		}
		_, _, err = websocket.DefaultDialer.Dial(u.String(), nil)
		if err == nil {
			t.Fatal("expected connection to be rejected")
		}

		if err := tListener.StopListener(); err != nil {
			t.Fatal("stop:", err)
		}
	})
}

func utilityGetSessionWS(options any, session Session) {
	options.(chan Session) <- session
}
