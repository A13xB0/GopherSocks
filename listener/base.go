package listener

import (
	"context"
	"crypto/tls"
	"net"
	"sync"
	"time"

	"github.com/quic-go/quic-go"
)

// BaseSession provides common session functionality for all protocol implementations
type BaseSession struct {
	ID            string
	ClientAddr    net.Addr
	DataChannel   chan []byte
	LastReceived  time.Time
	receivedMutex sync.RWMutex
	ctx           context.Context
	cancel        context.CancelFunc
	logger        Logger
}

// NewBaseSession creates a new base session with the given parameters
func NewBaseSession(addr net.Addr, ctx context.Context, logger Logger, config *ServerConfig) *BaseSession {
	sessionCtx, cancel := context.WithCancel(ctx)
	return &BaseSession{
		ClientAddr:   addr,
		DataChannel:  make(chan []byte, config.BufferSize),
		LastReceived: time.Now(),
		ctx:          sessionCtx,
		cancel:       cancel,
		logger:       logger,
	}
}

// GetSessionID returns the unique session identifier
func (s *BaseSession) GetSessionID() string {
	return s.ID
}

// GetClientAddr returns the client's network address
func (s *BaseSession) GetClientAddr() net.Addr {
	return s.ClientAddr
}

// GetLastReceived returns the timestamp of the last received data
func (s *BaseSession) GetLastReceived() time.Time {
	s.receivedMutex.RLock()
	defer s.receivedMutex.RUnlock()
	return s.LastReceived
}

// updateLastReceived updates the last received timestamp with the current time
func (s *BaseSession) updateLastReceived() {
	s.receivedMutex.Lock()
	s.LastReceived = time.Now()
	s.receivedMutex.Unlock()
}

// Data returns the channel for receiving data from the client
func (s *BaseSession) Data() chan []byte {
	return s.DataChannel
}

// Context returns the session's context
func (s *BaseSession) Context() context.Context {
	return s.ctx
}

// Cancel cancels the session's context
func (s *BaseSession) Cancel() {
	s.cancel()
}

// Logger defines the interface for logging operations
type Logger interface {
	Debug(msg string, keysAndValues ...interface{})
	Info(msg string, keysAndValues ...interface{})
	Warn(msg string, keysAndValues ...interface{})
	Error(msg string, keysAndValues ...interface{})
}

// DefaultLogger provides a basic implementation of the Logger interface
type DefaultLogger struct{}

func (l *DefaultLogger) Debug(msg string, keysAndValues ...interface{}) {

}
func (l *DefaultLogger) Info(msg string, keysAndValues ...interface{}) {

}
func (l *DefaultLogger) Warn(msg string, keysAndValues ...interface{}) {

}
func (l *DefaultLogger) Error(msg string, keysAndValues ...interface{}) {

}

// ServerOption defines a function type for configuring server options
type ServerOption func(*ServerConfig)

// Protocol-specific configurations
type WebSocketConfig struct {
	ReadBufferSize  int
	WriteBufferSize int
	Path            string
}

// QUICConfig holds QUIC-specific configuration
type QUICConfig struct {
	TLSConfig  *tls.Config
	QUICConfig *quic.Config
	Delimiter  []byte
}

// ServerConfig holds common configuration for all protocol servers
type ServerConfig struct {
	MaxLength      uint32
	BufferSize     int
	ReadTimeout    time.Duration
	WriteTimeout   time.Duration
	Logger         Logger
	MaxConnections int
	ProtocolConfig interface{} // Protocol-specific configuration
}

// defaultConfig returns a ServerConfig with default values
func defaultConfig() *ServerConfig {
	return &ServerConfig{
		MaxLength:      1024 * 1024, // 1MB
		BufferSize:     100,
		ReadTimeout:    time.Second * 30,
		WriteTimeout:   time.Second * 30,
		Logger:         &DefaultLogger{},
		MaxConnections: 1000,
		ProtocolConfig: &WebSocketConfig{
			ReadBufferSize:  1024,
			WriteBufferSize: 1024,
			Path:            "/ws",
		},
		// Default QUIC configuration will be set in NewQUIC
	}
}

// WithMaxLength sets the maximum message length
func WithMaxLength(length uint32) ServerOption {
	return func(c *ServerConfig) {
		c.MaxLength = length
	}
}

// WithBufferSize sets the channel buffer size
func WithBufferSize(size int) ServerOption {
	return func(c *ServerConfig) {
		c.BufferSize = size
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

// WithQUICDelimiter sets the delimiter for QUIC messages
func WithQUICDelimiter(delimiter []byte) ServerOption {
	return func(config *ServerConfig) {
		if quicConfig, ok := config.ProtocolConfig.(*QUICConfig); ok {
			quicConfig.Delimiter = delimiter
		}
	}
}
