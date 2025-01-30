package listener

import (
	"fmt"
)

// Error types for specific error cases
type ErrorType string

const (
	ErrConnection    ErrorType = "connection"
	ErrConfiguration ErrorType = "configuration"
	ErrProtocol      ErrorType = "protocol"
	ErrSession       ErrorType = "session"
)

// NetworkError represents a network-related error with context
type NetworkError struct {
	Type    ErrorType
	Message string
	Err     error
}

func (e *NetworkError) Error() string {
	if e.Err != nil {
		return fmt.Sprintf("%s error: %s: %v", e.Type, e.Message, e.Err)
	}
	return fmt.Sprintf("%s error: %s", e.Type, e.Message)
}

func (e *NetworkError) Unwrap() error {
	return e.Err
}

// Error constructors for common scenarios
func NewConnectionError(msg string, err error) error {
	return &NetworkError{
		Type:    ErrConnection,
		Message: msg,
		Err:     err,
	}
}

func NewConfigError(msg string, err error) error {
	return &NetworkError{
		Type:    ErrConfiguration,
		Message: msg,
		Err:     err,
	}
}

func NewProtocolError(msg string, err error) error {
	return &NetworkError{
		Type:    ErrProtocol,
		Message: msg,
		Err:     err,
	}
}

func NewSessionError(msg string, err error) error {
	return &NetworkError{
		Type:    ErrSession,
		Message: msg,
		Err:     err,
	}
}

// Validation errors
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation error: %s: %s", e.Field, e.Message)
}

// NewValidationError creates a new validation error
func NewValidationError(field, message string) error {
	return &ValidationError{
		Field:   field,
		Message: message,
	}
}

// ValidateConfig validates server configuration
func ValidateConfig(config *ServerConfig) error {
	if config.MaxLength == 0 {
		return NewValidationError("MaxLength", "must be greater than 0")
	}
	if config.BufferSize < 1 {
		return NewValidationError("BufferSize", "must be at least 1")
	}
	if config.MaxConnections < 1 {
		return NewValidationError("MaxConnections", "must be at least 1")
	}
	if config.ReadTimeout < 0 {
		return NewValidationError("ReadTimeout", "cannot be negative")
	}
	if config.WriteTimeout < 0 {
		return NewValidationError("WriteTimeout", "cannot be negative")
	}
	return nil
}
