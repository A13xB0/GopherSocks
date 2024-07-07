package listenerprotocols

import (
	"fmt"
	"golang.org/x/net/context"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

// TCPServer implements Stream interface for TCP streaming
type TCPServer struct {
	conn                   net.Listener
	addr                   string
	announceMiddleware     AnnounceMiddlewareFunc
	announceMiddlewareOpts any
	sessions               map[string]Session
	sessionsMutex          sync.RWMutex
	ctx                    context.Context
	cancel                 context.CancelFunc
	TCPConfig
}

type TCPSession struct {
	*TCPServer
	sConn        net.Conn
	SessionID    string      //UUID of session, this is to make sure they are the session which was setup by the server
	ClientAddr   net.Addr    //Client Address (IP:Port)
	DataChannel  chan []byte //Data Channel, this will be handled top level
	LastReceived time.Time   //Last time a packet was recieved
	ctx          context.Context
	cancel       context.CancelFunc
}

type TCPConfig struct {
}

func NewTCP(host string, port uint16, ctx context.Context, config TCPConfig) (*TCPServer, error) {
	addr := fmt.Sprintf("%v:%v", host, port)
	tcpContext, cancel := context.WithCancel(ctx)
	return &TCPServer{addr: addr, ctx: tcpContext, cancel: cancel, TCPConfig: config, sessions: make(map[string]Session)}, nil
}

func (t *TCPServer) StartReceiveStream() error {
	//Start listening for connections
	conn, err := net.Listen("tcp", t.addr)
	if err != nil {
		return err
	}
	t.conn = conn
	t.receiveStream()
	return nil
}

func (t *TCPServer) StopReceiveStream() error {
	for _, session := range t.sessions {
		session.CloseSession()
	}
	t.cancel()
	return nil
}

func (t *TCPServer) receiveStream() {
	for {
		select {
		case <-t.ctx.Done():
			if err := t.conn.Close(); err != nil {
				fmt.Printf("Error from connection: %v\n", err)
			}
			return
		default:
			sConn, err := t.conn.Accept() //Session connection
			if err != nil {
				fmt.Printf("Error accepting new connection: %s", err)
				continue
			}
			clientAddrStr := sConn.RemoteAddr().String()
			var session Session
			var ok bool
			//If session does not exist create new one
			t.sessionsMutex.RLock()
			session, ok = t.sessions[clientAddrStr]
			t.sessionsMutex.RUnlock()
			if !ok {
				session = t.newSession(sConn.RemoteAddr(), sConn, t.ctx)
			}
			go session.receiveBytes()
		}
	}
}

func (t *TCPServer) newSession(addr net.Addr, sConn net.Conn, ctx context.Context) Session {
	newSession := TCPSession{
		TCPServer:    t,
		SessionID:    uuid.NewString(),
		ClientAddr:   addr,
		DataChannel:  make(chan []byte, 100),
		LastReceived: time.Now(),
		sConn:        sConn,
	}
	newSession.ctx, newSession.cancel = context.WithCancel(ctx)
	t.sessionsMutex.Lock()
	t.sessions[addr.String()] = &newSession
	t.sessionsMutex.Unlock()
	if t.announceMiddleware != nil {
		t.announceMiddleware(t.announceMiddlewareOpts, &newSession)
	}
	return &newSession
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

func (s *TCPSession) SendToClient(data []byte) error {
	if _, err := s.sConn.Write(data); err != nil {
		return err
	}
	return nil
}

func (s *TCPSession) receiveBytes(data ...[]byte) {
	//Will need to implement a delimeter at some point for any streaming functionality
	//Will need to implement read deadline
	//Will need to implement nil pointer checks for terminated connection
	buffer := make([]byte, 10240)
	for {
		select {
		case <-s.ctx.Done():
			s.CloseSession()
			return
		default:
			_, err := s.sConn.Read(buffer)
			if err != nil {
				fmt.Printf("Error while reading from connection: %s\n", err)
				continue
			}
			s.DataChannel <- buffer
		}
	}
}

func (s *TCPSession) Data() (DataFromClient chan []byte) {
	return s.DataChannel
}

func (s *TCPSession) CloseSession() {

	defer s.cancel()
	s.conn.Close()
	close(s.DataChannel)
	s.sessionsMutex.Lock()
	delete(s.sessions, s.ClientAddr.String())
	s.sessionsMutex.Unlock()
}

// Session Getters
func (s *TCPSession) GetSessionID() string {
	return s.SessionID
}

// Get Client Addr
func (s *TCPSession) GetClientAddr() net.Addr {
	return s.ClientAddr
}

// Get Last Received
func (s *TCPSession) GetLastRecieved() time.Time {
	return s.LastReceived
}
