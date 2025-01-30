package listener

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
	"golang.org/x/net/context"
)

// WebSocketServer implements a WebSocket streaming server with enhanced session management
type WebSocketServer struct {
	httpServer             *http.Server
	upgrader               websocket.Upgrader
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

// WebSocketSession represents an active WebSocket connection
type WebSocketSession struct {
	*BaseSession
	server     *WebSocketServer
	ClientConn *websocket.Conn
}

// NewWebSocket creates a new WebSocket server with the given configuration
func NewWebSocket(host string, port uint16, ctx context.Context, opts ...ServerOption) (*WebSocketServer, error) {
	config := defaultConfig()
	for _, opt := range opts {
		opt(config)
	}

	if err := ValidateConfig(config); err != nil {
		return nil, NewConfigError("invalid configuration", err)
	}

	// Get WebSocket-specific configuration
	wsConfig, ok := config.ProtocolConfig.(*WebSocketConfig)
	if !ok {
		return nil, NewConfigError("invalid WebSocket configuration", nil)
	}

	addr := fmt.Sprintf("%v:%v", host, port)
	serverCtx, cancel := context.WithCancel(ctx)
	server := &WebSocketServer{
		addr:         addr,
		ServerConfig: config,
		sessions:     make(map[string]Session),
		ctx:          serverCtx,
		cancel:       cancel,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  wsConfig.ReadBufferSize,
			WriteBufferSize: wsConfig.WriteBufferSize,
			CheckOrigin:     func(r *http.Request) bool { return true }, // Allow all origins
		},
	}

	return server, nil
}

// StartListener begins accepting WebSocket connections
func (w *WebSocketServer) StartListener() error {
	mux := http.NewServeMux()
	wsConfig := w.ProtocolConfig.(*WebSocketConfig)
	mux.HandleFunc(wsConfig.Path, w.handleConnections)

	w.httpServer = &http.Server{
		Addr:    w.addr,
		Handler: mux,
	}

	w.Logger.Info("WebSocket server listening on %s%s", w.addr, wsConfig.Path)

	// Start server in a goroutine
	errChan := make(chan error, 1)
	go func() {
		if err := w.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			w.Logger.Error("HTTP server error: %v", err)
			errChan <- err
		}
	}()

	// Wait for server to start
	time.Sleep(100 * time.Millisecond)
	select {
	case err := <-errChan:
		return err
	default:
		// Server started successfully
	}

	return nil
}

// StopListener gracefully shuts down the WebSocket server
func (w *WebSocketServer) StopListener() error {
	w.Logger.Info("Shutting down WebSocket server")

	// Cancel context to stop all goroutines
	w.cancel()

	// Close all active sessions
	w.sessionsMutex.Lock()
	for _, session := range w.sessions {
		s := session.(*WebSocketSession)
		s.Cancel() // Cancel context from base session
		s.ClientConn.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(100*time.Millisecond))
		s.ClientConn.Close()
		close(s.DataChannel)
		delete(w.sessions, s.GetClientAddr().String())
	}
	w.sessionsMutex.Unlock()

	// Shutdown HTTP server with timeout
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer shutdownCancel()

	if err := w.httpServer.Shutdown(shutdownCtx); err != nil {
		return NewConnectionError("failed to shutdown server", err)
	}

	// Wait for all goroutines to finish
	w.wg.Wait()
	return nil
}

// SetAnnounceNewSession sets the middleware for announcing new sessions
func (w *WebSocketServer) SetAnnounceNewSession(function AnnounceMiddlewareFunc, options any) {
	w.announceMiddleware = function
	w.announceMiddlewareOpts = options
}

// handleConnections processes incoming WebSocket connections
func (w *WebSocketServer) handleConnections(rw http.ResponseWriter, req *http.Request) {
	// Check max connections before upgrading
	w.sessionsMutex.Lock()
	if len(w.sessions) >= w.MaxConnections {
		w.sessionsMutex.Unlock()
		w.Logger.Warn("Max connections reached, rejecting connection from %s", req.RemoteAddr)
		http.Error(rw, "Too many connections", http.StatusServiceUnavailable)
		w.cancel() // Stop accepting new connections
		return
	}
	w.sessionsMutex.Unlock()

	// Upgrade connection to WebSocket
	conn, err := w.upgrader.Upgrade(rw, req, nil)
	if err != nil {
		w.Logger.Error("WebSocket upgrade failed: %v", err)
		return
	}

	session := w.newSession(conn)
	w.handleSession(session)
}

// newSession creates a new WebSocket session
func (w *WebSocketServer) newSession(conn *websocket.Conn) *WebSocketSession {
	base := NewBaseSession(conn.RemoteAddr(), w.ctx, w.Logger) // Use server context
	base.ID = uuid.NewString()

	session := &WebSocketSession{
		BaseSession: base,
		server:      w,
		ClientConn:  conn,
	}

	w.sessionsMutex.Lock()
	w.sessions[conn.RemoteAddr().String()] = session
	w.sessionsMutex.Unlock()

	if w.announceMiddleware != nil {
		w.announceMiddleware(w.announceMiddlewareOpts, session)
	}

	return session
}

// handleSession processes incoming WebSocket messages
func (w *WebSocketServer) handleSession(session *WebSocketSession) {
	w.wg.Add(1)
	defer w.wg.Done()
	defer w.closeSession(session)

	readChan := make(chan []byte, w.BufferSize)
	errChan := make(chan error, 1)

	// Read messages in a separate goroutine
	go func() {
		defer close(readChan)
		defer close(errChan)

		for {
			select {
			case <-w.ctx.Done():
				return
			default:
				// Set read deadline to allow context cancellation
				session.ClientConn.SetReadDeadline(time.Now().Add(100 * time.Millisecond))
				messageType, message, err := session.ClientConn.ReadMessage()
				if err != nil {
					if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
						return // Normal closure
					}
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						continue // Deadline exceeded, try again
					}
					if ne, ok := err.(net.Error); ok && ne.Temporary() {
						w.Logger.Warn("Temporary error reading message: %v", err)
						time.Sleep(100 * time.Millisecond) // Basic retry backoff
						continue
					}
					select {
					case errChan <- err:
					case <-w.ctx.Done():
					}
					return
				}

				// Validate message before processing
				if messageType != websocket.BinaryMessage {
					w.Logger.Warn("Received non-binary message from %s", session.GetClientAddr())
					continue
				}

				if len(message) > int(w.MaxLength) {
					w.Logger.Warn("Message exceeds maximum length from %s", session.GetClientAddr())
					session.ClientConn.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(100*time.Millisecond))
					session.ClientConn.Close() // Close connection for security
					session.CloseSession()
					return
				}

				// Process the message
				select {
				case readChan <- message:
				case <-w.ctx.Done():
					return
				default:
					w.Logger.Warn("Channel full, dropping message from %s", session.GetClientAddr())
				}
			}
		}
	}()

	for {
		select {
		case <-w.ctx.Done():
			return
		case err := <-errChan:
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				w.Logger.Error("WebSocket error: %v", err)
			}
			return
		case message, ok := <-readChan:
			if !ok {
				return // Channel closed
			}

			// Process the received data with non-blocking send
			select {
			case session.DataChannel <- message:
				session.updateLastReceived()
			case <-w.ctx.Done():
				return
			default:
				w.Logger.Warn("Channel full, dropping message from %s", session.GetClientAddr())
			}
		}
	}
}

// closeSession closes a WebSocket session and cleans up resources
func (w *WebSocketServer) closeSession(session *WebSocketSession) {
	w.sessionsMutex.Lock()
	defer w.sessionsMutex.Unlock()

	// Check if session is already closed
	if _, exists := w.sessions[session.GetClientAddr().String()]; !exists {
		return
	}

	session.Cancel() // Cancel context from base session
	session.ClientConn.WriteControl(websocket.CloseMessage, []byte{}, time.Now().Add(100*time.Millisecond))
	session.ClientConn.Close()

	select {
	case <-session.DataChannel:
		// Drain any remaining messages
	default:
	}
	close(session.DataChannel)

	delete(w.sessions, session.GetClientAddr().String())
	w.Logger.Debug("Closed session for %s", session.GetClientAddr())

	// Wait for cleanup to complete
	time.Sleep(200 * time.Millisecond)
}

// GetActiveSessions returns all active sessions
func (w *WebSocketServer) GetActiveSessions() map[string]Session {
	w.sessionsMutex.RLock()
	defer w.sessionsMutex.RUnlock()
	sessions := make(map[string]Session)
	for k, v := range w.sessions {
		sessions[k] = v
	}
	return sessions
}

// GetSession returns a specific session by client address
func (w *WebSocketServer) GetSession(ClientAddr string) Session {
	w.sessionsMutex.RLock()
	defer w.sessionsMutex.RUnlock()
	return w.sessions[ClientAddr]
}

// SendToClient sends data to the WebSocket client
func (s *WebSocketSession) SendToClient(data []byte) error {
	if len(data) > int(s.server.MaxLength) {
		return NewProtocolError("message exceeds maximum length", nil)
	}

	if err := s.ClientConn.WriteMessage(websocket.BinaryMessage, data); err != nil {
		return NewConnectionError("failed to send message", err)
	}

	return nil
}

// receiveBytes processes received WebSocket messages
func (s *WebSocketSession) receiveBytes(data ...[]byte) {
	s.updateLastReceived()
	for _, d := range data {
		if len(d) == 0 {
			continue
		}
		select {
		case s.DataChannel <- d:
		case <-s.server.ctx.Done():
			return
		default:
			s.server.Logger.Warn("Channel full, dropping message")
		}
	}
}

// CloseSession closes the WebSocket session and cleans up resources
func (s *WebSocketSession) CloseSession() {
	s.server.closeSession(s)
}

// GetLastRecieved implements the Session interface
func (s *WebSocketSession) GetLastRecieved() time.Time {
	return s.BaseSession.GetLastReceived()
}
