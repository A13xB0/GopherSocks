// This package handles streaming protocols as an interface
package stream

import (
	"context"
	protocols "github.com/A13xB0/GopherSocks/protocols"
)

// Stream defines the interface for streaming TCP and UDP connections
type Stream interface {
	//Starts listener for stream transport
	StartReceiveStream() error

	//Stops listener for stream transport
	StopReceiveStream() error

	//Sets middlware for announcing a new session
	SetAnnounceNewSession(function protocols.AnnounceMiddlewareFunc, options any)

	//Getters

	//Get all sessions
	GetActiveSessions() map[string]protocols.Session

	//Get session from ClientAddr (IP:Port)
	GetSession(ClientAddr string) protocols.Session
}

// New creates a new Stream handler for your chosen stream type
func NewTCP(host string, port uint16, opts ...TCPOptFunc) (Stream, error) {
	return NewTCPWithContext(host, port, context.Background(), opts...)
}

// NewWithContext creates a new Stream handler for your chosen stream type, with context
func NewTCPWithContext(host string, port uint16, ctx context.Context, opts ...TCPOptFunc) (Stream, error) {
	tcpConfig := tcpDefaultConfig()
	for _, fn := range opts {
		fn(&tcpConfig)
	}
	return protocols.NewTCP(host, port, ctx, tcpConfig)
}

// New creates a new Stream handler for your chosen stream type
func NewUDP(host string, port uint16, opts ...UDPOptFunc) (Stream, error) {
	return NewUDPWithContext(host, port, context.Background(), opts...)
}

// NewWithContext creates a new Stream handler for your chosen stream type, with context
func NewUDPWithContext(host string, port uint16, ctx context.Context, opts ...UDPOptFunc) (Stream, error) {
	udpConfig := udpDefaultConfig()
	for _, fn := range opts {
		fn(&udpConfig)
	}
	return protocols.NewUDP(host, port, ctx, udpConfig)
}

// New creates a new Stream handler for your chosen stream type
func NewWebsockets(host string, port uint16, opts ...WebsocketOptFunc) (Stream, error) {
	return NewWebsocketsWithContext(host, port, context.Background(), opts...)
}

// NewWithContext creates a new Stream handler for your chosen stream type, with context
func NewWebsocketsWithContext(host string, port uint16, ctx context.Context, opts ...WebsocketOptFunc) (Stream, error) {
	wsConfig := websocketDefaultConfig()
	for _, fn := range opts {
		fn(&wsConfig)
	}
	return protocols.NewWebSocket(host, port, ctx, wsConfig)
}
