package listener

import (
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
)

// QUICServer implements a QUIC streaming server with session management
type QUICServer struct {
	listener               *quic.Listener
	addr                   string
	announceMiddleware     AnnounceMiddlewareFunc
	announceMiddlewareOpts any
	sessions               map[string]Session
	sessionsMutex          sync.RWMutex
	wg                     sync.WaitGroup
	ctx                    context.Context
	cancel                 context.CancelFunc
	tlsConfig              *tls.Config
	*ServerConfig
}

// QUICSession represents an active QUIC connection
type QUICSession struct {
	*BaseSession
	server *QUICServer
	conn   quic.Connection
	stream quic.Stream
}

// GetLastRecieved maintains backward compatibility with the Session interface
func (s *QUICSession) GetLastRecieved() time.Time {
	return s.GetLastReceived()
}

// NewQUIC creates a new QUIC server with the given configuration
func NewQUIC(host string, port uint16, tlsConfig *tls.Config, ctx context.Context, opts ...ServerOption) (Listener, error) {
	if tlsConfig == nil {
		return nil, NewConfigError("TLS config is required for QUIC", nil)
	}

	config := defaultConfig()
	for _, opt := range opts {
		opt(config)
	}

	if err := ValidateConfig(config); err != nil {
		return nil, NewConfigError("invalid configuration", err)
	}

	addr := fmt.Sprintf("%v:%v", host, port)
	serverCtx, cancel := context.WithCancel(ctx)
	server := &QUICServer{
		addr:         addr,
		ServerConfig: config,
		sessions:     make(map[string]Session),
		ctx:          serverCtx,
		cancel:       cancel,
		tlsConfig:    tlsConfig,
	}

	return server, nil
}

// StartListener begins accepting QUIC connections
func (q *QUICServer) StartListener() error {
	q.Logger.Debug("Starting QUIC listener with config", "addr", q.addr, "tls", q.tlsConfig)
	listener, err := quic.ListenAddr(q.addr, q.tlsConfig, &quic.Config{
		KeepAlivePeriod:            30 * time.Second,
		MaxIdleTimeout:             30 * time.Second,
		MaxIncomingStreams:         1000,
		MaxStreamReceiveWindow:     5000,
		MaxConnectionReceiveWindow: 5000 * 1000,
	})
	if err != nil {
		q.Logger.Error("Failed to start QUIC listener", "error", err)
		return NewConnectionError("failed to start listener", err)
	}
	q.listener = listener
	q.Logger.Debug("QUIC server listening", "addr", q.addr, "tls_config", fmt.Sprintf("%+v", q.tlsConfig))

	go q.receiveStream()
	return nil
}

// StopListener gracefully shuts down the QUIC server
func (q *QUICServer) StopListener() error {
	q.Logger.Info("Shutting down QUIC server")

	// Close listener first to stop accepting new connections
	if q.listener != nil {
		if err := q.listener.Close(); err != nil {
			q.Logger.Error("Error closing listener: %v", err)
		}
	}

	// Close all active sessions before canceling context
	q.sessionsMutex.Lock()
	for _, session := range q.sessions {
		s := session.(*QUICSession)
		if s.stream != nil {
			s.stream.Close()
		}
		if s.conn != nil {
			s.conn.CloseWithError(0, "server shutdown")
		}
	}
	q.sessionsMutex.Unlock()

	// Cancel context to stop all goroutines
	q.cancel()

	// Wait for all goroutines to finish
	q.wg.Wait()

	// Clean up remaining sessions
	q.sessionsMutex.Lock()
	for addr := range q.sessions {
		delete(q.sessions, addr)
	}
	q.sessionsMutex.Unlock()

	return nil
}

// receiveStream handles incoming QUIC connections
func (q *QUICServer) receiveStream() {
	q.wg.Add(1)
	defer q.wg.Done()

	acceptChan := make(chan struct {
		conn   quic.Connection
		stream quic.Stream
	}, q.BufferSize)
	errChan := make(chan error, 1)

	go q.acceptConnections(acceptChan, errChan)

	for {
		select {
		case <-q.ctx.Done():
			q.Logger.Info("Stopping QUIC receiver")
			return
		case err := <-errChan:
			if err == context.Canceled {
				return
			}
			q.Logger.Error("Error accepting connection: %v", err)
			// Don't return on error, keep trying to accept new connections
			continue
		case accept, ok := <-acceptChan:
			if !ok {
				return // Channel closed
			}
			// Check max connections
			q.sessionsMutex.Lock()
			if len(q.sessions) >= q.MaxConnections {
				q.sessionsMutex.Unlock()
				q.Logger.Warn("Max connections reached, rejecting connection from %s", accept.conn.RemoteAddr())
				accept.conn.CloseWithError(0, "max connections reached")
				continue
			}

			// Create new session
			session := q.newSession(accept.conn.RemoteAddr(), accept.conn, accept.stream)
			q.Logger.Debug("Created new session", "session_id", session.GetSessionID(), "remote_addr", session.GetClientAddr())
			q.sessionsMutex.Unlock()

			go q.handleSession(session)
		}
	}
}

// acceptConnections accepts new QUIC connections in a separate goroutine
func (q *QUICServer) acceptConnections(acceptChan chan<- struct {
	conn   quic.Connection
	stream quic.Stream
}, errChan chan<- error) {
	defer close(acceptChan)
	defer close(errChan)

	for {
		select {
		case <-q.ctx.Done():
			return
		default:
			q.Logger.Debug("Waiting for QUIC connection")
			conn, err := q.listener.Accept(q.ctx)
			if err != nil {
				q.Logger.Error("Failed to accept QUIC connection", "error", err)
				select {
				case errChan <- err:
				case <-q.ctx.Done():
				}
				if err == context.Canceled {
					return
				}
				time.Sleep(100 * time.Millisecond) // Basic retry backoff
				continue
			}
			q.Logger.Debug("Accepted QUIC connection", "remote_addr", conn.RemoteAddr())

			q.Logger.Debug("Accepting QUIC stream")
			stream, err := conn.AcceptStream(q.ctx)
			if err != nil {
				q.Logger.Error("Failed to accept QUIC stream", "error", err, "connection", fmt.Sprintf("%+v", conn))
				conn.CloseWithError(0, "failed to accept stream")
				continue
			}
			q.Logger.Debug("Accepted QUIC stream", "stream_id", stream.StreamID())

			select {
			case acceptChan <- struct {
				conn   quic.Connection
				stream quic.Stream
			}{conn, stream}:
			case <-q.ctx.Done():
				conn.CloseWithError(0, "server shutdown")
				return
			}
		}
	}
}

// newSession creates a new QUIC session
func (q *QUICServer) newSession(addr net.Addr, conn quic.Connection, stream quic.Stream) *QUICSession {
	base := NewBaseSession(addr, q.ctx, q.Logger, q.ServerConfig)
	base.ID = uuid.NewString()

	session := &QUICSession{
		BaseSession: base,
		server:      q,
		conn:        conn,
		stream:      stream,
	}

	q.sessions[addr.String()] = session

	if q.announceMiddleware != nil {
		q.announceMiddleware(q.announceMiddlewareOpts, session)
	}

	return session
}

func (q *QUICServer) SetAnnounceNewSession(function AnnounceMiddlewareFunc, options any) {
	q.announceMiddleware = function
	q.announceMiddlewareOpts = options
}

func (q *QUICServer) GetActiveSessions() map[string]Session {
	q.sessionsMutex.RLock()
	defer q.sessionsMutex.RUnlock()
	sessions := make(map[string]Session)
	for k, v := range q.sessions {
		sessions[k] = v
	}
	return sessions
}

func (q *QUICServer) GetSession(ClientAddr string) Session {
	q.sessionsMutex.RLock()
	defer q.sessionsMutex.RUnlock()
	return q.sessions[ClientAddr]
}

// SendToClient sends data to the QUIC client
func (s *QUICSession) SendToClient(data []byte) error {
	if len(data) > int(s.server.MaxLength) {
		return NewProtocolError("message exceeds maximum length", nil)
	}

	s.server.Logger.Debug("Sending data to client", "bytes", len(data), "remote_addr", s.GetClientAddr())

	// Write length prefix and data
	n, err := s.stream.Write(data)
	if err != nil {
		s.server.Logger.Error("Failed to write to stream", "error", err, "remote_addr", s.GetClientAddr())
		return NewConnectionError("failed to write message", err)
	}

	s.server.Logger.Debug("Sent data to client", "bytes", n, "remote_addr", s.GetClientAddr())
	return nil
}

// handleSession processes incoming data for a QUIC session
func (q *QUICServer) handleSession(session *QUICSession) {
	q.wg.Add(1)
	defer q.wg.Done()
	defer q.closeSession(session)

	readDone := make(chan struct{})
	go func() {
		defer close(readDone)
		buffer := make([]byte, q.MaxLength)
		for {
			n, err := session.stream.Read(buffer)
			if err != nil {
				if err == io.EOF {
					q.Logger.Debug("Connection closed by client", "remote_addr", session.GetClientAddr())
				} else {
					q.Logger.Error("Error reading from stream", "error", err, "remote_addr", session.GetClientAddr())
				}
				return
			}

			if n == 0 {
				q.Logger.Debug("Read 0 bytes, continuing", "remote_addr", session.GetClientAddr())
				continue
			}

			// Update last received time and send to channel
			session.updateLastReceived()
			data := make([]byte, n)
			copy(data, buffer[:n])

			q.Logger.Debug("Read data from stream", "bytes", n, "remote_addr", session.GetClientAddr())

			select {
			case session.DataChannel <- data:
				q.Logger.Debug("Sent data to channel", "bytes", n, "remote_addr", session.GetClientAddr())
			case <-q.ctx.Done():
				return
			default:
				q.Logger.Warn("Channel full, dropping message", "remote_addr", session.GetClientAddr())
			}
		}
	}()

	select {
	case <-q.ctx.Done():
	case <-readDone:
	}
}

// closeSession closes a QUIC session and cleans up resources
func (q *QUICServer) closeSession(session *QUICSession) {
	addr := session.GetClientAddr().String()

	q.sessionsMutex.Lock()
	if _, exists := q.sessions[addr]; !exists {
		q.sessionsMutex.Unlock()
		return
	}
	delete(q.sessions, addr)
	q.sessionsMutex.Unlock()

	session.Cancel()
	if session.stream != nil {
		session.stream.Close()
	}
	if session.conn != nil {
		session.conn.CloseWithError(0, "session closed")
	}

	// Drain and close channel
	for {
		select {
		case <-session.DataChannel:
		default:
			close(session.DataChannel)
			q.Logger.Debug("Closed session for %s", session.GetClientAddr())
			return
		}
	}
}

// CloseSession closes the QUIC session and cleans up resources
func (s *QUICSession) CloseSession() {
	s.server.closeSession(s)
}
