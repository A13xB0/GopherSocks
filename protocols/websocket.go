package streamProtocols

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

type WebSocketServer struct {
	upgrader               websocket.Upgrader
	addr                   string
	sessions               map[string]*WebSocketSession
	sessionsMutex          sync.RWMutex
	announceMiddleware     AnnounceMiddlewareFunc
	announceMiddlewareOpts any
	ctx                    context.Context
}

type WebSocketSession struct {
	*WebSocketServer
	SessionID         string
	ClientConn        *websocket.Conn
	DataChannel       chan []byte
	LastReceived      time.Time
	lastReceivedMutex sync.Mutex
}

func NewWebSocket(host string, port uint16, ctx context.Context) *WebSocketServer {
	addr := fmt.Sprintf("%v:%v", host, port)
	return &WebSocketServer{
		upgrader: websocket.Upgrader{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
		},
		addr:     addr,
		ctx:      ctx,
		sessions: make(map[string]*WebSocketSession),
	}
}

func (w *WebSocketServer) StartReceiveStream() error {
	http.HandleFunc("/", w.handleConnections)
	err := http.ListenAndServe(w.addr, nil)
	if err != nil {
		return err
	}
	return nil
}

func (w *WebSocketServer) StopReceiveStream() error {
	defer w.ctx.Done()
	w.sessionsMutex.Lock()
	for _, session := range w.sessions {
		session.CloseSession()
	}
	w.sessions = make(map[string]*WebSocketSession)
	w.sessionsMutex.Unlock()
	return nil
}

func (w *WebSocketServer) SetAnnounceNewSession(function AnnounceMiddlewareFunc, options any) {
	w.announceMiddleware = function
	w.announceMiddlewareOpts = options
}

func (w *WebSocketServer) handleConnections(rw http.ResponseWriter, req *http.Request) {
	conn, err := w.upgrader.Upgrade(rw, req, nil)
	if err != nil {
		return
	}
	session := w.newSession(conn)
	for {
		_, message, err := conn.ReadMessage()
		if err != nil {
			break
		}
		session.receiveBytes(message)
	}
}

func (w *WebSocketServer) newSession(conn *websocket.Conn) *WebSocketSession {
	newSession := &WebSocketSession{
		WebSocketServer: w,
		SessionID:       uuid.NewString(),
		ClientConn:      conn,
		DataChannel:     make(chan []byte, 100),
		LastReceived:    time.Now(),
	}
	fmt.Printf("New session created %v - Session ID: %v\n", conn.RemoteAddr().String(), newSession.SessionID)
	w.sessionsMutex.Lock()
	w.sessions[conn.RemoteAddr().String()] = newSession
	w.sessionsMutex.Unlock()
	newSession.receiveBytes(nil)
	if w.announceMiddleware != nil {
		w.announceMiddleware(w.announceMiddlewareOpts, newSession)
	}
	return newSession
}

func (w *WebSocketServer) GetActiveSessions() map[string]*WebSocketSession {
	return w.sessions
}

func (w *WebSocketServer) GetSession(ClientAddr string) *WebSocketSession {
	w.sessionsMutex.RLock()
	defer w.sessionsMutex.RUnlock()
	return w.sessions[ClientAddr]
}

func (s *WebSocketSession) SendToClient(data []byte) error {
	if err := s.ClientConn.WriteMessage(websocket.TextMessage, data); err != nil {
		return err
	}
	return nil
}

func (s *WebSocketSession) receiveBytes(data ...[]byte) {
	s.lastReceivedMutex.Lock()
	s.LastReceived = time.Now()
	s.lastReceivedMutex.Unlock()

	for _, d := range data {
		s.DataChannel <- d
	}
}

func (s *WebSocketSession) Data() (DataFromClient chan []byte) {
	return s.DataChannel
}

func (s *WebSocketSession) CloseSession() {
	close(s.DataChannel)
	s.sessionsMutex.Lock()
	delete(s.sessions, s.ClientConn.RemoteAddr().String())
	s.sessionsMutex.Unlock()
}

func (s *WebSocketSession) GetSessionID() string {
	return s.SessionID
}

func (s *WebSocketSession) GetClientAddr() net.Addr {
	return s.ClientConn.RemoteAddr()
}

func (s *WebSocketSession) GetLastRecieved() time.Time {
	s.lastReceivedMutex.Lock()
	lr := s.LastReceived
	s.lastReceivedMutex.Unlock()
	return lr
}
