package gophersocks

import (
	"crypto/tls"
	"net/http"
	"time"

	"github.com/A13xB0/GopherSocks/framing"
	"github.com/A13xB0/GopherSocks/listener"
	"github.com/quic-go/quic-go"
)

// ServerOptFunc configures a server. It is interchangeable with listener.ServerOption.
type ServerOptFunc func(config *listener.ServerConfig)

// WebSocketConfig is the WebSocket-specific configuration.
type WebSocketConfig = listener.WebSocketConfig

// WithMaxLength sets the maximum message length for any protocol
func WithMaxLength(length uint32) ServerOptFunc {
	return ServerOptFunc(listener.WithMaxLength(length))
}

// WithBufferSize sets the capacity of the channel returned by Session.Data.
func WithBufferSize(size int) ServerOptFunc {
	return ServerOptFunc(listener.WithBufferSize(size))
}

// WithSendQueueSize sets how many outbound messages a session buffers.
func WithSendQueueSize(size int) ServerOptFunc {
	return ServerOptFunc(listener.WithSendQueueSize(size))
}

// WithTimeouts sets read and write timeouts for any protocol
func WithTimeouts(read, write time.Duration) ServerOptFunc {
	return ServerOptFunc(listener.WithTimeouts(read, write))
}

// WithMaxConnections sets the maximum number of concurrent connections
func WithMaxConnections(max int) ServerOptFunc {
	return ServerOptFunc(listener.WithMaxConnections(max))
}

// WithLogger sets a custom logger implementation
func WithLogger(logger listener.Logger) ServerOptFunc {
	return ServerOptFunc(listener.WithLogger(logger))
}

// WithWebSocketBufferSizes sets the WebSocket read and write buffer sizes.
func WithWebSocketBufferSizes(readSize, writeSize int) ServerOptFunc {
	return ServerOptFunc(listener.WithWebSocketBufferSizes(readSize, writeSize))
}

// WithWebSocketPath sets the URL path of the WebSocket endpoint.
func WithWebSocketPath(path string) ServerOptFunc {
	return ServerOptFunc(listener.WithWebSocketPath(path))
}

// WithWebSocketCheckOrigin sets the origin check for WebSocket upgrades.
func WithWebSocketCheckOrigin(fn func(r *http.Request) bool) ServerOptFunc {
	return ServerOptFunc(listener.WithWebSocketCheckOrigin(fn))
}

// WithTLSConfig sets the QUIC server's TLS configuration.
func WithTLSConfig(c *tls.Config) ServerOptFunc {
	return ServerOptFunc(listener.WithTLSConfig(c))
}

// WithQUICConfig sets the QUIC server's quic-go configuration.
func WithQUICConfig(c *quic.Config) ServerOptFunc {
	return ServerOptFunc(listener.WithQUICConfig(c))
}

// WithQUICCodec accepts clients that negotiate ALPN protocol alpn and frames
// their messages with codec, preferring it over earlier protocols.
func WithQUICCodec(alpn string, codec framing.Codec) ServerOptFunc {
	return ServerOptFunc(listener.WithQUICCodec(alpn, codec))
}
