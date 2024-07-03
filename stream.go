// This package handles streaming protocols as an interface
package stream

import (
	"fmt"
	streamProtocols "github.com/A13xB0/GopherSocks/protocols"
)

type StreamType int

const (
	UDP       = 1
	TCP       = 2
	WEBSOCKET = 3
)

// Stream defines the interface for streaming TCP and UDP connections
type Stream interface {
	//Starts listener for stream transport
	StartReceiveStream() error

	//Stops listener for stream transport
	StopReceiveStream() error

	//Sets middlware for announcing a new session
	SetAnnounceNewSession(function streamProtocols.AnnounceMiddlewareFunc, options any)

	//Getters

	//Get all sessions
	GetActiveSessions() map[string]streamProtocols.Session

	//Get session from ClientAddr (IP:Port)
	GetSession(ClientAddr string) streamProtocols.Session
}

func New(host string, port uint16, stype StreamType) (Stream, error) {
	switch stype {
	case UDP:
		return streamProtocols.NewUDP(host, port), nil
	case TCP:
		return streamProtocols.NewTCP(host, port), nil
	case WEBSOCKET:
		return streamProtocols.NewWebSocket(host, port), nil
	default:
		return nil, fmt.Errorf("Streaming Protocol is not valid")
	}
}
