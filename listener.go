// Package gophersocks provides TCP, UDP, WebSocket and QUIC servers and
// clients that exchange framed messages, with per-session goroutines,
// bounded send queues and backpressure.
package gophersocks

import (
	"context"

	"github.com/A13xB0/GopherSocks/listener"
)

// Listener is a server for one protocol. See listener.Listener.
type Listener = listener.Listener

// Session is one client connection. See listener.Session.
type Session = listener.Session

// Handler serves one session. See listener.Handler.
type Handler = listener.Handler

// NewQUICListener creates a new QUIC server.
func NewQUICListener(host string, port uint16, opts ...ServerOptFunc) (Listener, error) {
	return NewQUICListenerWithContext(host, port, context.Background(), opts...)
}

// NewQUICListenerWithContext creates a new QUIC server with context.
func NewQUICListenerWithContext(host string, port uint16, ctx context.Context, opts ...ServerOptFunc) (Listener, error) {
	return listener.NewQUIC(host, port, ctx, toListenerOptions(opts)...)
}

// NewTCPListener creates a new TCP stream handler
func NewTCPListener(host string, port uint16, opts ...ServerOptFunc) (Listener, error) {
	return NewTCPListenerWithContext(host, port, context.Background(), opts...)
}

// NewTCPListenerWithContext creates a new TCP stream handler with context
func NewTCPListenerWithContext(host string, port uint16, ctx context.Context, opts ...ServerOptFunc) (Listener, error) {
	return listener.NewTCP(host, port, ctx, toListenerOptions(opts)...)
}

// NewUDPListener creates a new UDP stream handler
func NewUDPListener(host string, port uint16, opts ...ServerOptFunc) (Listener, error) {
	return NewUDPListenerWithContext(host, port, context.Background(), opts...)
}

// NewUDPListenerWithContext creates a new UDP stream handler with context
func NewUDPListenerWithContext(host string, port uint16, ctx context.Context, opts ...ServerOptFunc) (Listener, error) {
	return listener.NewUDP(host, port, ctx, toListenerOptions(opts)...)
}

// NewWebSocketListener creates a new WebSocket stream handler
func NewWebSocketListener(host string, port uint16, opts ...ServerOptFunc) (Listener, error) {
	return NewWebSocketListenerWithContext(host, port, context.Background(), opts...)
}

// NewWebSocketListenerWithContext creates a new WebSocket stream handler with context
func NewWebSocketListenerWithContext(host string, port uint16, ctx context.Context, opts ...ServerOptFunc) (Listener, error) {
	return listener.NewWebSocket(host, port, ctx, toListenerOptions(opts)...)
}

// toListenerOptions passes every option through, protocol-specific ones included.
func toListenerOptions(opts []ServerOptFunc) []listener.ServerOption {
	out := make([]listener.ServerOption, len(opts))
	for i, o := range opts {
		out[i] = listener.ServerOption(o)
	}
	return out
}
