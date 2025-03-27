package client

import (
	"context"
	"fmt"
	"net"
)

// UDPClient implements a UDP connection client
type UDPClient struct {
	conn   *net.UDPConn
	addr   string
	config *ClientConfig
}

// NewUDPClient creates a new UDP client with the given address
func NewUDPClient(addr string, config *ClientConfig) (*UDPClient, error) {
	if addr == "" {
		return nil, fmt.Errorf("address is required")
	}

	return &UDPClient{
		addr:   addr,
		config: config,
	}, nil
}

// Connect establishes a UDP connection
func (c *UDPClient) Connect(ctx context.Context) error {
	udpAddr, err := net.ResolveUDPAddr("udp", c.addr)
	if err != nil {
		return fmt.Errorf("failed to resolve UDP address: %w", err)
	}

	conn, err := net.DialUDP("udp", nil, udpAddr)
	if err != nil {
		return fmt.Errorf("failed to dial UDP: %w", err)
	}
	c.conn = conn

	return nil
}

// Send sends data over the UDP connection
func (c *UDPClient) Send(data []byte) error {
	if c.conn == nil {
		return fmt.Errorf("not connected")
	}

	_, err := c.conn.Write(data)
	if err != nil {
		return fmt.Errorf("failed to write to UDP connection: %w", err)
	}

	return nil
}

// Receive receives data from the UDP connection
func (c *UDPClient) Receive() ([]byte, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	buffer := make([]byte, c.config.BufferSize)
	n, err := c.conn.Read(buffer)
	if err != nil {
		return nil, fmt.Errorf("failed to read from UDP connection: %w", err)
	}

	return buffer[:n], nil
}

// Close closes the UDP connection
func (c *UDPClient) Close() error {
	if c.conn != nil {
		if err := c.conn.Close(); err != nil {
			return fmt.Errorf("failed to close UDP connection: %w", err)
		}
	}
	return nil
}

// LocalAddr returns the local network address
func (c *UDPClient) LocalAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote network address
func (c *UDPClient) RemoteAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.RemoteAddr()
}
