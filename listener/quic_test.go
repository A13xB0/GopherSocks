package listener

import (
	"bytes"
	"context"
	crand "crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"math/rand"
	"net"
	"reflect"
	"testing"
	"time"

	"github.com/quic-go/quic-go"
)

// mockAddr implements net.Addr interface for testing
type mockAddr struct {
	addr string
}

// setupQUICListener creates and starts a QUIC listener with the given options
func setupQUICListener(t *testing.T, host string, port uint16, opts ...ServerOption) (Listener, chan Session, chan error) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	t.Cleanup(cancel)

	tlsConfig := generateTestTLSConfig()

	// Create server
	tListener, err := NewQUIC(host, port, tlsConfig, ctx, opts...)
	if err != nil {
		t.Fatal(err)
	}

	// Set up session announcement
	newSessionChan := make(chan Session)
	tListener.SetAnnounceNewSession(utilityGetSessionQUIC, newSessionChan)

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

func utilityGetSessionQUIC(options any, session Session) {
	options.(chan Session) <- session
}

func TestQUICServer_RealSession(t *testing.T) {
	t.Run("Basic message exchange", func(t *testing.T) {
		tListener, newSessionChan, _ := setupQUICListener(t, "localhost", 0,
			WithMaxLength(1024),
			WithBufferSize(100),
			WithTimeouts(100*time.Millisecond, 100*time.Millisecond),
		)
		defer tListener.StopListener()

		// Get the actual server address
		addr := tListener.(*QUICServer).listener.Addr().String()
		t.Logf("Server listening on %s", addr)

		// Create client TLS config
		clientTLSConfig := &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"quic-test"},
		}

		// Connect client
		conn, err := quic.DialAddr(context.Background(), addr, clientTLSConfig, &quic.Config{})
		if err != nil {
			t.Fatal(err)
		}
		defer conn.CloseWithError(0, "test complete")

		// Open a stream
		stream, err := conn.OpenStreamSync(context.Background())
		if err != nil {
			t.Fatal(err)
		}
		defer stream.Close()

		// Test message exchange
		want := []byte("Hello QUIC World!")
		if _, err := stream.Write(want); err != nil {
			t.Fatal(err)
		}

		// Get session and verify received message
		var session Session
		select {
		case session = <-newSessionChan:
			t.Log("Session created successfully")
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for session")
		}

		select {
		case got := <-session.Data():
			if !reflect.DeepEqual(want, got) {
				t.Fatalf("want: %s, got: %s", want, got)
			}
		case <-time.After(1 * time.Second):
			t.Fatal("timeout waiting for message")
		}

		// Test echo
		if err := session.SendToClient(want); err != nil {
			t.Fatal("echo:", err)
		}
	})

	t.Run("Multiple sessions with random data", func(t *testing.T) {
		// Create a context with timeout for the entire test
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		tListener, newSessionChan, _ := setupQUICListener(t, "localhost", 0,
			WithMaxLength(1024*1024), // 1MB max for large random packets
			WithBufferSize(1000),     // Large buffer for concurrent operations
			WithTimeouts(time.Second, time.Second),
		)
		defer tListener.StopListener()

		addr := tListener.(*QUICServer).listener.Addr().String()
		t.Logf("Server listening on %s", addr)

		clientTLSConfig := &tls.Config{
			InsecureSkipVerify: true,
			NextProtos:         []string{"quic-test"},
		}

		// Test parameters (minimal for faster testing)
		numClients := 2
		packetsPerClient := 5
		maxPacketSize := 100

		// Create and store sessions
		sessions := make([]Session, numClients)
		streams := make([]quic.Stream, numClients)
		conns := make([]quic.Connection, numClients)

		// Cleanup function to ensure all connections are closed
		defer func() {
			for i := range conns {
				if conns[i] != nil {
					conns[i].CloseWithError(0, "test complete")
				}
			}
			for i := range streams {
				if streams[i] != nil {
					streams[i].Close()
				}
			}
		}()

		// First establish all connections sequentially
		for i := 0; i < numClients; i++ {
			// Connect client with timeout
			connCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			conn, err := quic.DialAddr(connCtx, addr, clientTLSConfig, &quic.Config{})
			cancel()
			if err != nil {
				t.Fatalf("Failed to connect client %d: %v", i, err)
			}
			conns[i] = conn

			// Open stream with timeout
			streamCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
			stream, err := conn.OpenStreamSync(streamCtx)
			cancel()
			if err != nil {
				t.Fatalf("Failed to open stream for client %d: %v", i, err)
			}
			streams[i] = stream

			// Send initial message to establish session
			msg := []byte(fmt.Sprintf("init from client %d", i))
			if _, err := stream.Write(msg); err != nil {
				t.Fatalf("Failed to write init message for client %d: %v", i, err)
			}

			// Wait for session
			select {
			case session := <-newSessionChan:
				t.Logf("Session %d created", i+1)
				sessions[i] = session
				// Read initial message
				select {
				case <-session.Data():
					t.Logf("Initial message received from client %d", i+1)
				case <-time.After(1 * time.Second):
					t.Fatalf("Timeout waiting for initial message from client %d", i+1)
				}
			case <-time.After(1 * time.Second):
				t.Fatalf("Timeout waiting for session %d", i+1)
			}
		}

		// Now send data from each client sequentially
		for i := 0; i < numClients; i++ {
			for j := 0; j < packetsPerClient; j++ {
				// Generate random data
				size := rand.Intn(maxPacketSize) + 1
				data := make([]byte, size)
				if _, err := crand.Read(data); err != nil {
					t.Fatalf("Failed to generate random data: %v", err)
				}

				// Send data
				if _, err := streams[i].Write(data); err != nil {
					t.Fatalf("Failed to write data: %v", err)
				}

				// Wait for data to be received
				select {
				case received := <-sessions[i].Data():
					if !bytes.Equal(data, received) {
						t.Errorf("Data mismatch for client %d packet %d", i, j)
					}
				case <-time.After(1 * time.Second):
					t.Fatalf("Timeout waiting for data from client %d packet %d", i, j)
				}
			}
		}

		t.Log("All data transmitted and verified successfully")
	})
}

var _ net.Addr = (*mockAddr)(nil) // Verify mockAddr implements net.Addr

func (m *mockAddr) Network() string { return "mock" }
func (m *mockAddr) String() string  { return m.addr }

func generateTestTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(crand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(crand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-test"},
	}
}

func TestNewQUIC(t *testing.T) {
	tlsConfig := generateTestTLSConfig()
	ctx := context.Background()

	tests := []struct {
		name    string
		host    string
		port    uint16
		tls     *tls.Config
		wantErr bool
	}{
		{
			name:    "Valid configuration",
			host:    "localhost",
			port:    0,
			tls:     tlsConfig,
			wantErr: false,
		},
		{
			name:    "Missing TLS config",
			host:    "localhost",
			port:    0,
			tls:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewQUIC(tt.host, tt.port, tt.tls, ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewQUIC() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestQUICServer_StartStopListener(t *testing.T) {
	tlsConfig := generateTestTLSConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, err := NewQUIC("localhost", 0, tlsConfig, ctx)
	if err != nil {
		t.Fatalf("Failed to create QUIC server: %v", err)
	}

	// Test StartListener
	if err := server.StartListener(); err != nil {
		t.Errorf("StartListener() error = %v", err)
	}

	// Test StopListener
	if err := server.StopListener(); err != nil {
		t.Errorf("StopListener() error = %v", err)
	}
}

func TestQUICServer_SessionManagement(t *testing.T) {
	tlsConfig := generateTestTLSConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	listener, err := NewQUIC("localhost", 0, tlsConfig, ctx)
	if err != nil {
		t.Fatalf("Failed to create QUIC server: %v", err)
	}
	server := listener.(*QUICServer)

	// Test initial state
	if sessions := server.GetActiveSessions(); len(sessions) != 0 {
		t.Errorf("Expected 0 active sessions initially, got %d", len(sessions))
	}

	// Add multiple mock sessions directly to the sessions map
	mockAddrs := []string{"192.168.1.1:1234", "192.168.1.2:1234", "192.168.1.3:1234"}
	expectedSessions := make(map[string]Session)

	func() {
		server.sessionsMutex.Lock()
		defer server.sessionsMutex.Unlock()
		for _, addr := range mockAddrs {
			mockAddr := &mockAddr{addr: addr}
			base := NewBaseSession(mockAddr, ctx, server.Logger, server.ServerConfig)
			base.ID = addr // Using addr as ID for easy testing
			session := &QUICSession{
				BaseSession: base,
				server:      server,
			}
			server.sessions[addr] = session
			expectedSessions[addr] = session
		}
	}()

	// Test GetActiveSessions with multiple sessions
	activeSessions := server.GetActiveSessions()
	if len(activeSessions) != len(mockAddrs) {
		t.Errorf("Expected %d active sessions, got %d", len(mockAddrs), len(activeSessions))
	}

	// Test getting specific sessions
	for _, addr := range mockAddrs {
		session := server.GetSession(addr)
		if session == nil {
			t.Errorf("Expected to find session for %s, got nil", addr)
		}
		if session.GetSessionID() != addr {
			t.Errorf("Expected session ID %s, got %s", addr, session.GetSessionID())
		}
	}

	// Test getting non-existent session
	if session := server.GetSession("non-existent"); session != nil {
		t.Error("Expected nil for non-existent session")
	}

	// Verify all expected sessions are present
	for addr, expectedSession := range expectedSessions {
		actualSession := server.GetSession(addr)
		if actualSession != expectedSession {
			t.Errorf("Session mismatch for %s", addr)
		}
	}
}
