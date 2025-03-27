package gophersocks

import (
	"context"
	"fmt"
	"net"

	"github.com/A13xB0/GopherSocks/client"
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

// NewQUICClient creates a new QUIC client with the given address and options
func NewQUICClient(addr string, opts ...ClientOptFunc) (Client, error) {
	if addr == "" {
		return nil, fmt.Errorf("address is required")
	}

	config := NewClientConfig(opts...)
	return client.NewQUICClient(addr, config)
}
