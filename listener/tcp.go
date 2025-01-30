package listener

import (
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"golang.org/x/net/context"

	"github.com/google/uuid"
)

// TCPServer implements a TCP streaming server with enhanced session management
type TCPServer struct {
	conn                   net.Listener
	addr                   string
	announceMiddleware     AnnounceMiddlewareFunc
	announceMiddlewareOpts any
	sessions               map[string]Session
	sessionsMutex          sync.RWMutex
	wg                     sync.WaitGroup
	ctx                    context.Context
	cancel                 context.CancelFunc
	*ServerConfig
}

// TCPSession represents an active TCP connection with enhanced management capabilities
type TCPSession struct {
	*BaseSession
	server *TCPServer
	conn   net.Conn
}

// GetLastRecieved maintains backward compatibility with the Session interface
func (s *TCPSession) GetLastRecieved() time.Time {
	return s.GetLastReceived()
}

// receiveBytes implements the Session interface
func (s *TCPSession) receiveBytes(data ...[]byte) {
	// TCP doesn't use the data parameter as it reads directly from the connection
	// This method exists only to satisfy the Session interface
}

// NewTCP creates a new TCP server with the given configuration
func NewTCP(host string, port uint16, ctx context.Context, opts ...ServerOption) (*TCPServer, error) {
	config := defaultConfig()
	for _, opt := range opts {
		opt(config)
	}

	if err := ValidateConfig(config); err != nil {
		return nil, NewConfigError("invalid configuration", err)
	}

	addr := fmt.Sprintf("%v:%v", host, port)
	serverCtx, cancel := context.WithCancel(ctx)
	server := &TCPServer{
		addr:         addr,
		ServerConfig: config,
		sessions:     make(map[string]Session),
		ctx:          serverCtx,
		cancel:       cancel,
	}

	return server, nil
}

// StartListener begins accepting TCP connections
func (t *TCPServer) StartListener() error {
	conn, err := net.Listen("tcp", t.addr)
	if err != nil {
		return NewConnectionError("failed to start listener", err)
	}
	t.conn = conn
	t.Logger.Info("TCP server listening on %s", t.addr)

	go t.receiveStream()
	return nil
}

// StopListener gracefully shuts down the TCP server
func (t *TCPServer) StopListener() error {
	t.Logger.Info("Shutting down TCP server")

	// Cancel context to stop all goroutines
	t.cancel()

	// Close all active sessions
	t.sessionsMutex.Lock()
	for _, session := range t.sessions {
		session.CloseSession()
	}
	t.sessionsMutex.Unlock()

	// Close listener
	if err := t.conn.Close(); err != nil {
		return NewConnectionError("failed to close listener", err)
	}

	// Wait for all goroutines to finish
	t.wg.Wait()
	return nil
}

// receiveStream handles incoming TCP connections
func (t *TCPServer) receiveStream() {
	t.wg.Add(1)
	defer t.wg.Done()

	acceptChan := make(chan net.Conn)
	errChan := make(chan error)

	// Accept connections in a separate goroutine
	go func() {
		for {
			select {
			case <-t.ctx.Done():
				return
			default:
				// Set accept deadline to allow context cancellation
				t.conn.(*net.TCPListener).SetDeadline(time.Now().Add(time.Second))
				conn, err := t.conn.Accept()
				if err != nil {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						continue // Deadline exceeded, try again
					}
					errChan <- err
					return
				}
				acceptChan <- conn
			}
		}
	}()

	for {
		select {
		case <-t.ctx.Done():
			t.Logger.Info("Stopping TCP receiver")
			return
		case err := <-errChan:
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				t.Logger.Warn("Temporary error accepting connection: %v", err)
				time.Sleep(time.Second) // Basic retry backoff
				continue
			}
			t.Logger.Error("Error accepting connection: %v", err)
			return
		case conn := <-acceptChan:
			// Check max connections
			t.sessionsMutex.RLock()
			if len(t.sessions) >= t.MaxConnections {
				t.sessionsMutex.RUnlock()
				t.Logger.Warn("Max connections reached, rejecting connection from %s", conn.RemoteAddr())
				conn.Close()
				continue
			}
			t.sessionsMutex.RUnlock()

			// Set connection timeouts
			conn.SetReadDeadline(time.Now().Add(t.ReadTimeout))
			conn.SetWriteDeadline(time.Now().Add(t.WriteTimeout))

			clientAddr := conn.RemoteAddr()
			t.Logger.Debug("New connection from %s", clientAddr)

			session := t.newSession(clientAddr, conn)
			go t.handleSession(session)
		}
	}
}

// newSession creates a new TCP session
func (t *TCPServer) newSession(addr net.Addr, conn net.Conn) *TCPSession {
	base := NewBaseSession(addr, context.Background(), t.Logger)
	base.ID = uuid.NewString()

	session := &TCPSession{
		BaseSession: base,
		server:      t,
		conn:        conn,
	}

	t.sessionsMutex.Lock()
	t.sessions[addr.String()] = session
	t.sessionsMutex.Unlock()

	if t.announceMiddleware != nil {
		t.announceMiddleware(t.announceMiddlewareOpts, session)
	}

	return session
}

func (t *TCPServer) SetAnnounceNewSession(function AnnounceMiddlewareFunc, options any) {
	t.announceMiddleware = function
	t.announceMiddlewareOpts = options
}

func (t *TCPServer) GetActiveSessions() map[string]Session {
	return t.sessions
}

func (t *TCPServer) GetSession(ClientAddr string) Session {
	t.sessionsMutex.RLock()
	defer t.sessionsMutex.RUnlock()
	return t.sessions[ClientAddr]
}

// SendToClient sends data to the TCP client with length prefix
func (s *TCPSession) SendToClient(data []byte) error {
	if len(data) > int(s.server.MaxLength) {
		return NewProtocolError("message exceeds maximum length", nil)
	}

	// Set write deadline
	if err := s.conn.SetWriteDeadline(time.Now().Add(s.server.WriteTimeout)); err != nil {
		return NewConnectionError("failed to set write deadline", err)
	}

	// Write length prefix
	length := uint32(len(data))
	if err := binary.Write(s.conn, binary.BigEndian, length); err != nil {
		return NewConnectionError("failed to write message length", err)
	}

	// Write data
	if _, err := s.conn.Write(data); err != nil {
		return NewConnectionError("failed to write message data", err)
	}

	return nil
}

// handleSession processes incoming data for a TCP session
func (t *TCPServer) handleSession(session *TCPSession) {
	t.wg.Add(1)
	defer t.wg.Done()
	defer session.CloseSession()

	for {
		// Set read deadline
		if err := session.conn.SetReadDeadline(time.Now().Add(t.ReadTimeout)); err != nil {
			t.Logger.Error("Failed to set read deadline: %v", err)
			return
		}

		// Read message length
		var length uint32
		if err := binary.Read(session.conn, binary.BigEndian, &length); err != nil {
			if err == io.EOF {
				t.Logger.Debug("Connection closed by client: %s", session.GetClientAddr())
				return
			}
			t.Logger.Error("Error reading message length: %v", err)
			return
		}

		// Validate message length
		if length > t.MaxLength {
			t.Logger.Warn("Message exceeds maximum length from %s", session.GetClientAddr())
			continue
		}

		// Read message data
		buffer := make([]byte, length)
		if _, err := io.ReadFull(session.conn, buffer); err != nil {
			t.Logger.Error("Error reading message data: %v", err)
			return
		}

		// Update last received time and send to channel
		session.updateLastReceived()
		session.DataChannel <- buffer
	}
}

// CloseSession closes the TCP session and cleans up resources
func (s *TCPSession) CloseSession() {
	s.server.sessionsMutex.Lock()
	defer s.server.sessionsMutex.Unlock()

	// Check if session is already closed
	if _, exists := s.server.sessions[s.GetClientAddr().String()]; !exists {
		return
	}

	s.Cancel() // Cancel context from base session
	s.conn.Close()
	close(s.DataChannel)
	delete(s.server.sessions, s.GetClientAddr().String())

	s.server.Logger.Debug("Closed session for %s", s.GetClientAddr())
}
