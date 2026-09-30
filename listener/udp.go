package listener

import (
	"context"
	"errors"
	"net"
	"sync"
	"time"
)

// maxUDPPayload is the largest UDP payload.
const maxUDPPayload = 65507

// UDPServer implements a UDP server. Each client address is a session; a
// session with no datagrams for ReadTimeout is closed.
type UDPServer struct {
	*server
	addr string

	listenMu sync.Mutex
	conn     *net.UDPConn
}

// NewUDP creates a new UDP server with the given configuration
func NewUDP(host string, port uint16, ctx context.Context, opts ...ServerOption) (Listener, error) {
	config := defaultConfig()
	for _, opt := range opts {
		opt(config)
	}
	if err := ValidateConfig(config); err != nil {
		return nil, NewConfigError("invalid configuration", err)
	}
	return &UDPServer{
		server: newServer(ctx, config),
		addr:   hostPort(host, port),
	}, nil
}

func (u *UDPServer) Listen() error {
	u.listenMu.Lock()
	defer u.listenMu.Unlock()
	if u.conn != nil {
		return nil
	}
	addr, err := net.ResolveUDPAddr("udp", u.addr)
	if err != nil {
		return NewConnectionError("failed to resolve address", err)
	}
	conn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return NewConnectionError("failed to start listener", err)
	}
	u.conn = conn
	u.log.Info("gophersocks: UDP server listening", "addr", u.addr)
	return nil
}

// StartListener binds the address and serves in the background.
func (u *UDPServer) StartListener() error {
	if err := u.Listen(); err != nil {
		return err
	}
	go func() {
		if err := u.Serve(u.ctx, u.legacyHandler); err != nil {
			u.log.Error("gophersocks: UDP serve stopped", "err", err)
		}
	}()
	return nil
}

// Serve reads datagrams until ctx ends or StopListener is called.
func (u *UDPServer) Serve(ctx context.Context, h Handler) error {
	if err := u.Listen(); err != nil {
		return err
	}
	if !u.enter() {
		return ErrServerStopped
	}
	defer u.wg.Done()
	ctx, stop := u.serveContext(ctx)
	defer stop()
	unblock := context.AfterFunc(ctx, func() { _ = u.closeConn() })
	defer unblock()
	go u.reapIdle(ctx)

	// One byte over the limit tells an oversized datagram from a full one.
	buf := make([]byte, min(int(u.cfg.MaxLength), maxUDPPayload)+1)
	for {
		n, addr, err := u.conn.ReadFromUDP(buf)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return NewConnectionError("read failed", err)
		}
		if n == 0 {
			continue
		}
		if n > int(u.cfg.MaxLength) {
			u.log.Warn("gophersocks: dropping oversized datagram", "addr", addr.String(), "bytes", n)
			continue
		}
		u.deliver(ctx, addr, append([]byte(nil), buf[:n]...), h)
	}
}

// deliver hands a datagram to its session, creating the session on first contact.
func (u *UDPServer) deliver(ctx context.Context, addr *net.UDPAddr, msg []byte, h Handler) {
	u.mu.RLock()
	sess := u.byAddr[addr.String()]
	u.mu.RUnlock()
	if sess == nil {
		if sess = u.open(ctx, addr, h); sess == nil {
			return
		}
	}
	sess.touch()
	t := sess.t.(*udpTransport)
	select {
	case t.in <- msg:
	default:
		u.log.Warn("gophersocks: session queue full, dropping datagram", "session", sess.id)
	}
}

func (u *UDPServer) open(ctx context.Context, addr *net.UDPAddr, h Handler) *session {
	if !u.tryAdmit() {
		return nil
	}
	if !u.enter() {
		u.release()
		return nil
	}
	t := &udpTransport{
		conn: u.conn,
		addr: addr,
		in:   make(chan []byte, max(u.cfg.BufferSize, 1)),
		done: make(chan struct{}),
	}
	sess := newSession(ctx, addr, t, u.cfg, u.remove)
	u.add(sess)
	go func() {
		defer u.wg.Done()
		u.serveSession(h, sess)
	}()
	return sess
}

// reapIdle closes sessions that have been silent for ReadTimeout.
func (u *UDPServer) reapIdle(ctx context.Context) {
	if u.cfg.ReadTimeout <= 0 {
		return
	}
	t := time.NewTicker(max(u.cfg.ReadTimeout/4, 10*time.Millisecond))
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-t.C:
			for _, s := range u.GetActiveSessions() {
				if now.Sub(s.GetLastRecieved()) > u.cfg.ReadTimeout {
					s.(*session).closeWith(context.DeadlineExceeded)
				}
			}
		}
	}
}

func (u *UDPServer) closeConn() error {
	u.listenMu.Lock()
	defer u.listenMu.Unlock()
	if u.conn == nil {
		return nil
	}
	err := u.conn.Close()
	if errors.Is(err, net.ErrClosed) {
		return nil
	}
	return err
}

// StopListener stops reading, closes every session and waits for them.
func (u *UDPServer) StopListener() error {
	u.log.Info("gophersocks: shutting down UDP server", "addr", u.addr)
	return u.stop(u.closeConn)
}

// udpTransport is one client address on the shared socket.
type udpTransport struct {
	conn      *net.UDPConn
	addr      *net.UDPAddr
	in        chan []byte // sent to only by the server's read loop; never closed
	done      chan struct{}
	closeOnce sync.Once
}

func (t *udpTransport) readMessage() ([]byte, error) {
	select {
	case m := <-t.in:
		return m, nil
	case <-t.done:
		return nil, ErrSessionClosed
	}
}

func (t *udpTransport) writeMessages(msgs [][]byte) error {
	for _, m := range msgs {
		if _, err := t.conn.WriteToUDP(m, t.addr); err != nil {
			return err
		}
	}
	return nil
}

func (t *udpTransport) close(error) {
	t.closeOnce.Do(func() { close(t.done) })
}
