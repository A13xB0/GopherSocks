package streamProtocols

import (
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/google/uuid"
	"golang.org/x/net/websocket"
)

type WebSocketServer struct {
	server                 *websocket.Server
	addr                   string
	sessions               map[string]Session
	sessionsMutex          sync.RWMutex
	announceMiddleware     AnnounceMiddlewareFunc
	announceMiddlewareOpts any
	stop                   chan bool
}

type WebSocketSession struct {
	*WebSocketServer
	SessionID         string
	ClientConn        *websocket.Conn
	DataChannel       chan []byte
	LastReceived      time.Time
	lastReceivedMutex sync.Mutex
}

func NewWebSocket(host string, port uint16) *WebSocketServer {
	addr := fmt.Sprintf("%v:%v", host, port)
	return &WebSocketServer{addr: addr, stop: make(chan bool), sessions: make(map[string]Session)}
}

func (w *WebSocketServer) StartReceiveStream() error {
	w.server = &websocket.Server{Handler: w.handleConnections}
	go http.ListenAndServe(w.addr, nil)
	return nil
}

func (w *WebSocketServer) StopReceiveStream() error {
	w.stop <- true
	w.sessionsMutex.Lock()
	for _, session := range w.sessions {
		session.CloseSession()
	}
	clear(w.sessions)
	w.sessionsMutex.Unlock()
	return nil
}

func (w *WebSocketServer) SetAnnounceNewSession(function AnnounceMiddlewareFunc, options any) {
	w.announceMiddleware = function
	w.announceMiddlewareOpts = options
}

func (w *WebSocketServer) handleConnections(ws *websocket.Conn) {
	clientAddrStr := ws.RemoteAddr().String()
	var session Session
	var ok bool
	w.sessionsMutex.RLock()
	if session, ok = w.sessions[clientAddrStr]; !ok {
		w.sessionsMutex.RUnlock()
		session = w.newSession(ws)
	} else {
		w.sessionsMutex.RUnlock()
	}
	for {
		var message []byte
		err := websocket.Message.Receive(ws, &message)
		if err != nil {
			break
		}
		session.receiveBytes(message)
	}
}

func (w *WebSocketServer) newSession(ws *websocket.Conn) Session {
	newSession := WebSocketSession{
		WebSocketServer: w,
		SessionID:       uuid.NewString(),
		ClientConn:      ws,
		DataChannel:     make(chan []byte, 100),
		LastReceived:    time.Now(),
	}
	fmt.Printf("New session created %v - Session ID: %v\n", ws.RemoteAddr().String(), newSession.SessionID)
	w.sessionsMutex.Lock()
	w.sessions[ws.RemoteAddr().String()] = &newSession
	w.sessionsMutex.Unlock()
	newSession.receiveBytes(nil)
	if w.announceMiddleware != nil {
		w.announceMiddleware(w.announceMiddlewareOpts, &newSession)
	}
	return &newSession
}

func (w *WebSocketServer) GetActiveSessions() map[string]Session {
	return w.sessions
}

func (w *WebSocketServer) GetSession(ClientAddr string) Session {
	w.sessionsMutex.RLock()
	defer w.sessionsMutex.RUnlock()
	return w.sessions[ClientAddr]
}

func (s *WebSocketSession) SendToClient(data []byte) error {
	if err := websocket.Message.Send(s.ClientConn, data); err != nil {
		return err
	}
	return nil
}

func (s *WebSocketSession) receiveBytes(data []byte) {
	s.lastReceivedMutex.Lock()
	s.LastReceived = time.Now()
	s.lastReceivedMutex.Unlock()

	if data != nil {
		s.DataChannel <- data
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
