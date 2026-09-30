package listener

import (
	"context"
	"maps"
	"sync"
	"sync/atomic"
)

// server holds what every protocol server shares: the session registry,
// connection admission, the announce callback and the stop sequence.
type server struct {
	cfg    *ServerConfig
	log    Logger
	ctx    context.Context // ends when StopListener is called
	cancel context.CancelCauseFunc

	mu           sync.RWMutex
	byAddr       map[string]*session
	announce     AnnounceMiddlewareFunc
	announceOpts any
	stopped      bool

	active atomic.Int64   // admitted connections, including ones still handshaking
	wg     sync.WaitGroup // Serve loops and session goroutines
}

func newServer(ctx context.Context, cfg *ServerConfig) *server {
	sctx, cancel := context.WithCancelCause(ctx)
	return &server{
		cfg:    cfg,
		log:    cfg.Logger,
		ctx:    sctx,
		cancel: cancel,
		byAddr: make(map[string]*session),
	}
}

// enter registers a goroutine with the stop sequence. It reports false once
// StopListener has started, so nothing new starts during shutdown.
func (s *server) enter() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.stopped {
		return false
	}
	s.wg.Add(1)
	return true
}

// tryAdmit reserves a connection slot. A full server rejects the one
// connection and carries on serving everyone else.
func (s *server) tryAdmit() bool {
	if s.active.Add(1) > int64(s.cfg.MaxConnections) {
		s.active.Add(-1)
		s.log.Warn("gophersocks: max connections reached, rejecting connection", "max", s.cfg.MaxConnections)
		return false
	}
	return true
}

func (s *server) release() { s.active.Add(-1) }

func (s *server) add(sess *session) {
	s.mu.Lock()
	s.byAddr[sess.addr.String()] = sess
	s.mu.Unlock()
}

// remove is every session's onClose callback.
func (s *server) remove(sess *session) {
	s.mu.Lock()
	if s.byAddr[sess.addr.String()] == sess {
		delete(s.byAddr, sess.addr.String())
	}
	s.mu.Unlock()
	s.release()
	s.log.Debug("gophersocks: session closed", "session", sess.id, "addr", sess.addr.String())
}

// serveSession runs h for sess on the calling goroutine and closes the
// session when h returns.
func (s *server) serveSession(h Handler, sess *session) {
	defer sess.closeWith(ErrSessionClosed)
	h(sess.ctx, sess)
}

// serveContext merges Serve's context with the listener's lifetime.
func (s *server) serveContext(ctx context.Context) (context.Context, func()) {
	ctx, cancel := context.WithCancelCause(ctx)
	stop := context.AfterFunc(s.ctx, func() { cancel(context.Cause(s.ctx)) })
	return ctx, func() {
		stop()
		cancel(nil)
	}
}

// legacyHandler adapts SetAnnounceNewSession to Handler: start reading into
// Data (as earlier versions did, so a disconnect is noticed even if nobody
// reads), announce, then keep the session open until it closes.
func (s *server) legacyHandler(ctx context.Context, sess Session) {
	sess.Data()
	s.mu.RLock()
	fn, opts := s.announce, s.announceOpts
	s.mu.RUnlock()
	if fn != nil {
		fn(opts, sess)
	}
	<-ctx.Done()
}

// SetAnnounceNewSession sets the callback StartListener runs for each session.
func (s *server) SetAnnounceNewSession(function AnnounceMiddlewareFunc, options any) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.announce = function
	s.announceOpts = options
}

// GetActiveSessions returns the open sessions keyed by client address.
func (s *server) GetActiveSessions() map[string]Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make(map[string]Session, len(s.byAddr))
	for k, v := range s.byAddr {
		out[k] = v
	}
	return out
}

// GetSession returns the session for a client address, or nil.
func (s *server) GetSession(clientAddr string) Session {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if sess, ok := s.byAddr[clientAddr]; ok {
		return sess
	}
	return nil
}

// stop runs the shutdown sequence: refuse new work, stop accepting, close
// every session and wait for all goroutines.
func (s *server) stop(closeListener func() error) error {
	s.mu.Lock()
	s.stopped = true
	sessions := maps.Clone(s.byAddr)
	s.mu.Unlock()

	s.cancel(ErrServerStopped)
	err := closeListener()
	for _, sess := range sessions {
		sess.closeWith(ErrServerStopped)
	}
	s.wg.Wait()
	return err
}
