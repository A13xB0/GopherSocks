package listener

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

// UDPServer implements a UDP streaming server with enhanced session management
type UDPServer struct {
	conn                   net.PacketConn
	addr                   string
	sessions               map[string]Session
	sessionsMutex          sync.RWMutex
	announceMiddleware     AnnounceMiddlewareFunc
	announceMiddlewareOpts any
	wg                     sync.WaitGroup
	ctx                    context.Context
	cancel                 context.CancelFunc
	*ServerConfig
}

// UDPSession represents an active UDP connection
type UDPSession struct {
	*BaseSession
	server *UDPServer
}

// NewUDP creates a new UDP server with the given configuration
func NewUDP(host string, port uint16, ctx context.Context, opts ...ServerOption) (*UDPServer, error) {
	config := defaultConfig()
	for _, opt := range opts {
		opt(config)
	}

	if err := ValidateConfig(config); err != nil {
		return nil, NewConfigError("invalid configuration", err)
	}

	addr := fmt.Sprintf("%v:%v", host, port)
	serverCtx, cancel := context.WithCancel(ctx)
	server := &UDPServer{
		addr:         addr,
		ServerConfig: config,
		sessions:     make(map[string]Session),
		ctx:          serverCtx,
		cancel:       cancel,
	}

	return server, nil
}

// StartListener begins accepting UDP packets
func (u *UDPServer) StartListener() error {
	conn, err := net.ListenPacket("udp", u.addr)
	if err != nil {
		return NewConnectionError("failed to start listener", err)
	}
	u.conn = conn
	u.Logger.Info("UDP server listening on %s", u.addr)

	go u.receiveStream()
	return nil
}

// StopListener gracefully shuts down the UDP server
func (u *UDPServer) StopListener() error {
	u.Logger.Info("Shutting down UDP server")

	// Cancel context to stop all goroutines
	u.cancel()

	// Close all active sessions
	u.sessionsMutex.Lock()
	for _, session := range u.sessions {
		session.CloseSession()
	}
	u.sessionsMutex.Unlock()

	// Close listener
	if err := u.conn.Close(); err != nil {
		return NewConnectionError("failed to close listener", err)
	}

	// Wait for all goroutines to finish
	u.wg.Wait()
	return nil
}

// SetAnnounceNewSession sets the middleware for announcing new sessions
func (u *UDPServer) SetAnnounceNewSession(function AnnounceMiddlewareFunc, options any) {
	u.announceMiddleware = function
	u.announceMiddlewareOpts = options
}

// receiveStream handles incoming UDP packets
func (u *UDPServer) receiveStream() {
	u.wg.Add(1)
	defer u.wg.Done()

	readChan := make(chan struct {
		n    int
		addr net.Addr
		data []byte
	})
	errChan := make(chan error)

	// Read packets in a separate goroutine
	go func() {
		buffer := make([]byte, u.BufferSize)
		for {
			select {
			case <-u.ctx.Done():
				return
			default:
				// Set read deadline to allow context cancellation
				u.conn.SetReadDeadline(time.Now().Add(time.Second))
				n, addr, err := u.conn.ReadFrom(buffer)
				if err != nil {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						continue // Deadline exceeded, try again
					}
					errChan <- err
					return
				}
				// Make a copy of the data since buffer will be reused
				data := make([]byte, n)
				copy(data, buffer[:n])
				readChan <- struct {
					n    int
					addr net.Addr
					data []byte
				}{n, addr, data}
			}
		}
	}()

	for {
		select {
		case <-u.ctx.Done():
			u.Logger.Info("Stopping UDP receiver")
			return
		case err := <-errChan:
			if ne, ok := err.(net.Error); ok && ne.Temporary() {
				u.Logger.Warn("Temporary error reading packet: %v", err)
				time.Sleep(time.Second) // Basic retry backoff
				continue
			}
			u.Logger.Error("Error reading packet: %v", err)
			return
		case read := <-readChan:
			clientAddrStr := read.addr.String()
			var session Session
			var ok bool

			// Check if session exists
			u.sessionsMutex.RLock()
			session, ok = u.sessions[clientAddrStr]
			u.sessionsMutex.RUnlock()

			if !ok {
				// Check max connections before creating new session
				u.sessionsMutex.RLock()
				if len(u.sessions) >= u.MaxConnections {
					u.sessionsMutex.RUnlock()
					u.Logger.Warn("Max connections reached, rejecting packet from %s", read.addr)
					continue
				}
				u.sessionsMutex.RUnlock()

				session = u.newSession(read.addr)
			}

			// Process the received data
			session.receiveBytes(read.data)
		}
	}
}

// newSession creates a new UDP session
func (u *UDPServer) newSession(addr net.Addr) *UDPSession {
	base := NewBaseSession(addr, context.Background(), u.Logger)
	base.ID = uuid.NewString()

	session := &UDPSession{
		BaseSession: base,
		server:      u,
	}

	u.sessionsMutex.Lock()
	u.sessions[addr.String()] = session
	u.sessionsMutex.Unlock()

	if u.announceMiddleware != nil {
		u.announceMiddleware(u.announceMiddlewareOpts, session)
	}

	return session
}

// GetActiveSessions returns all active sessions
func (u *UDPServer) GetActiveSessions() map[string]Session {
	return u.sessions
}

// GetSession returns a specific session by client address
func (u *UDPServer) GetSession(ClientAddr string) Session {
	u.sessionsMutex.RLock()
	defer u.sessionsMutex.RUnlock()
	return u.sessions[ClientAddr]
}

// SendToClient sends data to the UDP client
func (s *UDPSession) SendToClient(data []byte) error {
	if len(data) > int(s.server.MaxLength) {
		return NewProtocolError("message exceeds maximum length", nil)
	}

	if _, err := s.server.conn.WriteTo(data, s.GetClientAddr()); err != nil {
		return NewConnectionError("failed to send data", err)
	}

	return nil
}

// receiveBytes processes received UDP packets
func (s *UDPSession) receiveBytes(data ...[]byte) {
	s.updateLastReceived()
	for _, d := range data {
		s.DataChannel <- d
	}
}

// CloseSession closes the UDP session and cleans up resources
func (s *UDPSession) CloseSession() {
	s.server.sessionsMutex.Lock()
	defer s.server.sessionsMutex.Unlock()

	// Check if session is already closed
	if _, exists := s.server.sessions[s.GetClientAddr().String()]; !exists {
		return
	}

	s.Cancel() // Cancel context from base session
	close(s.DataChannel)
	delete(s.server.sessions, s.GetClientAddr().String())

	s.server.Logger.Debug("Closed session for %s", s.GetClientAddr())
}

// GetLastRecieved maintains backward compatibility with the Session interface
func (s *UDPSession) GetLastRecieved() time.Time {
	return s.GetLastReceived()
}
