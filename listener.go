// This package handles streaming protocols as an interface
package gophersocks

import (
	"context"
	protocols "github.com/A13xB0/GopherSocks/protocols"
)

// Listener defines the interface for streaming TCP and UDP connections
type Listener interface {
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
func NewTCPListener(host string, port uint16, opts ...TCPOptFunc) (Listener, error) {
	return NewTCPListenerWithContext(host, port, context.Background(), opts...)
}

// NewWithContext creates a new Stream handler for your chosen stream type, with context
func NewTCPListenerWithContext(host string, port uint16, ctx context.Context, opts ...TCPOptFunc) (Listener, error) {
	tcpConfig := tcpDefaultConfig()
	for _, fn := range opts {
		fn(&tcpConfig)
	}
	return protocols.NewTCP(host, port, ctx, tcpConfig)
}

// New creates a new Stream handler for your chosen stream type
func NewUDPListener(host string, port uint16, opts ...UDPOptFunc) (Listener, error) {
	return NewUDPListenerWithContext(host, port, context.Background(), opts...)
}

// NewWithContext creates a new Stream handler for your chosen stream type, with context
func NewUDPListenerWithContext(host string, port uint16, ctx context.Context, opts ...UDPOptFunc) (Listener, error) {
	udpConfig := udpDefaultConfig()
	for _, fn := range opts {
		fn(&udpConfig)
	}
	return protocols.NewUDP(host, port, ctx, udpConfig)
}

// New creates a new Stream handler for your chosen stream type
func NewWebsocketsListener(host string, port uint16, opts ...WebsocketOptFunc) (Listener, error) {
	return NewWebsocketsListenerWithContext(host, port, context.Background(), opts...)
}

// NewWithContext creates a new Stream handler for your chosen stream type, with context
func NewWebsocketsListenerWithContext(host string, port uint16, ctx context.Context, opts ...WebsocketOptFunc) (Listener, error) {
	wsConfig := websocketDefaultConfig()
	for _, fn := range opts {
		fn(&wsConfig)
	}
	return protocols.NewWebSocket(host, port, ctx, wsConfig)
}
