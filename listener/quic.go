package listener

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/A13xB0/GopherSocks/framing"
	"github.com/quic-go/quic-go"
)

// QUIC application error codes used when the server closes a connection.
const (
	quicCodeClosed     quic.ApplicationErrorCode = 0
	quicCodeServerFull quic.ApplicationErrorCode = 1
	quicCodeProtocol   quic.ApplicationErrorCode = 2
)

// defaultStreamTimeout bounds how long a new connection may take to open its
// stream when no ReadTimeout is configured.
const defaultStreamTimeout = 10 * time.Second

// QUICServer implements a QUIC streaming server with session management.
// Each connection carries one bidirectional stream of framed messages; the
// framing is chosen by the ALPN protocol the client negotiates.
type QUICServer struct {
	*server
	addr string
	qcfg *QUICConfig

	listenMu sync.Mutex
	udp      *net.UDPConn
	tr       *quic.Transport
	ln       *quic.Listener
	codecs   map[string]framing.Codec
}

// NewQUIC creates a new QUIC server with the given configuration
func NewQUIC(host string, port uint16, ctx context.Context, opts ...ServerOption) (Listener, error) {
	config := defaultConfig()
	qcfg := defaultQUICConfig()
	config.ProtocolConfig = qcfg
	for _, opt := range opts {
		opt(config)
	}
	if err := ValidateConfig(config); err != nil {
		return nil, NewConfigError("invalid configuration", err)
	}
	if q, ok := config.ProtocolConfig.(*QUICConfig); ok {
		qcfg = q
	}
	return &QUICServer{
		server: newServer(ctx, config),
		addr:   hostPort(host, port),
		qcfg:   qcfg,
	}, nil
}

// listen binds the UDP socket and starts the QUIC listener once.
func (q *QUICServer) Listen() error {
	q.listenMu.Lock()
	defer q.listenMu.Unlock()
	if q.ln != nil {
		return nil
	}
	codecs, err := q.resolveCodecs()
	if err != nil {
		return NewConfigError("invalid QUIC framing", err)
	}
	tlsConf, err := q.tlsConfig()
	if err != nil {
		return NewConfigError("TLS setup failed", err)
	}
	addr, err := net.ResolveUDPAddr("udp", q.addr)
	if err != nil {
		return NewConnectionError("failed to resolve address", err)
	}
	udp, err := net.ListenUDP("udp", addr)
	if err != nil {
		return NewConnectionError("failed to start listener", err)
	}
	tr := &quic.Transport{Conn: udp}
	ln, err := tr.Listen(tlsConf, q.qcfg.QUICConfig)
	if err != nil {
		_ = udp.Close()
		return NewConnectionError("failed to start QUIC listener", err)
	}
	q.udp, q.tr, q.ln, q.codecs = udp, tr, ln, codecs
	q.log.Info("gophersocks: QUIC server listening", "addr", q.addr)
	return nil
}

func (q *QUICServer) resolveCodecs() (map[string]framing.Codec, error) {
	out := make(map[string]framing.Codec, len(q.qcfg.ALPN))
	for _, alpn := range q.qcfg.ALPN {
		if c, ok := q.qcfg.Codecs[alpn]; ok {
			out[alpn] = c
			continue
		}
		if alpn != DefaultALPN {
			return nil, fmt.Errorf("no codec for ALPN %q", alpn)
		}
		c, err := framing.NewLegacy(q.qcfg.Delimiter)
		if err != nil {
			return nil, err
		}
		out[alpn] = c
	}
	if len(out) == 0 {
		return nil, errors.New("no ALPN protocols configured")
	}
	return out, nil
}

// tlsConfig returns the configured TLS settings, with NextProtos filled in,
// or an ephemeral self-signed certificate when none was given.
func (q *QUICServer) tlsConfig() (*tls.Config, error) {
	if q.qcfg.TLSConfig != nil {
		c := q.qcfg.TLSConfig.Clone()
		if len(c.NextProtos) == 0 {
			c.NextProtos = q.qcfg.ALPN
		}
		return c, nil
	}
	cert, err := selfSignedCert()
	if err != nil {
		return nil, err
	}
	return &tls.Config{
		Certificates: []tls.Certificate{cert},
		NextProtos:   q.qcfg.ALPN,
		MinVersion:   tls.VersionTLS13,
	}, nil
}

// selfSignedCert creates an ECDSA P-256 certificate valid for a year. It is
// for development: clients must skip verification or pin its key.
func selfSignedCert() (tls.Certificate, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return tls.Certificate{}, err
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		return tls.Certificate{}, err
	}
	template := x509.Certificate{
		SerialNumber: serial,
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().AddDate(1, 0, 0),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}
	der, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		return tls.Certificate{}, err
	}
	return tls.Certificate{Certificate: [][]byte{der}, PrivateKey: key}, nil
}

// StartListener binds the address and serves in the background.
func (q *QUICServer) StartListener() error {
	if err := q.Listen(); err != nil {
		return err
	}
	go func() {
		if err := q.Serve(q.ctx, q.legacyHandler); err != nil {
			q.log.Error("gophersocks: QUIC serve stopped", "err", err)
		}
	}()
	return nil
}

// Serve accepts connections until ctx ends or StopListener is called.
func (q *QUICServer) Serve(ctx context.Context, h Handler) error {
	if err := q.Listen(); err != nil {
		return err
	}
	if !q.enter() {
		return ErrServerStopped
	}
	defer q.wg.Done()
	ctx, stop := q.serveContext(ctx)
	defer stop()

	for {
		conn, err := q.ln.Accept(ctx)
		if err != nil {
			if ctx.Err() != nil || errors.Is(err, quic.ErrServerClosed) {
				return nil
			}
			return NewConnectionError("accept failed", err)
		}
		if !q.tryAdmit() {
			_ = conn.CloseWithError(quicCodeServerFull, "server full")
			continue
		}
		if !q.enter() {
			q.release()
			_ = conn.CloseWithError(quicCodeClosed, "server stopping")
			return nil
		}
		go func() {
			defer q.wg.Done()
			q.handleConn(ctx, conn, h)
		}()
	}
}

// handleConn waits for the client's stream on the connection's own
// goroutine, so a client that never opens one can't hold up anyone else.
func (q *QUICServer) handleConn(ctx context.Context, conn *quic.Conn, h Handler) {
	timeout := q.cfg.ReadTimeout
	if timeout <= 0 {
		timeout = defaultStreamTimeout
	}
	sctx, cancel := context.WithTimeout(ctx, timeout)
	stream, err := conn.AcceptStream(sctx)
	cancel()
	if err != nil {
		q.release()
		_ = conn.CloseWithError(quicCodeProtocol, "no stream opened")
		q.log.Debug("gophersocks: QUIC connection opened no stream", "addr", conn.RemoteAddr().String(), "err", err)
		return
	}
	codec := q.codecs[conn.ConnectionState().TLS.NegotiatedProtocol]
	if codec == nil {
		codec = q.codecs[DefaultALPN]
	}
	if codec == nil {
		q.release()
		_ = conn.CloseWithError(quicCodeProtocol, "unsupported protocol")
		return
	}
	t := &streamTransport{
		r:                newReader(stream),
		w:                stream,
		codec:            codec,
		max:              min(int(q.cfg.MaxLength), codec.MaxPayload()),
		writeTimeout:     q.cfg.WriteTimeout,
		setWriteDeadline: stream.SetWriteDeadline,
		closeFn: func(error) {
			_ = conn.CloseWithError(quicCodeClosed, "session closed")
		},
	}
	sess := newSession(ctx, conn.RemoteAddr(), t, q.cfg, q.remove)
	// quic-go notices a dead connection (idle timeout, peer close) on its
	// own; close the session then even if nobody is reading.
	stopWatch := context.AfterFunc(conn.Context(), func() { sess.closeWith(ErrSessionClosed) })
	defer stopWatch()
	q.add(sess)
	q.serveSession(h, sess)
}

// StopListener stops accepting, closes every session and waits for them.
func (q *QUICServer) StopListener() error {
	q.log.Info("gophersocks: shutting down QUIC server", "addr", q.addr)
	return q.stop(func() error {
		q.listenMu.Lock()
		defer q.listenMu.Unlock()
		if q.ln == nil {
			return nil
		}
		err := q.ln.Close()
		_ = q.tr.Close()
		_ = q.udp.Close()
		return err
	})
}
