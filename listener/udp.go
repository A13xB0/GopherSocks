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

	// Close listener to unblock any reads
	if err := u.conn.Close(); err != nil {
		u.Logger.Error("Error closing listener: %v", err)
	}

	// Wait for all goroutines to finish
	u.wg.Wait()

	// Close all active sessions after goroutines are done
	u.sessionsMutex.Lock()
	for _, session := range u.sessions {
		s := session.(*UDPSession)
		close(s.DataChannel)
		delete(u.sessions, s.GetClientAddr().String())
	}
	u.sessionsMutex.Unlock()

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
		addr net.Addr
		data []byte
	}, u.BufferSize)
	errChan := make(chan error, 1)

	// Read packets in a separate goroutine
	go func() {
		defer close(readChan)
		defer close(errChan)

		buffer := make([]byte, u.BufferSize)
		for {
			select {
			case <-u.ctx.Done():
				return
			default:
				// Set read deadline to allow context cancellation
				u.conn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				n, addr, err := u.conn.ReadFrom(buffer)
				if err != nil {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						continue // Deadline exceeded, try again
					}
					if ne, ok := err.(net.Error); ok && ne.Temporary() {
						u.Logger.Warn("Temporary error reading packet: %v", err)
						time.Sleep(100 * time.Millisecond) // Basic retry backoff
						continue
					}
					select {
					case errChan <- err:
					case <-u.ctx.Done():
					}
					return
				}

				// Make a copy of the data since buffer will be reused
				data := make([]byte, n)
				copy(data, buffer[:n])

				select {
				case readChan <- struct {
					addr net.Addr
					data []byte
				}{addr, data}:
				case <-u.ctx.Done():
					return
				}
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
				time.Sleep(100 * time.Millisecond) // Basic retry backoff
				continue
			}
			u.Logger.Error("Error reading packet: %v", err)
			return
		case read, ok := <-readChan:
			if !ok {
				return // Channel closed
			}

			clientAddrStr := read.addr.String()
			var session Session
			var exists bool

			// Check if session exists
			u.sessionsMutex.RLock()
			session, exists = u.sessions[clientAddrStr]
			u.sessionsMutex.RUnlock()

			if !exists {
				// Check max connections before creating new session
				u.sessionsMutex.Lock()
				if len(u.sessions) >= u.MaxConnections {
					u.sessionsMutex.Unlock()
					u.Logger.Warn("Max connections reached, rejecting packet from %s", read.addr)
					continue
				}

				// Create new session under write lock
				session = u.newSession(read.addr)
				u.sessionsMutex.Unlock()
			}

			// Check session timeout
			if exists && time.Since(session.GetLastRecieved()) > u.ReadTimeout {
				u.Logger.Debug("Session timeout for %s", read.addr)
				session.CloseSession()
				// Create new session since old one timed out
				u.sessionsMutex.Lock()
				session = u.newSession(read.addr)
				u.sessionsMutex.Unlock()
			}

			// Validate message length
			if len(read.data) > int(u.MaxLength) {
				u.Logger.Warn("Message exceeds maximum length from %s", read.addr)
				session.CloseSession()
				continue
			}

			// Process the received data with non-blocking send
			select {
			case session.Data() <- read.data:
				session.(*UDPSession).updateLastReceived()
			case <-u.ctx.Done():
				return
			default:
				u.Logger.Warn("Channel full, dropping packet from %s", read.addr)
			}
		}
	}
}

// newSession creates a new UDP session
func (u *UDPServer) newSession(addr net.Addr) *UDPSession {
	base := NewBaseSession(addr, u.ctx, u.Logger) // Use server context
	base.ID = uuid.NewString()

	session := &UDPSession{
		BaseSession: base,
		server:      u,
	}

	u.sessions[addr.String()] = session

	if u.announceMiddleware != nil {
		u.announceMiddleware(u.announceMiddlewareOpts, session)
	}

	return session
}

// GetActiveSessions returns all active sessions
func (u *UDPServer) GetActiveSessions() map[string]Session {
	u.sessionsMutex.RLock()
	defer u.sessionsMutex.RUnlock()
	sessions := make(map[string]Session)
	for k, v := range u.sessions {
		sessions[k] = v
	}
	return sessions
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
		select {
		case s.DataChannel <- d:
		case <-s.server.ctx.Done():
			return
		default:
			s.server.Logger.Warn("Channel full, dropping packet")
		}
	}
}

// CloseSession closes the UDP session and cleans up resources
func (s *UDPSession) CloseSession() {
	s.server.closeSession(s)
}

// closeSession closes a UDP session and cleans up resources
func (u *UDPServer) closeSession(session *UDPSession) {
	u.sessionsMutex.Lock()
	defer u.sessionsMutex.Unlock()

	// Check if session is already closed
	if _, exists := u.sessions[session.GetClientAddr().String()]; !exists {
		return
	}

	session.Cancel() // Cancel context from base session

	select {
	case <-session.DataChannel:
		// Drain any remaining messages
	default:
	}
	close(session.DataChannel)

	delete(u.sessions, session.GetClientAddr().String())
	u.Logger.Debug("Closed session for %s", session.GetClientAddr())
}

// GetLastRecieved implements the Session interface
func (s *UDPSession) GetLastRecieved() time.Time {
	return s.BaseSession.GetLastReceived()
}
