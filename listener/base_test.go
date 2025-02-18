package listener

import (
	"context"
	"net"
	"testing"
	"time"
)

// getTestConfig returns a ServerConfig for testing
func getTestConfig() *ServerConfig {
	return &ServerConfig{
		MaxLength:      1024 * 1024,
		BufferSize:     100,
		ReadTimeout:    time.Second * 30,
		WriteTimeout:   time.Second * 30,
		Logger:         &DefaultLogger{},
		MaxConnections: 1000,
	}
}

func TestBaseSession(t *testing.T) {
	t.Run("Session initialization", func(t *testing.T) {
		addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
		logger := &DefaultLogger{}
		session := NewBaseSession(addr, context.Background(), logger, getTestConfig())

		if session.ClientAddr != addr {
			t.Errorf("expected client address %v, got %v", addr, session.ClientAddr)
		}

		if session.DataChannel == nil {
			t.Error("data channel not initialized")
		}

		if session.ctx == nil {
			t.Error("context not initialized")
		}

		if session.cancel == nil {
			t.Error("cancel function not initialized")
		}
	})

	t.Run("Last received time", func(t *testing.T) {
		addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
		session := NewBaseSession(addr, context.Background(), &DefaultLogger{}, getTestConfig())

		initialTime := session.GetLastReceived()
		time.Sleep(time.Millisecond * 10)
		session.updateLastReceived()
		updatedTime := session.GetLastReceived()

		if !updatedTime.After(initialTime) {
			t.Error("last received time not updated")
		}
	})

	t.Run("Context cancellation", func(t *testing.T) {
		addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
		session := NewBaseSession(addr, context.Background(), &DefaultLogger{}, getTestConfig())

		session.Cancel()
		select {
		case <-session.Context().Done():
			// Expected behavior
		default:
			t.Error("context not cancelled")
		}
	})

	t.Run("Data channel operations", func(t *testing.T) {
		addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8080}
		session := NewBaseSession(addr, context.Background(), &DefaultLogger{}, getTestConfig())

		data := []byte("test data")
		go func() {
			session.DataChannel <- data
		}()

		select {
		case received := <-session.Data():
			if string(received) != string(data) {
				t.Errorf("expected data %s, got %s", data, received)
			}
		case <-time.After(time.Second):
			t.Error("timeout waiting for data")
		}
	})
}

func TestServerConfig(t *testing.T) {
	t.Run("Default configuration", func(t *testing.T) {
		config := defaultConfig()

		if config.MaxLength != 1024*1024 {
			t.Errorf("expected max length %d, got %d", 1024*1024, config.MaxLength)
		}

		if config.BufferSize != 100 {
			t.Errorf("expected buffer size %d, got %d", 100, config.BufferSize)
		}

		if config.MaxConnections != 1000 {
			t.Errorf("expected max connections %d, got %d", 1000, config.MaxConnections)
		}
	})

	t.Run("Configuration options", func(t *testing.T) {
		config := defaultConfig()

		// Test MaxLength option
		maxLength := uint32(500)
		WithMaxLength(maxLength)(config)
		if config.MaxLength != maxLength {
			t.Errorf("WithMaxLength: expected %d, got %d", maxLength, config.MaxLength)
		}

		// Test BufferSize option
		bufferSize := 200
		WithBufferSize(bufferSize)(config)
		if config.BufferSize != bufferSize {
			t.Errorf("WithBufferSize: expected %d, got %d", bufferSize, config.BufferSize)
		}

		// Test MaxConnections option
		maxConns := 50
		WithMaxConnections(maxConns)(config)
		if config.MaxConnections != maxConns {
			t.Errorf("WithMaxConnections: expected %d, got %d", maxConns, config.MaxConnections)
		}

		// Test Timeouts option
		readTimeout := time.Second * 10
		writeTimeout := time.Second * 20
		WithTimeouts(readTimeout, writeTimeout)(config)
		if config.ReadTimeout != readTimeout || config.WriteTimeout != writeTimeout {
			t.Error("WithTimeouts: timeouts not set correctly")
		}
	})

	t.Run("Configuration validation", func(t *testing.T) {
		tests := []struct {
			name        string
			config      *ServerConfig
			expectError bool
		}{
			{
				name:        "Valid config",
				config:      defaultConfig(),
				expectError: false,
			},
			{
				name: "Zero max length",
				config: &ServerConfig{
					MaxLength:      0,
					BufferSize:     100,
					MaxConnections: 1000,
				},
				expectError: true,
			},
			{
				name: "Zero buffer size",
				config: &ServerConfig{
					MaxLength:      1024,
					BufferSize:     0,
					MaxConnections: 1000,
				},
				expectError: true,
			},
			{
				name: "Zero max connections",
				config: &ServerConfig{
					MaxLength:      1024,
					BufferSize:     100,
					MaxConnections: 0,
				},
				expectError: true,
			},
			{
				name: "Negative timeouts",
				config: &ServerConfig{
					MaxLength:      1024,
					BufferSize:     100,
					MaxConnections: 1000,
					ReadTimeout:    -1,
					WriteTimeout:   -1,
				},
				expectError: true,
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := ValidateConfig(tt.config)
				if tt.expectError && err == nil {
					t.Error("expected validation error, got nil")
				}
				if !tt.expectError && err != nil {
					t.Errorf("unexpected validation error: %v", err)
				}
			})
		}
	})
}
