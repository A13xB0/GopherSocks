// This package handles streaming listenerprotocols as an interface
package gophersocks

import (
	"context"
	"github.com/A13xB0/GopherSocks/listenerprotocols"
)

// Listener defines the interface for streaming TCP and UDP connections
type Listener interface {
	// StartReceiveStream Starts listener for stream transport
	StartReceiveStream() error

	// StopReceiveStream Stops listener for stream transport
	StopReceiveStream() error

	// SetAnnounceNewSession Sets middlware for announcing a new session
	SetAnnounceNewSession(function listenerprotocols.AnnounceMiddlewareFunc, options any)

	//Getters

	// GetActiveSessions Get all sessions
	GetActiveSessions() map[string]listenerprotocols.Session

	// GetSession Get session from ClientAddr (IP:Port)
	GetSession(ClientAddr string) listenerprotocols.Session
}

// NewTCPListener creates a new Stream handler for your chosen stream type
func NewTCPListener(host string, port uint16, opts ...TCPOptFunc) (Listener, error) {
	return NewTCPListenerWithContext(host, port, context.Background(), opts...)
}

// NewTCPListenerWithContext creates a new Stream handler for your chosen stream type, with context
func NewTCPListenerWithContext(host string, port uint16, ctx context.Context, opts ...TCPOptFunc) (Listener, error) {
	tcpConfig := tcpDefaultConfig()
	for _, fn := range opts {
		fn(&tcpConfig)
	}
	return listenerprotocols.NewTCP(host, port, ctx, tcpConfig)
}

// NewUDPListener creates a new Stream handler for your chosen stream type
func NewUDPListener(host string, port uint16, opts ...UDPOptFunc) (Listener, error) {
	return NewUDPListenerWithContext(host, port, context.Background(), opts...)
}

// NewUDPListenerWithContext creates a new Stream handler for your chosen stream type, with context
func NewUDPListenerWithContext(host string, port uint16, ctx context.Context, opts ...UDPOptFunc) (Listener, error) {
	udpConfig := udpDefaultConfig()
	for _, fn := range opts {
		fn(&udpConfig)
	}
	return listenerprotocols.NewUDP(host, port, ctx, udpConfig)
}

// NewWebsocketsListener creates a new Stream handler for your chosen stream type
func NewWebsocketsListener(host string, port uint16, opts ...WebsocketOptFunc) (Listener, error) {
	return NewWebsocketsListenerWithContext(host, port, context.Background(), opts...)
}

// NewWebsocketsListenerWithContext creates a new Stream handler for your chosen stream type, with context
func NewWebsocketsListenerWithContext(host string, port uint16, ctx context.Context, opts ...WebsocketOptFunc) (Listener, error) {
	wsConfig := websocketDefaultConfig()
	for _, fn := range opts {
		fn(&wsConfig)
	}
	return listenerprotocols.NewWebSocket(host, port, ctx, wsConfig)
}
