package listener

import (
	"context"
	"iter"
	"net"
	"time"
)

// Session is one client connection. Its methods are safe for concurrent use.
type Session interface {
	// SendToClient queues data for the client and returns without waiting
	// for the write. The session keeps data until it is written: don't
	// modify it afterwards. When SendQueueSize messages are already queued
	// it waits for space, for up to WriteTimeout; a client still that far
	// behind is closed and ErrSlowConsumer returned.
	SendToClient(data []byte) error

	// Messages yields messages from the client as they are read. The next
	// message is read only when the loop asks for it, so a slow consumer
	// slows the client down instead of buffering. It stops when the session
	// closes; use either Messages or Data, not both.
	Messages() iter.Seq[[]byte]

	// Data returns a channel of messages from the client, closed when the
	// session ends. Messages are read ahead into it up to BufferSize.
	Data() (DataFromClient chan []byte)

	// CloseSession closes the session.
	CloseSession()

	// Context ends when the session closes; context.Cause tells why.
	Context() context.Context

	// GetSessionID returns the session's unique ID.
	GetSessionID() string

	// GetClientAddr returns the client's address.
	GetClientAddr() net.Addr

	// GetLastRecieved returns when the last message arrived.
	GetLastRecieved() time.Time
}

// DatagramSender is a Session that can send unreliable QUIC datagrams:
// delivered at most once, possibly out of order or not at all, never held
// up behind lost stream data. Use a type assertion; SendDatagram returns
// ErrDatagramsUnsupported unless the server enables datagrams
// (WithQUICDatagrams) and the client does too.
type DatagramSender interface {
	SendDatagram(data []byte) error
	// DatagramsEnabled reports whether both ends enabled datagrams, so
	// SendDatagram can work.
	DatagramsEnabled() bool
}

// AnnounceMiddlewareFunc is called for each new session started by StartListener.
type AnnounceMiddlewareFunc func(options any, session Session)
