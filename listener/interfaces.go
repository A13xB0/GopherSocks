package listener

import "context"

// Handler serves one session on its own goroutine. The session is closed when
// the handler returns.
type Handler func(ctx context.Context, s Session)

// Listener defines the interface for streaming TCP, UDP, QUIC and WebSocket
// servers.
type Listener interface {
	// Listen binds the address now, so bind errors are returned here and
	// clients can connect as soon as it returns. Serve and StartListener
	// call it if it hasn't been.
	Listen() error

	// Serve accepts sessions and runs h for each on its own goroutine until
	// ctx ends or StopListener is called. It binds the address first if
	// StartListener has not.
	Serve(ctx context.Context, h Handler) error

	// StartListener binds the address and serves in the background, calling
	// the function set with SetAnnounceNewSession for each new session.
	StartListener() error

	// StopListener stops accepting, closes every session and waits for them.
	StopListener() error

	// SetAnnounceNewSession sets the callback StartListener runs for each new
	// session. It runs on the session's own goroutine.
	SetAnnounceNewSession(function AnnounceMiddlewareFunc, options any)

	// GetActiveSessions returns the open sessions keyed by client address.
	GetActiveSessions() map[string]Session

	// GetSession returns the session for a client address (IP:Port), or nil.
	GetSession(ClientAddr string) Session
}
