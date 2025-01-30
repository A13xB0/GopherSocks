package gophersocks

import (
	"time"

	"github.com/A13xB0/GopherSocks/listener"
)

// Common server options that apply to all protocols
type ServerOptFunc func(config *listener.ServerConfig)

// WithMaxLength sets the maximum message length for any protocol
func WithMaxLength(length uint32) ServerOptFunc {
	return func(config *listener.ServerConfig) {
		config.MaxLength = length
	}
}

// WithBufferSize sets the channel buffer size for any protocol
func WithBufferSize(size int) ServerOptFunc {
	return func(config *listener.ServerConfig) {
		config.BufferSize = size
	}
}

// WithTimeouts sets read and write timeouts for any protocol
func WithTimeouts(read, write time.Duration) ServerOptFunc {
	return func(config *listener.ServerConfig) {
		config.ReadTimeout = read
		config.WriteTimeout = write
	}
}

// WithMaxConnections sets the maximum number of concurrent connections
func WithMaxConnections(max int) ServerOptFunc {
	return func(config *listener.ServerConfig) {
		config.MaxConnections = max
	}
}

// WithLogger sets a custom logger implementation
func WithLogger(logger listener.Logger) ServerOptFunc {
	return func(config *listener.ServerConfig) {
		config.Logger = logger
	}
}

// Protocol-specific options

// WebSocket specific configuration
type WebSocketConfig struct {
	ReadBufferSize  int
	WriteBufferSize int
	Path            string // URL path for the WebSocket endpoint
}

// WebSocket specific options
func WithWebSocketBufferSizes(readSize, writeSize int) ServerOptFunc {
	return func(config *listener.ServerConfig) {
		if wsConfig, ok := config.ProtocolConfig.(*WebSocketConfig); ok {
			wsConfig.ReadBufferSize = readSize
			wsConfig.WriteBufferSize = writeSize
		}
	}
}

func WithWebSocketPath(path string) ServerOptFunc {
	return func(config *listener.ServerConfig) {
		if wsConfig, ok := config.ProtocolConfig.(*WebSocketConfig); ok {
			wsConfig.Path = path
		}
	}
}

// Helper function to convert old-style options to new ServerConfig
func convertToServerConfig(opts ...ServerOptFunc) *listener.ServerConfig {
	config := &listener.ServerConfig{
		MaxLength:      1024 * 1024, // 1MB default
		BufferSize:     100,         // Default channel buffer size
		ReadTimeout:    time.Second * 30,
		WriteTimeout:   time.Second * 30,
		MaxConnections: 1000,
		Logger:         &listener.DefaultLogger{},
	}

	for _, opt := range opts {
		opt(config)
	}

	return config
}
