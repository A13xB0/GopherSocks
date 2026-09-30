package listener

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"

	"github.com/A13xB0/GopherSocks/framing"
)

// acceptRetryDelay is the pause after a transient Accept error.
const acceptRetryDelay = 50 * time.Millisecond

// TCPServer implements a TCP streaming server. Messages are framed with a
// big-endian uint32 length prefix (framing.Uint32).
type TCPServer struct {
	*server
	addr string

	listenMu sync.Mutex
	ln       net.Listener
}

// NewTCP creates a new TCP server with the given configuration
func NewTCP(host string, port uint16, ctx context.Context, opts ...ServerOption) (*TCPServer, error) {
	config := defaultConfig()
	for _, opt := range opts {
		opt(config)
	}
	if err := ValidateConfig(config); err != nil {
		return nil, NewConfigError("invalid configuration", err)
	}
	return &TCPServer{
		server: newServer(ctx, config),
		addr:   hostPort(host, port),
	}, nil
}

func (t *TCPServer) Listen() error {
	t.listenMu.Lock()
	defer t.listenMu.Unlock()
	if t.ln != nil {
		return nil
	}
	ln, err := net.Listen("tcp", t.addr)
	if err != nil {
		return NewConnectionError("failed to start listener", err)
	}
	t.ln = ln
	t.log.Info("gophersocks: TCP server listening", "addr", t.addr)
	return nil
}

// StartListener binds the address and serves in the background.
func (t *TCPServer) StartListener() error {
	if err := t.Listen(); err != nil {
		return err
	}
	go func() {
		if err := t.Serve(t.ctx, t.legacyHandler); err != nil {
			t.log.Error("gophersocks: TCP serve stopped", "err", err)
		}
	}()
	return nil
}

// Serve accepts connections until ctx ends or StopListener is called.
func (t *TCPServer) Serve(ctx context.Context, h Handler) error {
	if err := t.Listen(); err != nil {
		return err
	}
	if !t.enter() {
		return ErrServerStopped
	}
	defer t.wg.Done()
	ctx, stop := t.serveContext(ctx)
	defer stop()
	unblock := context.AfterFunc(ctx, func() { _ = t.closeListener() })
	defer unblock()

	for {
		conn, err := t.ln.Accept()
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			var ne net.Error
			if errors.As(err, &ne) && ne.Timeout() {
				time.Sleep(acceptRetryDelay)
				continue
			}
			return NewConnectionError("accept failed", err)
		}
		t.accept(ctx, conn, h)
	}
}

func (t *TCPServer) accept(ctx context.Context, conn net.Conn, h Handler) {
	if !t.tryAdmit() {
		_ = conn.Close()
		return
	}
	if !t.enter() {
		t.release()
		_ = conn.Close()
		return
	}
	tr := &streamTransport{
		r:                newReader(conn),
		w:                conn,
		codec:            framing.Uint32,
		max:              int(t.cfg.MaxLength),
		readTimeout:      t.cfg.ReadTimeout,
		writeTimeout:     t.cfg.WriteTimeout,
		setReadDeadline:  conn.SetReadDeadline,
		setWriteDeadline: conn.SetWriteDeadline,
		closeFn:          func(error) { _ = conn.Close() },
	}
	sess := newSession(ctx, conn.RemoteAddr(), tr, t.cfg, t.remove)
	t.add(sess)
	go func() {
		defer t.wg.Done()
		t.serveSession(h, sess)
	}()
}

func (t *TCPServer) closeListener() error {
	t.listenMu.Lock()
	defer t.listenMu.Unlock()
	if t.ln == nil {
		return nil
	}
	err := t.ln.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// StopListener stops accepting, closes every session and waits for them.
func (t *TCPServer) StopListener() error {
	t.log.Info("gophersocks: shutting down TCP server", "addr", t.addr)
	return t.stop(t.closeListener)
}
