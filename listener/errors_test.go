package listener

import (
	"errors"
	"strings"
	"testing"
)

func TestNetworkError(t *testing.T) {
	t.Run("Error with cause", func(t *testing.T) {
		cause := errors.New("underlying error")
		err := NewConnectionError("connection failed", cause)

		if !strings.Contains(err.Error(), "connection error") {
			t.Error("error string missing error type")
		}
		if !strings.Contains(err.Error(), "connection failed") {
			t.Error("error string missing message")
		}
		if !strings.Contains(err.Error(), cause.Error()) {
			t.Error("error string missing underlying cause")
		}

		// Test error unwrapping
		unwrapped := errors.Unwrap(err)
		if unwrapped != cause {
			t.Error("error unwrap failed")
		}
	})

	t.Run("Error without cause", func(t *testing.T) {
		err := NewConnectionError("connection failed", nil)

		if !strings.Contains(err.Error(), "connection error") {
			t.Error("error string missing error type")
		}
		if !strings.Contains(err.Error(), "connection failed") {
			t.Error("error string missing message")
		}

		// Test error unwrapping with nil cause
		unwrapped := errors.Unwrap(err)
		if unwrapped != nil {
			t.Error("expected nil unwrapped error")
		}
	})

	t.Run("Error constructors", func(t *testing.T) {
		tests := []struct {
			name     string
			err      error
			contains string
		}{
			{
				name:     "Connection error",
				err:      NewConnectionError("test", nil),
				contains: "connection error",
			},
			{
				name:     "Configuration error",
				err:      NewConfigError("test", nil),
				contains: "configuration error",
			},
			{
				name:     "Protocol error",
				err:      NewProtocolError("test", nil),
				contains: "protocol error",
			},
			{
				name:     "Session error",
				err:      NewSessionError("test", nil),
				contains: "session error",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				if !strings.Contains(tt.err.Error(), tt.contains) {
					t.Errorf("error string missing %q", tt.contains)
				}
			})
		}
	})
}

func TestValidationError(t *testing.T) {
	t.Run("Error formatting", func(t *testing.T) {
		err := NewValidationError("MaxLength", "must be greater than 0")

		expected := "validation error: MaxLength: must be greater than 0"
		if err.Error() != expected {
			t.Errorf("expected error %q, got %q", expected, err.Error())
		}
	})

	t.Run("Config validation", func(t *testing.T) {
		tests := []struct {
			name        string
			config      *ServerConfig
			wantField   string
			wantMessage string
		}{
			{
				name: "Invalid max length",
				config: &ServerConfig{
					MaxLength:      0,
					BufferSize:     100,
					MaxConnections: 1000,
				},
				wantField:   "MaxLength",
				wantMessage: "must be greater than 0",
			},
			{
				name: "Invalid buffer size",
				config: &ServerConfig{
					MaxLength:      1024,
					BufferSize:     0,
					MaxConnections: 1000,
				},
				wantField:   "BufferSize",
				wantMessage: "must be at least 1",
			},
			{
				name: "Invalid max connections",
				config: &ServerConfig{
					MaxLength:      1024,
					BufferSize:     100,
					MaxConnections: 0,
				},
				wantField:   "MaxConnections",
				wantMessage: "must be at least 1",
			},
			{
				name: "Invalid read timeout",
				config: &ServerConfig{
					MaxLength:      1024,
					BufferSize:     100,
					MaxConnections: 1000,
					ReadTimeout:    -1,
				},
				wantField:   "ReadTimeout",
				wantMessage: "cannot be negative",
			},
			{
				name: "Invalid write timeout",
				config: &ServerConfig{
					MaxLength:      1024,
					BufferSize:     100,
					MaxConnections: 1000,
					WriteTimeout:   -1,
				},
				wantField:   "WriteTimeout",
				wantMessage: "cannot be negative",
			},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				err := ValidateConfig(tt.config)
				if err == nil {
					t.Fatal("expected validation error, got nil")
				}

				validationErr, ok := err.(*ValidationError)
				if !ok {
					t.Fatal("error is not a ValidationError")
				}

				if validationErr.Field != tt.wantField {
					t.Errorf("expected field %q, got %q", tt.wantField, validationErr.Field)
				}
				if validationErr.Message != tt.wantMessage {
					t.Errorf("expected message %q, got %q", tt.wantMessage, validationErr.Message)
				}
			})
		}
	})
}
