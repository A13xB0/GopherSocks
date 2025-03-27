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

	dataLen := len(data)
	if dataLen > 10000 {
		return fmt.Errorf("data length exceeds maximum of 10000 bytes")
	}

	// Format: data + 2-byte length + delimiter
	lengthBytes := []byte{byte(dataLen >> 8), byte(dataLen)}
	framedData := append(data, lengthBytes...)
	framedData = append(framedData, c.delimiter...)
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

	chunk := make([]byte, 1024)
	for {
		// Try to process any complete messages in the buffer
		for {
			// Look for delimiter
			delimiterIndex := bytes.Index(c.buffer, c.delimiter)
			if delimiterIndex == -1 {
				break // No delimiter found, need more data
			}

			// Need at least 2 bytes before delimiter for length
			if delimiterIndex < 2 {
				// Invalid format, remove up to delimiter and continue
				c.buffer = c.buffer[delimiterIndex+len(c.delimiter):]
				continue
			}

			// Extract the 2-byte length
			lengthBytes := c.buffer[delimiterIndex-2 : delimiterIndex]
			length := int(lengthBytes[0])<<8 | int(lengthBytes[1])

			// Verify length is within bounds
			if length <= 0 || length > 10000 {
				// Invalid length, remove up to delimiter and continue
				c.buffer = c.buffer[delimiterIndex+len(c.delimiter):]
				continue
			}

			// Verify we have enough data
			messageStart := delimiterIndex - 2 - length
			if messageStart < 0 {
				break // Need more data
			}

			// Extract the message
			message := c.buffer[messageStart : delimiterIndex-2]
			// Remove processed data including delimiter
			c.buffer = c.buffer[delimiterIndex+len(c.delimiter):]
			return message, nil
		}

		// Read more data
		n, err := c.stream.Read(chunk)
		if err != nil {
			if err == io.EOF {
				// If we have remaining data, try to process it
				if len(c.buffer) > 0 {
					delimiterIndex := bytes.Index(c.buffer, c.delimiter)
					if delimiterIndex != -1 && delimiterIndex >= 2 {
						lengthBytes := c.buffer[delimiterIndex-2 : delimiterIndex]
						length := int(lengthBytes[0])<<8 | int(lengthBytes[1])
						if length > 0 && length <= 10000 {
							messageStart := delimiterIndex - 2 - length
							if messageStart >= 0 {
								message := c.buffer[messageStart : delimiterIndex-2]
								c.buffer = nil
								return message, nil
							}
						}
					}
				}
				return nil, io.EOF
			}
			return nil, fmt.Errorf("failed to read from QUIC stream: %w", err)
		}
		if n == 0 {
			continue
		}

		c.buffer = append(c.buffer, chunk[:n]...)
	}
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
