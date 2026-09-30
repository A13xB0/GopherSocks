package listener

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// closeFrameTimeout bounds how long closing waits to send the close frame.
const closeFrameTimeout = 100 * time.Millisecond

// WebSocketServer implements a WebSocket server. Each binary WebSocket
// message is one GopherSocks message; text messages are ignored.
type WebSocketServer struct {
	*server
	addr     string
	wsCfg    *WebSocketConfig
	upgrader websocket.Upgrader

	listenMu sync.Mutex
	ln       net.Listener
	http     *http.Server
}

// NewWebSocket creates a new WebSocket server with the given configuration
func NewWebSocket(host string, port uint16, ctx context.Context, opts ...ServerOption) (*WebSocketServer, error) {
	config := defaultConfig()
	for _, opt := range opts {
		opt(config)
	}
	if err := ValidateConfig(config); err != nil {
		return nil, NewConfigError("invalid configuration", err)
	}
	wsConfig, ok := config.ProtocolConfig.(*WebSocketConfig)
	if !ok {
		return nil, NewConfigError("invalid WebSocket configuration", nil)
	}
	checkOrigin := wsConfig.CheckOrigin
	if checkOrigin == nil {
		checkOrigin = allowAnyOrigin
	}
	return &WebSocketServer{
		server: newServer(ctx, config),
		addr:   hostPort(host, port),
		wsCfg:  wsConfig,
		upgrader: websocket.Upgrader{
			ReadBufferSize:  wsConfig.ReadBufferSize,
			WriteBufferSize: wsConfig.WriteBufferSize,
			CheckOrigin:     checkOrigin,
		},
	}, nil
}

// allowAnyOrigin keeps the original behaviour: game clients are not
// browsers, so there is no cross-site request to guard against. Set
// WebSocketConfig.CheckOrigin when serving browsers.
func allowAnyOrigin(*http.Request) bool { return true }

// listen binds the address, so bind errors are returned from StartListener.
func (w *WebSocketServer) Listen() error {
	w.listenMu.Lock()
	defer w.listenMu.Unlock()
	if w.ln != nil {
		return nil
	}
	ln, err := net.Listen("tcp", w.addr)
	if err != nil {
		return NewConnectionError("failed to start listener", err)
	}
	w.ln = ln
	w.log.Info("gophersocks: WebSocket server listening", "addr", w.addr, "path", w.wsCfg.Path)
	return nil
}

// StartListener binds the address and serves in the background.
func (w *WebSocketServer) StartListener() error {
	if err := w.Listen(); err != nil {
		return err
	}
	go func() {
		if err := w.Serve(w.ctx, w.legacyHandler); err != nil {
			w.log.Error("gophersocks: WebSocket serve stopped", "err", err)
		}
	}()
	return nil
}

// Serve accepts WebSocket connections until ctx ends or StopListener is called.
func (w *WebSocketServer) Serve(ctx context.Context, h Handler) error {
	if err := w.Listen(); err != nil {
		return err
	}
	if !w.enter() {
		return ErrServerStopped
	}
	defer w.wg.Done()
	ctx, stop := w.serveContext(ctx)
	defer stop()

	mux := http.NewServeMux()
	mux.HandleFunc(w.wsCfg.Path, func(rw http.ResponseWriter, r *http.Request) { w.upgrade(ctx, rw, r, h) })
	srv := &http.Server{Handler: mux, ReadHeaderTimeout: 10 * time.Second}
	w.listenMu.Lock()
	w.http = srv
	w.listenMu.Unlock()
	unblock := context.AfterFunc(ctx, func() { _ = srv.Close() })
	defer unblock()

	if err := srv.Serve(w.ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return NewConnectionError("serve failed", err)
	}
	return nil
}

// upgrade runs on the HTTP server's goroutine for the connection and serves
// the session there.
func (w *WebSocketServer) upgrade(ctx context.Context, rw http.ResponseWriter, r *http.Request, h Handler) {
	if !w.tryAdmit() {
		http.Error(rw, "Too many connections", http.StatusServiceUnavailable)
		return
	}
	if !w.enter() {
		w.release()
		http.Error(rw, "Server stopping", http.StatusServiceUnavailable)
		return
	}
	defer w.wg.Done()
	conn, err := w.upgrader.Upgrade(rw, r, nil)
	if err != nil {
		w.release()
		w.log.Debug("gophersocks: WebSocket upgrade failed", "addr", r.RemoteAddr, "err", err)
		return
	}
	conn.SetReadLimit(int64(w.cfg.MaxLength))
	t := &wsTransport{conn: conn, readTimeout: w.cfg.ReadTimeout, writeTimeout: w.cfg.WriteTimeout}
	sess := newSession(ctx, conn.RemoteAddr(), t, w.cfg, w.remove)
	w.add(sess)
	w.serveSession(h, sess)
}

// StopListener stops accepting, closes every session and waits for them.
func (w *WebSocketServer) StopListener() error {
	w.log.Info("gophersocks: shutting down WebSocket server", "addr", w.addr)
	return w.stop(func() error {
		w.listenMu.Lock()
		defer w.listenMu.Unlock()
		if w.http != nil {
			return w.http.Close()
		}
		if w.ln != nil {
			return w.ln.Close()
		}
		return nil
	})
}

// wsTransport adapts a gorilla WebSocket connection.
type wsTransport struct {
	conn         *websocket.Conn
	readTimeout  time.Duration
	writeTimeout time.Duration
	closeOnce    sync.Once
}

func (t *wsTransport) readMessage() ([]byte, error) {
	for {
		if t.readTimeout > 0 {
			if err := t.conn.SetReadDeadline(time.Now().Add(t.readTimeout)); err != nil {
				return nil, err
			}
		}
		kind, data, err := t.conn.ReadMessage()
		if err != nil {
			return nil, err
		}
		if kind == websocket.BinaryMessage {
			return data, nil
		}
	}
}

func (t *wsTransport) writeMessages(msgs [][]byte) error {
	if t.writeTimeout > 0 {
		if err := t.conn.SetWriteDeadline(time.Now().Add(t.writeTimeout)); err != nil {
			return err
		}
	}
	for _, m := range msgs {
		if err := t.conn.WriteMessage(websocket.BinaryMessage, m); err != nil {
			return err
		}
	}
	return nil
}

func (t *wsTransport) close(error) {
	t.closeOnce.Do(func() {
		_ = t.conn.WriteControl(websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(closeFrameTimeout))
		_ = t.conn.Close()
	})
}
