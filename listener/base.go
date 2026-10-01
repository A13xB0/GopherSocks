package listener

import (
	"crypto/tls"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/A13xB0/GopherSocks/framing"
	"github.com/quic-go/quic-go"
)

// Logger defines the interface for logging operations. Messages are constant
// strings; details are passed as key/value pairs.
type Logger interface {
	Debug(msg string, keysAndValues ...any)
	Info(msg string, keysAndValues ...any)
	Warn(msg string, keysAndValues ...any)
	Error(msg string, keysAndValues ...any)
}

// DefaultLogger discards everything.
type DefaultLogger struct{}

// Debug discards the message.
func (*DefaultLogger) Debug(string, ...any) {
	// Logging is disabled unless a Logger is configured.
}

// Info discards the message.
func (*DefaultLogger) Info(string, ...any) {
	// Logging is disabled unless a Logger is configured.
}

// Warn discards the message.
func (*DefaultLogger) Warn(string, ...any) {
	// Logging is disabled unless a Logger is configured.
}

// Error discards the message.
func (*DefaultLogger) Error(string, ...any) {
	// Logging is disabled unless a Logger is configured.
}

// NewSlogLogger adapts a *slog.Logger to Logger.
func NewSlogLogger(l *slog.Logger) Logger {
	return slogLogger{l: l}
}

type slogLogger struct{ l *slog.Logger }

func (s slogLogger) Debug(msg string, kv ...any) { s.l.Debug(msg, kv...) }
func (s slogLogger) Info(msg string, kv ...any)  { s.l.Info(msg, kv...) }
func (s slogLogger) Warn(msg string, kv ...any)  { s.l.Warn(msg, kv...) }
func (s slogLogger) Error(msg string, kv ...any) { s.l.Error(msg, kv...) }

// ServerOption defines a function type for configuring server options
type ServerOption func(*ServerConfig)

// WebSocketConfig holds WebSocket-specific configuration.
type WebSocketConfig struct {
	ReadBufferSize  int
	WriteBufferSize int
	Path            string
	// CheckOrigin decides whether to accept a browser's cross-origin request.
	// nil accepts every origin, as before.
	CheckOrigin func(r *http.Request) bool
}

// DefaultALPN is the ALPN protocol of the original GopherSocks QUIC framing.
const DefaultALPN = "gophersocks"

// QUICConfig holds QUIC-specific configuration.
type QUICConfig struct {
	// TLSConfig is used as-is if set (NextProtos is filled from ALPN when
	// empty). If nil, an ephemeral self-signed ECDSA certificate is created,
	// which suits development only.
	TLSConfig  *tls.Config
	QUICConfig *quic.Config
	// Delimiter sets the Legacy codec's delimiter for DefaultALPN.
	Delimiter []byte
	// ALPN lists the protocols the server accepts, most preferred first.
	// Each needs a codec in Codecs.
	ALPN []string
	// Codecs maps an ALPN protocol to its framing.
	Codecs map[string]framing.Codec
}

// ServerConfig holds common configuration for all protocol servers
type ServerConfig struct {
	MaxLength uint32
	// BufferSize is the capacity of the channel returned by Session.Data.
	BufferSize int
	// ReadTimeout closes a TCP or WebSocket connection, or a UDP session,
	// that sends nothing for this long. QUIC uses its own idle timeout.
	ReadTimeout  time.Duration
	WriteTimeout time.Duration
	Logger       Logger
	// MaxConnections is the most sessions served at once; more are rejected
	// one by one while the server keeps running.
	MaxConnections int
	// SendQueueSize is how many outbound messages a session buffers. When
	// it is full, SendToClient waits up to WriteTimeout before closing the
	// session as a slow consumer.
	SendQueueSize  int
	ProtocolConfig any // *QUICConfig or *WebSocketConfig
}

const (
	defaultSendQueueSize = 1024
	defaultMaxLength     = 1024 * 1024
)

// defaultConfig returns a ServerConfig with default values
func defaultConfig() *ServerConfig {
	return &ServerConfig{
		MaxLength:      defaultMaxLength,
		BufferSize:     100,
		ReadTimeout:    time.Second * 30,
		WriteTimeout:   time.Second * 30,
		Logger:         &DefaultLogger{},
		MaxConnections: 1000,
		SendQueueSize:  defaultSendQueueSize,
		ProtocolConfig: &WebSocketConfig{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			Path:            "/ws",
		},
	}
}

func defaultQUICConfig() *QUICConfig {
	return &QUICConfig{
		QUICConfig: &quic.Config{
			MaxIncomingStreams:    4, // a session uses one stream
			MaxIncomingUniStreams: -1,
		},
		Delimiter: framing.DefaultDelimiter,
		ALPN:      []string{DefaultALPN},
		Codecs:    map[string]framing.Codec{},
	}
}

// WithMaxLength sets the maximum message length
func WithMaxLength(length uint32) ServerOption {
	return func(c *ServerConfig) {
		c.MaxLength = length
	}
}

// WithBufferSize sets the capacity of the channel returned by Session.Data.
func WithBufferSize(size int) ServerOption {
	return func(c *ServerConfig) {
		c.BufferSize = size
	}
}

// WithSendQueueSize sets how many outbound messages a session buffers.
func WithSendQueueSize(size int) ServerOption {
	return func(c *ServerConfig) {
		c.SendQueueSize = size
	}
}

// WithLogger sets the logger implementation
func WithLogger(logger Logger) ServerOption {
	return func(c *ServerConfig) {
		c.Logger = logger
	}
}

// WithTimeouts sets read and write timeouts
func WithTimeouts(read, write time.Duration) ServerOption {
	return func(c *ServerConfig) {
		c.ReadTimeout = read
		c.WriteTimeout = write
	}
}

// WithMaxConnections sets the maximum number of concurrent connections
func WithMaxConnections(max int) ServerOption {
	return func(c *ServerConfig) {
		c.MaxConnections = max
	}
}

// WithWebSocketBufferSizes sets the WebSocket read and write buffer sizes
func WithWebSocketBufferSizes(readSize, writeSize int) ServerOption {
	return func(config *ServerConfig) {
		if wsConfig, ok := config.ProtocolConfig.(*WebSocketConfig); ok {
			wsConfig.ReadBufferSize = readSize
			wsConfig.WriteBufferSize = writeSize
		}
	}
}

// WithWebSocketPath sets the WebSocket endpoint path
func WithWebSocketPath(path string) ServerOption {
	return func(config *ServerConfig) {
		if wsConfig, ok := config.ProtocolConfig.(*WebSocketConfig); ok {
			wsConfig.Path = path
		}
	}
}

// WithWebSocketCheckOrigin sets the origin check for WebSocket upgrades.
func WithWebSocketCheckOrigin(fn func(r *http.Request) bool) ServerOption {
	return func(config *ServerConfig) {
		if wsConfig, ok := config.ProtocolConfig.(*WebSocketConfig); ok {
			wsConfig.CheckOrigin = fn
		}
	}
}

// WithTLSConfig sets the TLS configuration for QUIC
func WithTLSConfig(tlsConfig *tls.Config) ServerOption {
	return func(config *ServerConfig) {
		if quicConfig, ok := config.ProtocolConfig.(*QUICConfig); ok {
			quicConfig.TLSConfig = tlsConfig
		}
	}
}

// WithQUICConfig sets the QUIC configuration
func WithQUICConfig(quicConfig *quic.Config) ServerOption {
	return func(config *ServerConfig) {
		if qConfig, ok := config.ProtocolConfig.(*QUICConfig); ok {
			qConfig.QUICConfig = quicConfig
		}
	}
}

// WithQUICDelimiter sets the delimiter of the Legacy framing used for the
// default "gophersocks" ALPN protocol.
func WithQUICDelimiter(delimiter []byte) ServerOption {
	return func(config *ServerConfig) {
		if quicConfig, ok := config.ProtocolConfig.(*QUICConfig); ok {
			quicConfig.Delimiter = delimiter
		}
	}
}

// WithQUICCodec accepts clients that negotiate the ALPN protocol alpn and
// frames their messages with codec. The newest option added is preferred,
// so a server can offer a new format and still accept the default one:
//
//	listener.WithQUICCodec("nixie/2", framing.Uvarint)
func WithQUICCodec(alpn string, codec framing.Codec) ServerOption {
	return func(config *ServerConfig) {
		q, ok := config.ProtocolConfig.(*QUICConfig)
		if !ok {
			return
		}
		if q.Codecs == nil {
			q.Codecs = map[string]framing.Codec{}
		}
		q.Codecs[alpn] = codec
		alpns := []string{alpn}
		for _, p := range q.ALPN {
			if p != alpn {
				alpns = append(alpns, p)
			}
		}
		q.ALPN = alpns
	}
}

// WithQUICDatagrams lets QUIC clients that also enable them receive
// datagrams (see DatagramSender).
func WithQUICDatagrams() ServerOption {
	return func(config *ServerConfig) {
		q, ok := config.ProtocolConfig.(*QUICConfig)
		if !ok {
			return
		}
		if q.QUICConfig == nil {
			q.QUICConfig = &quic.Config{}
		}
		q.QUICConfig.EnableDatagrams = true
	}
}

// hostPort joins host and port. host may be an IPv6 address with or without
// brackets ("::1" or "[::1]"), as earlier versions accepted both.
func hostPort(host string, port uint16) string {
	return net.JoinHostPort(strings.TrimSuffix(strings.TrimPrefix(host, "["), "]"), strconv.Itoa(int(port)))
}
