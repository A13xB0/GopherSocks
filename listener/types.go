package listener

import (
	"net"
	"time"
)

// This is a session interface for managing the active connection of the streaming protocol
type Session interface {
	//Sends to client
	SendToClient(data []byte) error
	//Receive Data channel
	Data() (DataFromClient chan []byte)
	//Close Session
	CloseSession()

	//Getters

	//Get Session UUID
	GetSessionID() string
	//Get Client Addr
	GetClientAddr() net.Addr
	//Get Last Recieved
	GetLastRecieved() time.Time
}

type AnnounceMiddlewareFunc func(options any, session Session)
