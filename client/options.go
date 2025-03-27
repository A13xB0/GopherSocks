package client

import "time"

// Protocol-specific configurations
type QUICConfig struct {
	InsecureSkipVerify bool
	NextProtos         []string
	MinVersion         uint16
}

// ClientConfig holds common configuration for all protocol clients
type ClientConfig struct {
	// Delimiter is used to frame messages (default: "\n\n\n")
	Delimiter []byte
	// ReadTimeout is the timeout for read operations
	ReadTimeout time.Duration
	// WriteTimeout is the timeout for write operations
	WriteTimeout time.Duration
	// BufferSize is the size of the read buffer
	BufferSize int
	// ProtocolConfig holds protocol-specific configuration
	ProtocolConfig interface{}
}

// DefaultConfig returns a ClientConfig with default values
func DefaultConfig() *ClientConfig {
	return &ClientConfig{
		Delimiter:    []byte("\n\n\n"),
		ReadTimeout:  time.Second * 30,
		WriteTimeout: time.Second * 30,
		BufferSize:   1024,
		ProtocolConfig: &QUICConfig{
			InsecureSkipVerify: true,
			NextProtos:         []string{"gophersocks"},
			MinVersion:         0x0304, // TLS 1.3
		},
	}
}
