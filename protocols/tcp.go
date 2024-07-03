package streamProtocols

import (
	"fmt"
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
	stop                   chan bool
}

type TCPSession struct {
	*TCPServer
	sConn        net.Conn
	SessionID    string      //UUID of session, this is to make sure they are the session which was setup by the server
	ClientAddr   net.Addr    //Client Address (IP:Port)
	DataChannel  chan []byte //Data Channel, this will be handled top level
	LastReceived time.Time   //Last time a packet was recieved
	sessionStop  chan bool
}

func NewTCP(host string, port uint16) *TCPServer {
	addr := fmt.Sprintf("%v:%v", host, port)
	return &TCPServer{addr: addr, stop: make(chan bool)}
}

func (t *TCPServer) StartReceiveStream() error {
	//Start listening for connections
	conn, err := net.Listen("tcp", t.addr)
	if err != nil {
		return err
	}
	t.conn = conn
	go t.receiveStream()
	return nil
}

func (t *TCPServer) StopReceiveStream() error {
	return nil
}

func (t *TCPServer) receiveStream() {
	for {
		sConn, err := t.conn.Accept() //Session connection
		if err != nil {
			fmt.Printf("Error from connection: %s\n", err)
			continue
		}
		clientAddrStr := sConn.RemoteAddr().String()
		var session Session
		var ok bool
		//If session does not exist create new one
		t.sessionsMutex.RLock()
		if session, ok = t.sessions[clientAddrStr]; !ok {
			t.sessionsMutex.RUnlock()
			session = t.newSession(sConn.RemoteAddr(), sConn)
		}
		go session.receiveBytes(nil)
	}
}

func (t *TCPServer) newSession(addr net.Addr, sConn net.Conn) Session {
	newSession := TCPSession{
		TCPServer:    t,
		SessionID:    uuid.NewString(),
		ClientAddr:   addr,
		DataChannel:  make(chan []byte, 100),
		LastReceived: time.Now(),
		sConn:        sConn,
	}
	fmt.Printf("New session created %v - Session ID: %v\n", addr.String(), newSession.SessionID)
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

func (s *TCPSession) receiveBytes(data []byte) {
	//Data is not used, it only is for UDP
	//Will need to implement a delimeter at some point for any streaming functionality
	//Will need to implement read deadline
	//Will need to implement nil pointer checks for terminated connection
	buffer := make([]byte, 10240)
	for {
		if len(s.stop) == 1 {
			<-s.stop
			break
		}
		if len(s.sessionStop) == 1 {
			<-s.sessionStop
			break
		}
		_, err := s.sConn.Read(buffer)
		if err != nil {
			//I REALLY NEED TO CREATE A LOGGER
			fmt.Printf("Error while reading from connection: %s\n", err)
			continue
		}
		s.DataChannel <- buffer
	}
}

func (s *TCPSession) Data() (DataFromClient chan []byte) {
	return s.DataChannel
}

func (s *TCPSession) CloseSession() {
	s.sessionStop <- true
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
