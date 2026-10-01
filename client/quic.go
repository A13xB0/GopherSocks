package client

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"sync"

	"github.com/A13xB0/GopherSocks/framing"
	"github.com/quic-go/quic-go"
)

// ErrNotConnected is returned before Connect succeeds.
var ErrNotConnected = errors.New("gophersocks client: not connected")

// QUICClient implements a QUIC connection client. Send is safe for
// concurrent use; Receive must be called from one goroutine at a time.
type QUICClient struct {
	addr   string
	config *ClientConfig

	conn   *quic.Conn
	stream *quic.Stream
	codec  framing.Codec
	r      *bufio.Reader

	sendMu  sync.Mutex
	sendBuf []byte
}

// NewQUICClient creates a new QUIC client with the given address
func NewQUICClient(addr string, config *ClientConfig) (*QUICClient, error) {
	if addr == "" {
		return nil, fmt.Errorf("address is required")
	}
	if config == nil {
		config = DefaultConfig()
	}
	return &QUICClient{addr: addr, config: config}, nil
}

func (c *QUICClient) quicConfig() *QUICConfig {
	if q, ok := c.config.ProtocolConfig.(*QUICConfig); ok {
		return q
	}
	return DefaultConfig().ProtocolConfig.(*QUICConfig)
}

// Connect dials the server, opens the stream and picks the framing for the
// negotiated ALPN protocol.
func (c *QUICClient) Connect(ctx context.Context) error {
	qc := c.quicConfig()
	tlsConf := &tls.Config{
		NextProtos:         qc.NextProtos,
		InsecureSkipVerify: qc.InsecureSkipVerify, //nolint:gosec // opt-in, for self-signed development servers
		MinVersion:         qc.MinVersion,
	}
	conn, err := quic.DialAddr(ctx, c.addr, tlsConf, &quic.Config{EnableDatagrams: qc.Datagrams})
	if err != nil {
		return fmt.Errorf("failed to dial QUIC: %w", err)
	}
	codec, err := c.codecFor(conn.ConnectionState().TLS.NegotiatedProtocol)
	if err != nil {
		_ = conn.CloseWithError(0, "unsupported protocol")
		return err
	}
	stream, err := conn.OpenStreamSync(ctx)
	if err != nil {
		_ = conn.CloseWithError(0, "no stream")
		return fmt.Errorf("failed to open QUIC stream: %w", err)
	}
	c.conn, c.stream, c.codec = conn, stream, codec
	c.r = bufio.NewReaderSize(stream, max(c.config.BufferSize, 4096))
	return nil
}

func (c *QUICClient) codecFor(alpn string) (framing.Codec, error) {
	if codec, ok := c.quicConfig().Codecs[alpn]; ok {
		return codec, nil
	}
	if alpn == DefaultALPN || alpn == "" {
		return framing.NewLegacy(c.config.Delimiter)
	}
	return nil, fmt.Errorf("gophersocks client: no codec for ALPN %q", alpn)
}

// Send sends one message.
func (c *QUICClient) Send(data []byte) error {
	if c.stream == nil {
		return ErrNotConnected
	}
	c.sendMu.Lock()
	defer c.sendMu.Unlock()
	frame, err := c.codec.Append(c.sendBuf[:0], data)
	if err != nil {
		return err
	}
	c.sendBuf = frame
	if _, err := c.stream.Write(frame); err != nil {
		return fmt.Errorf("failed to write to QUIC stream: %w", err)
	}
	return nil
}

// Receive returns the next message from the server.
func (c *QUICClient) Receive() ([]byte, error) {
	if c.stream == nil {
		return nil, ErrNotConnected
	}
	return c.codec.Read(c.r, c.config.MaxLength)
}

// NegotiatedProtocol returns the ALPN protocol agreed with the server.
func (c *QUICClient) NegotiatedProtocol() string {
	if c.conn == nil {
		return ""
	}
	return c.conn.ConnectionState().TLS.NegotiatedProtocol
}

// Close closes the QUIC connection
func (c *QUICClient) Close() error {
	var err error
	if c.stream != nil {
		err = c.stream.Close()
	}
	if c.conn != nil {
		_ = c.conn.CloseWithError(0, "client closed connection")
	}
	if err != nil {
		return fmt.Errorf("failed to close QUIC stream: %w", err)
	}
	return nil
}

// LocalAddr returns the local network address
func (c *QUICClient) LocalAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.LocalAddr()
}

// RemoteAddr returns the remote network address
func (c *QUICClient) RemoteAddr() net.Addr {
	if c.conn == nil {
		return nil
	}
	return c.conn.RemoteAddr()
}

// ReceiveDatagram waits for the next QUIC datagram from the server. It needs
// the Datagrams option, and a server that enables them too.
func (c *QUICClient) ReceiveDatagram(ctx context.Context) ([]byte, error) {
	if c.conn == nil {
		return nil, errors.New("gophersocks: not connected")
	}
	if !c.conn.ConnectionState().SupportsDatagrams.Remote {
		return nil, errors.New("gophersocks: the server doesn't send datagrams")
	}
	return c.conn.ReceiveDatagram(ctx)
}

// DatagramsSupported reports whether both sides enabled datagrams.
func (c *QUICClient) DatagramsSupported() bool {
	return c.conn != nil && c.conn.ConnectionState().SupportsDatagrams.Remote && c.conn.ConnectionState().SupportsDatagrams.Local
}
