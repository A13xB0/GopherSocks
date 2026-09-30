package gophersocks

import (
	"time"

	"github.com/A13xB0/GopherSocks/client"
	"github.com/A13xB0/GopherSocks/framing"
)

// Common client options that apply to all protocols
type ClientOptFunc func(config *client.ClientConfig)

// WithDelimiter sets the message delimiter
func WithDelimiter(delimiter []byte) ClientOptFunc {
	return func(config *client.ClientConfig) {
		config.Delimiter = delimiter
	}
}

// WithClientTimeouts sets read and write timeouts for any protocol
func WithClientTimeouts(read, write time.Duration) ClientOptFunc {
	return func(config *client.ClientConfig) {
		config.ReadTimeout = read
		config.WriteTimeout = write
	}
}

// WithClientBufferSize sets the read buffer size for any protocol
func WithClientBufferSize(size int) ClientOptFunc {
	return func(config *client.ClientConfig) {
		config.BufferSize = size
	}
}

// Protocol-specific options

// QUIC specific options
func WithQUICInsecureSkipVerify(skip bool) ClientOptFunc {
	return func(config *client.ClientConfig) {
		if quicConfig, ok := config.ProtocolConfig.(*client.QUICConfig); ok {
			quicConfig.InsecureSkipVerify = skip
		}
	}
}

func WithQUICNextProtos(protos []string) ClientOptFunc {
	return func(config *client.ClientConfig) {
		if quicConfig, ok := config.ProtocolConfig.(*client.QUICConfig); ok {
			quicConfig.NextProtos = protos
		}
	}
}

func WithQUICMinVersion(version uint16) ClientOptFunc {
	return func(config *client.ClientConfig) {
		if quicConfig, ok := config.ProtocolConfig.(*client.QUICConfig); ok {
			quicConfig.MinVersion = version
		}
	}
}

// Helper function to create a new client config with options
func NewClientConfig(opts ...ClientOptFunc) *client.ClientConfig {
	config := client.DefaultConfig()
	for _, opt := range opts {
		opt(config)
	}
	return config
}

// WithClientQUICCodec offers ALPN protocol alpn, preferred over the protocols
// already offered, and frames messages with codec when the server picks it.
func WithClientQUICCodec(alpn string, codec framing.Codec) ClientOptFunc {
	return func(config *client.ClientConfig) {
		quicConfig, ok := config.ProtocolConfig.(*client.QUICConfig)
		if !ok {
			return
		}
		if quicConfig.Codecs == nil {
			quicConfig.Codecs = map[string]framing.Codec{}
		}
		quicConfig.Codecs[alpn] = codec
		protos := []string{alpn}
		for _, p := range quicConfig.NextProtos {
			if p != alpn {
				protos = append(protos, p)
			}
		}
		quicConfig.NextProtos = protos
	}
}

// WithClientMaxLength sets the largest message Receive accepts.
func WithClientMaxLength(n int) ClientOptFunc {
	return func(config *client.ClientConfig) {
		config.MaxLength = n
	}
}
