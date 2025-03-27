package client

import (
	"bytes"
	"context"
	"crypto/tls"
	"fmt"
	"io"
	"net"

	"github.com/quic-go/quic-go"
)

// QUICClient implements a QUIC connection client
type QUICClient struct {
	conn      quic.Connection
	stream    quic.Stream
	addr      string
	delimiter []byte
	buffer    []byte
	config    *ClientConfig
}

// NewQUICClient creates a new QUIC client with the given address
func NewQUICClient(addr string, config *ClientConfig) (*QUICClient, error) {
	if addr == "" {
		return nil, fmt.Errorf("address is required")
	}

	return &QUICClient{
		addr:      addr,
		delimiter: config.Delimiter,
		buffer:    make([]byte, 0),
		config:    config,
	}, nil
}

// Connect establishes a QUIC connection
func (c *QUICClient) Connect(ctx context.Context) error {
	quicConfig, ok := c.config.ProtocolConfig.(*QUICConfig)
	if !ok {
		quicConfig = DefaultConfig().ProtocolConfig.(*QUICConfig)
	}

	tlsConf := &tls.Config{
		NextProtos:         quicConfig.NextProtos,
		InsecureSkipVerify: quicConfig.InsecureSkipVerify,
		MinVersion:         quicConfig.MinVersion,
	}

	conn, err := quic.DialAddr(ctx, c.addr, tlsConf, &quic.Config{})
	if err != nil {
		return fmt.Errorf("failed to dial QUIC: %w", err)
	}
	c.conn = conn

	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		return fmt.Errorf("failed to open QUIC stream: %w", err)
	}
	c.stream = stream

	return nil
}

// Send sends data over the QUIC connection
func (c *QUICClient) Send(data []byte) error {
	if c.stream == nil {
		return fmt.Errorf("not connected")
	}

	// Append delimiter to frame the message
	framedData := append(data, c.delimiter...)
	_, err := c.stream.Write(framedData)
	if err != nil {
		return fmt.Errorf("failed to write to QUIC stream: %w", err)
	}

	return nil
}

// Receive receives data from the QUIC connection
func (c *QUICClient) Receive() ([]byte, error) {
	if c.stream == nil {
		return nil, fmt.Errorf("not connected")
	}

	// Read in chunks
	chunk := make([]byte, 1024)
	n, err := c.stream.Read(chunk)
	if err != nil {
		if err == io.EOF {
			return nil, io.EOF
		}
		return nil, fmt.Errorf("failed to read from QUIC stream: %w", err)
	}
	if n == 0 {
		return nil, nil
	}

	c.buffer = append(c.buffer, chunk[:n]...)

	// Look for delimiter
	index := bytes.Index(c.buffer, c.delimiter)
	if index == -1 {
		return nil, nil // Need more data
	}

	// Extract message and update buffer
	message := c.buffer[:index]
	c.buffer = c.buffer[index+len(c.delimiter):]

	return message, nil
}

// Close closes the QUIC connection
func (c *QUICClient) Close() error {
	if c.stream != nil {
		if err := c.stream.Close(); err != nil {
			return fmt.Errorf("failed to close QUIC stream: %w", err)
		}
	}
	if c.conn != nil {
		c.conn.CloseWithError(0, "client closed connection")
	}
	return nil
}

// LocalAddr returns the local network address
func (c *QUICClient) LocalAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote network address
func (c *QUICClient) RemoteAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.RemoteAddr()
}
