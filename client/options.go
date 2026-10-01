package client

import (
	"time"

	"github.com/A13xB0/GopherSocks/framing"
)

// DefaultALPN is the ALPN protocol of the original GopherSocks QUIC framing.
const DefaultALPN = "gophersocks"

// QUICConfig holds QUIC-specific client configuration.
type QUICConfig struct {
	InsecureSkipVerify bool
	// NextProtos lists the ALPN protocols to offer, most preferred first.
	NextProtos []string
	MinVersion uint16
	// Codecs maps an ALPN protocol to its framing. DefaultALPN uses the
	// Legacy framing with ClientConfig.Delimiter unless set here.
	Codecs map[string]framing.Codec
	// Datagrams asks for QUIC datagrams; read them with ReceiveDatagram.
	Datagrams bool
}

// ClientConfig holds common configuration for all protocol clients
type ClientConfig struct {
	// Delimiter is the Legacy framing's terminator (default: "\n\n\n")
	Delimiter []byte
	// ReadTimeout is the timeout for read operations
	ReadTimeout time.Duration
	// WriteTimeout is the timeout for write operations
	WriteTimeout time.Duration
	// BufferSize is the size of the read buffer
	BufferSize int
	// MaxLength is the largest message Receive accepts.
	MaxLength int
	// ProtocolConfig holds protocol-specific configuration
	ProtocolConfig any
}

// DefaultConfig returns a ClientConfig with default values
func DefaultConfig() *ClientConfig {
	return &ClientConfig{
		Delimiter:    framing.DefaultDelimiter,
		ReadTimeout:  time.Second * 30,
		WriteTimeout: time.Second * 30,
		BufferSize:   64 * 1024,
		MaxLength:    1024 * 1024,
		ProtocolConfig: &QUICConfig{
			InsecureSkipVerify: true,
			NextProtos:         []string{DefaultALPN},
			MinVersion:         0x0304, // TLS 1.3
		},
	}
}
