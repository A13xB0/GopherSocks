package listener

import (
	"fmt"
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
		wsConfig = &WebSocketConfig{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			Path:            "/ws",
		}
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
	go func() {
		if err := w.httpServer.ListenAndServe(); err != http.ErrServerClosed {
			w.Logger.Error("HTTP server error: %v", err)
		}
	}()

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
		session.CloseSession()
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
	w.sessionsMutex.RLock()
	if len(w.sessions) >= w.MaxConnections {
		w.sessionsMutex.RUnlock()
		w.Logger.Warn("Max connections reached, rejecting connection from %s", req.RemoteAddr)
		http.Error(rw, "Too many connections", http.StatusServiceUnavailable)
		return
	}
	w.sessionsMutex.RUnlock()

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
	base := NewBaseSession(conn.RemoteAddr(), context.Background(), w.Logger)
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
	defer session.CloseSession()

	readChan := make(chan []byte)
	errChan := make(chan error)

	// Read messages in a separate goroutine
	go func() {
		for {
			select {
			case <-w.ctx.Done():
				return
			default:
				// Set read deadline to allow context cancellation
				session.ClientConn.SetReadDeadline(time.Now().Add(time.Second))
				messageType, message, err := session.ClientConn.ReadMessage()
				if err != nil {
					if websocket.IsCloseError(err, websocket.CloseGoingAway, websocket.CloseNormalClosure) {
						return // Normal closure
					}
					errChan <- err
					return
				}
				if messageType != websocket.BinaryMessage {
					w.Logger.Warn("Received non-binary message from %s", session.GetClientAddr())
					continue
				}
				readChan <- message
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
		case message := <-readChan:
			if len(message) > int(w.MaxLength) {
				w.Logger.Warn("Message exceeds maximum length from %s", session.GetClientAddr())
				continue
			}
			session.receiveBytes(message)
		}
	}
}

// GetActiveSessions returns all active sessions
func (w *WebSocketServer) GetActiveSessions() map[string]Session {
	return w.sessions
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
		s.DataChannel <- d
	}
}

// CloseSession closes the WebSocket session and cleans up resources
func (s *WebSocketSession) CloseSession() {
	s.server.sessionsMutex.Lock()
	defer s.server.sessionsMutex.Unlock()

	// Check if session is already closed
	if _, exists := s.server.sessions[s.GetClientAddr().String()]; !exists {
		return
	}

	s.Cancel() // Cancel context from base session
	s.ClientConn.Close()
	close(s.DataChannel)
	delete(s.server.sessions, s.GetClientAddr().String())

	s.server.Logger.Debug("Closed session for %s", s.GetClientAddr())
}

// GetLastRecieved maintains backward compatibility with the Session interface
func (s *WebSocketSession) GetLastRecieved() time.Time {
	return s.GetLastReceived()
}
