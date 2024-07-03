package streamProtocols

import (
	"context"
	"fmt"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
)

// This is the UDP Server for listening for UDP Packets on the port specified
type UDPServer struct {
	conn                   net.PacketConn         //UDP Listener
	addr                   string                 //Host Address (IP:PORT)
	sessions               map[string]Session     //Active Session (uses addr)
	sessionsMutex          sync.RWMutex           //Mutex for sessions
	announceMiddleware     AnnounceMiddlewareFunc //Middleware for announcing new session
	announceMiddlewareOpts any                    //Options for middleware
	ctx                    context.Context
}

type UDPSession struct {
	*UDPServer                    //composite UDPServer
	SessionID         string      //UUID of session, this is to make sure they are the session which was setup by the server
	ClientAddr        net.Addr    //Client Address (IP:Port)
	DataChannel       chan []byte //Data Channel, this will be handled top level
	LastReceived      time.Time   //Last time a packet was recieved
	lastReceivedMutex sync.Mutex
}

// Create new UDP Listener Object
func NewUDP(host string, port uint16, ctx context.Context) *UDPServer {
	addr := fmt.Sprintf("%v:%v", host, port)
	return &UDPServer{addr: addr, ctx: ctx, sessions: make(map[string]Session)}
}

// Start Go Routine to listen for UDP packets
func (u *UDPServer) StartReceiveStream() error {
	//Announce listening port and open
	conn, err := net.ListenPacket("udp", u.addr)
	if err != nil {
		return err
	}
	u.conn = conn
	//Start receive stream
	go u.receiveStream()
	return nil
}

// Stops Receiving stream
func (u *UDPServer) StopReceiveStream() error {

	if err := u.conn.Close(); err != nil {
		return err
	}
	u.sessionsMutex.Lock()
	clear(u.sessions)
	u.sessionsMutex.Unlock()
	return nil
}

// Allows for middleware for announcing a new session (allows for session handling)
func (u *UDPServer) SetAnnounceNewSession(function AnnounceMiddlewareFunc, options any) {
	u.announceMiddleware = function
	u.announceMiddlewareOpts = options
}

// This function recieves the streamed data from various clients and sorts it into channels
func (u *UDPServer) receiveStream() {
	buffer := make([]byte, 10240)
	for {
		select {
		case <-u.ctx.Done():
			return
		default:
			n, addr, err := u.conn.ReadFrom(buffer)
			if err != nil {
				//I REALLY NEED TO CREATE A LOGGER
				fmt.Printf("Error while reading from connection: %s\n", err)
				continue
			}
			clientAddrStr := addr.String()
			var session Session
			var ok bool
			//If session does not exist create new one
			u.sessionsMutex.RLock()
			if session, ok = u.sessions[clientAddrStr]; !ok {
				u.sessionsMutex.RUnlock()
				session = u.newSession(addr, buffer[:n])
			} else {
				u.sessionsMutex.RUnlock()
				session.receiveBytes(buffer[:n])
			}
		}
	}
}

// Create new session
func (u *UDPServer) newSession(addr net.Addr, buffer []byte) Session {
	newSession := UDPSession{
		UDPServer:    u,
		SessionID:    uuid.NewString(),
		ClientAddr:   addr,
		DataChannel:  make(chan []byte, 100),
		LastReceived: time.Now(),
	}
	fmt.Printf("New session created %v - Session ID: %v\n", addr.String(), newSession.SessionID)
	// No lock needed as lock is in the calling function
	u.sessionsMutex.Lock()
	u.sessions[addr.String()] = &newSession
	u.sessionsMutex.Unlock()
	//receive bytes needs to be before announcements but after new session
	newSession.receiveBytes(buffer)
	if u.announceMiddleware != nil {
		u.announceMiddleware(u.announceMiddlewareOpts, &newSession)
	}
	return &newSession
}

func (u *UDPServer) GetActiveSessions() map[string]Session {
	return u.sessions
}

func (u *UDPServer) GetSession(ClientAddr string) Session {
	u.sessionsMutex.RLock()
	defer u.sessionsMutex.RUnlock()
	return u.sessions[ClientAddr]
}

//Session function

// Sends data to the client of the session
func (s *UDPSession) SendToClient(data []byte) error {
	if _, err := s.conn.WriteTo(data, s.ClientAddr); err != nil {
		return err
	}
	return nil
}

// Sends bytes to the appropriate channel
func (s *UDPSession) receiveBytes(data ...[]byte) {
	s.lastReceivedMutex.Lock()
	s.LastReceived = time.Now()
	s.lastReceivedMutex.Unlock()
	for _, d := range data {
		s.DataChannel <- d
	}
}

// Allows for data channel to be accessed
func (s *UDPSession) Data() (DataFromClient chan []byte) {
	return s.DataChannel
}

func (s *UDPSession) CloseSession() {
	close(s.DataChannel)
	s.sessionsMutex.Lock()
	delete(s.sessions, s.ClientAddr.String())
	s.sessionsMutex.Unlock()
}

// Session Getters

// Get Session ID
func (s *UDPSession) GetSessionID() string {
	return s.SessionID
}

// Get Client Addr
func (s *UDPSession) GetClientAddr() net.Addr {
	return s.ClientAddr
}

// Get Last Received
func (s *UDPSession) GetLastRecieved() time.Time {
	s.lastReceivedMutex.Lock()
	lr := s.LastReceived
	s.lastReceivedMutex.Unlock()
	return lr
}
