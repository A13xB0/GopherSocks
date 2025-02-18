package listener

// Listener defines the interface for streaming TCP and UDP connections
type Listener interface {
	// StartReceiveStream Starts listener for stream transport
	StartListener() error

	// StopReceiveStream Stops listener for stream transport
	StopListener() error

	// SetAnnounceNewSession Sets middleware for announcing a new session
	SetAnnounceNewSession(function AnnounceMiddlewareFunc, options any)

	//Getters

	// GetActiveSessions Get all sessions
	GetActiveSessions() map[string]Session

	// GetSession Get session from ClientAddr (IP:Port)
	GetSession(ClientAddr string) Session
}
