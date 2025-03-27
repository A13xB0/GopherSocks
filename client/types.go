package client

import (
	"context"
	"net"
)

// Client defines the interface for all client implementations
type Client interface {
	// Connect establishes a connection to the server
	Connect(ctx context.Context) error

	// Send sends data to the server
	Send(data []byte) error

	// Receive receives data from the server
	Receive() ([]byte, error)

	// Close closes the client connection
	Close() error

	// LocalAddr returns the local network address
	LocalAddr() net.Addr

	// RemoteAddr returns the remote network address
	RemoteAddr() net.Addr
}

// Options represents client configuration options
type Options struct {
	// Address is the server address to connect to
	Address string

	// Delimiter is used to frame messages (default: "\n\n\n")
	Delimiter []byte
}

// Option is a function that configures Options
type Option func(*Options)

// WithAddress sets the server address
func WithAddress(addr string) Option {
	return func(o *Options) {
		o.Address = addr
	}
}

// WithDelimiter sets the message delimiter
func WithDelimiter(delimiter []byte) Option {
	return func(o *Options) {
		o.Delimiter = delimiter
	}
}
