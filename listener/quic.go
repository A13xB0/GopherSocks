package listener

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/quic-go/quic-go"
)

// QUICServer implements a QUIC streaming server with session management
type QUICServer struct {
	listener               *quic.Listener
	addr                   string
	announceMiddleware     AnnounceMiddlewareFunc
	announceMiddlewareOpts any
	sessions               map[string]Session
	sessionsMutex          sync.RWMutex
	ctx                    context.Context
	cancel                 context.CancelFunc
	*ServerConfig
}

func (q *QUICServer) SetAnnounceNewSession(function AnnounceMiddlewareFunc, options any) {
	q.announceMiddleware = function
	q.announceMiddlewareOpts = options
}

func (q *QUICServer) GetActiveSessions() map[string]Session {
	q.sessionsMutex.RLock()
	defer q.sessionsMutex.RUnlock()
	sessions := make(map[string]Session)
	for k, v := range q.sessions {
		sessions[k] = v
	}
	return sessions
}

func (q *QUICServer) GetSession(ClientAddr string) Session {
	q.sessionsMutex.RLock()
	defer q.sessionsMutex.RUnlock()
	return q.sessions[ClientAddr]
}

// QUICSession represents an active QUIC connection
type QUICSession struct {
	*BaseSession
	server *QUICServer
	conn   quic.Connection
	stream quic.Stream
}

// GetLastRecieved maintains backward compatibility with the Session interface
func (s *QUICSession) GetLastRecieved() time.Time {
	return s.GetLastReceived()
}

func (s *QUICSession) SendToClient(data []byte) error {
	quicConfig := s.server.ServerConfig.ProtocolConfig.(*QUICConfig)
	delimiter := quicConfig.Delimiter
	sData := append(data, delimiter...)
	_, err := s.stream.Write(sData)
	if err != nil {
		return err
	}
	return nil
}

func (s *QUICSession) CloseSession() {
	s.server.closeSession(s)
}

// NewQUIC creates a new QUIC server with the given configuration
func NewQUIC(host string, port uint16, ctx context.Context, opts ...ServerOption) (Listener, error) {
	config := defaultConfig()
	// Set default QUIC configuration
	config.ProtocolConfig = &QUICConfig{
		TLSConfig: defaultTLSConfig(),
		QUICConfig: &quic.Config{
			MaxIncomingStreams:    int64(config.MaxConnections),
			MaxIncomingUniStreams: int64(config.MaxConnections),
		},
		Delimiter: []byte("\n\n\n"),
	}

	for _, opt := range opts {
		opt(config)
	}

	if err := ValidateConfig(config); err != nil {
		return nil, NewConfigError("invalid configuration", err)
	}

	addr := fmt.Sprintf("%v:%v", host, port)
	ctx, cancel := context.WithCancel(ctx)

	return &QUICServer{
		addr:         addr,
		ctx:          ctx,
		cancel:       cancel,
		sessions:     make(map[string]Session),
		ServerConfig: config,
	}, nil
}

func (q *QUICServer) closeSession(session *QUICSession) {
	q.sessionsMutex.Lock()
	defer q.sessionsMutex.Unlock()
	// Check if session is already closed
	if _, exists := q.sessions[session.GetClientAddr().String()]; !exists {
		return
	}
	_ = session.stream.Close()
	session.BaseSession.Cancel()
	select {
	case <-session.DataChannel:
		// Drain any remaining messages
	default:
	}
	close(session.DataChannel)
	delete(q.sessions, session.GetClientAddr().String())
	q.Logger.Debug("Closed session for %s", session.GetClientAddr())
}

// StartListener begins accepting QUIC connections
func (q *QUICServer) StartListener() error {
	addr, err := net.ResolveUDPAddr("udp", q.addr)
	if err != nil {
		return NewConnectionError("failed to resolve address", err)
	}

	udpConn, err := net.ListenUDP("udp", addr)
	if err != nil {
		return err
	}
	tr := quic.Transport{
		Conn: udpConn,
	}
	quicConfig := q.ServerConfig.ProtocolConfig.(*QUICConfig)
	q.listener, err = tr.Listen(quicConfig.TLSConfig, quicConfig.QUICConfig)
	if err != nil {
		return err
	}
	go q.receiveConnections()

	return nil
}

func (q *QUICServer) receiveConnections() {
	for {
		conn, err := q.listener.Accept(q.ctx)
		if err != nil {
			q.Logger.Error("failed to accept stream", err)
			continue
		}
		session := q.newSession(conn)
		go session.receiveStream()
	}
}

func (q *QUICServer) newSession(conn quic.Connection) *QUICSession {
	var err error
	base := NewBaseSession(conn.RemoteAddr(), q.ctx, q.Logger, q.ServerConfig)
	base.ID = uuid.NewString()
	session := &QUICSession{
		BaseSession: base,
		server:      q,
		conn:        conn,
		stream:      nil,
	}
	q.sessionsMutex.Lock()
	q.sessions[conn.RemoteAddr().String()] = session
	q.sessionsMutex.Unlock()

	session.stream, err = session.conn.AcceptStream(session.server.ctx)
	if err != nil {
		q.Logger.Error("failed to accept stream", err)
	}

	if q.announceMiddleware != nil {
		q.announceMiddleware(q.announceMiddlewareOpts, session)
	}

	return session
}

func (q *QUICSession) receiveStream() {
	buffer := make([]byte, 0)
	quicConfig := q.server.ServerConfig.ProtocolConfig.(*QUICConfig)
	delimiter := quicConfig.Delimiter
	for {
		streamBytes := make([]byte, 1024) // Read in chunks
		n, err := q.stream.Read(streamBytes)
		if err != nil {
			if err == io.EOF {
				// Handle connection closed
				fmt.Println("Connection closed")
				q.server.closeSession(q)
				return
			} else {
				q.server.Logger.Error("receive stream error", err)
			}
		}
		if n == 0 {
			continue
		}
		q.updateLastReceived()

		buffer = append(buffer, streamBytes[:n]...)

		for {
			index := bytes.Index(buffer, delimiter)
			if index == -1 {
				break
			}

			chunk := buffer[:index]
			buffer = buffer[index+len(delimiter):]

			// Process the chunk
			q.DataChannel <- chunk
		}
	}
}

// StopListener gracefully shuts down the QUIC server
func (q *QUICServer) StopListener() error {
	q.Logger.Info("Shutting down UDP server")
	err := q.listener.Close()
	if err != nil {
		return err
	}
	q.cancel()
	return nil
}

func defaultTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour * 24),
		KeyUsage:     x509.KeyUsageKeyEncipherment | x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, &template, &template, &key.PublicKey, key)
	if err != nil {
		panic(err)
	}

	keyPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(key),
	})
	certPEM := pem.EncodeToMemory(&pem.Block{
		Type:  "CERTIFICATE",
		Bytes: certDER,
	})

	tlsCert, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		panic(err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"gophersocks"},
		MinVersion:   tls.VersionTLS13,
	}
}
