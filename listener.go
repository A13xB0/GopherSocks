// This package handles streaming listener protocols as an interface
package gophersocks

import (
	"context"

	"github.com/A13xB0/GopherSocks/listener"
)

// Listener defines the interface for streaming TCP and UDP connections
type Listener interface {
	// StartReceiveStream Starts listener for stream transport
	StartListener() error

	// StopReceiveStream Stops listener for stream transport
	StopListener() error

	// SetAnnounceNewSession Sets middleware for announcing a new session
	SetAnnounceNewSession(function listener.AnnounceMiddlewareFunc, options any)

	//Getters

	// GetActiveSessions Get all sessions
	GetActiveSessions() map[string]listener.Session

	// GetSession Get session from ClientAddr (IP:Port)
	GetSession(ClientAddr string) listener.Session
}

// NewTCPListener creates a new TCP stream handler
func NewTCPListener(host string, port uint16, opts ...ServerOptFunc) (Listener, error) {
	return NewTCPListenerWithContext(host, port, context.Background(), opts...)
}

// NewTCPListenerWithContext creates a new TCP stream handler with context
func NewTCPListenerWithContext(host string, port uint16, ctx context.Context, opts ...ServerOptFunc) (Listener, error) {
	listenerOpts := convertToListenerOptions(opts)
	return listener.NewTCP(host, port, ctx, listenerOpts...)
}

// NewUDPListener creates a new UDP stream handler
func NewUDPListener(host string, port uint16, opts ...ServerOptFunc) (Listener, error) {
	return NewUDPListenerWithContext(host, port, context.Background(), opts...)
}

// NewUDPListenerWithContext creates a new UDP stream handler with context
func NewUDPListenerWithContext(host string, port uint16, ctx context.Context, opts ...ServerOptFunc) (Listener, error) {
	listenerOpts := convertToListenerOptions(opts)
	return listener.NewUDP(host, port, ctx, listenerOpts...)
}

// NewWebSocketListener creates a new WebSocket stream handler (previously NewWebsocketsListener)
func NewWebSocketListener(host string, port uint16, opts ...ServerOptFunc) (Listener, error) {
	return NewWebSocketListenerWithContext(host, port, context.Background(), opts...)
}

// NewWebSocketListenerWithContext creates a new WebSocket stream handler with context
func NewWebSocketListenerWithContext(host string, port uint16, ctx context.Context, opts ...ServerOptFunc) (Listener, error) {
	config := convertToServerConfig(opts...)
	// Set default WebSocket-specific configuration
	if config.ProtocolConfig == nil {
		config.ProtocolConfig = &WebSocketConfig{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			Path:            "/ws",
		}
	}
	listenerOpts := convertToListenerOptions(opts)
	return listener.NewWebSocket(host, port, ctx, listenerOpts...)
}

// convertToListenerOptions converts our ServerOptFunc options to listener.ServerOption options
func convertToListenerOptions(opts []ServerOptFunc) []listener.ServerOption {
	config := convertToServerConfig(opts...)
	listenerOpts := make([]listener.ServerOption, 0)

	// Convert our config settings to listener.ServerOption functions
	listenerOpts = append(listenerOpts,
		listener.WithMaxLength(config.MaxLength),
		listener.WithBufferSize(config.BufferSize),
		listener.WithLogger(config.Logger),
		listener.WithTimeouts(config.ReadTimeout, config.WriteTimeout),
		listener.WithMaxConnections(config.MaxConnections),
	)

	return listenerOpts
}
