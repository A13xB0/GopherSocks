package listener

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"math/big"
	"testing"
	"time"
)

func generateTestTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		panic(err)
	}

	template := x509.Certificate{
		SerialNumber: big.NewInt(1),
		NotBefore:    time.Now(),
		NotAfter:     time.Now().Add(time.Hour),
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
		NextProtos:   []string{"quic-test"},
	}
}

func TestNewQUIC(t *testing.T) {
	tlsConfig := generateTestTLSConfig()
	ctx := context.Background()

	tests := []struct {
		name    string
		host    string
		port    uint16
		tls     *tls.Config
		wantErr bool
	}{
		{
			name:    "Valid configuration",
			host:    "localhost",
			port:    0,
			tls:     tlsConfig,
			wantErr: false,
		},
		{
			name:    "Missing TLS config",
			host:    "localhost",
			port:    0,
			tls:     nil,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := NewQUIC(tt.host, tt.port, tt.tls, ctx)
			if (err != nil) != tt.wantErr {
				t.Errorf("NewQUIC() error = %v, wantErr %v", err, tt.wantErr)
			}
		})
	}
}

func TestQUICServer_StartStopListener(t *testing.T) {
	tlsConfig := generateTestTLSConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, err := NewQUIC("localhost", 0, tlsConfig, ctx)
	if err != nil {
		t.Fatalf("Failed to create QUIC server: %v", err)
	}

	// Test StartListener
	if err := server.StartListener(); err != nil {
		t.Errorf("StartListener() error = %v", err)
	}

	// Test StopListener
	if err := server.StopListener(); err != nil {
		t.Errorf("StopListener() error = %v", err)
	}
}

func TestQUICServer_SessionManagement(t *testing.T) {
	tlsConfig := generateTestTLSConfig()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	server, err := NewQUIC("localhost", 0, tlsConfig, ctx)
	if err != nil {
		t.Fatalf("Failed to create QUIC server: %v", err)
	}

	// Test session management methods
	if sessions := server.GetActiveSessions(); len(sessions) != 0 {
		t.Errorf("Expected 0 active sessions, got %d", len(sessions))
	}

	if session := server.GetSession("non-existent"); session != nil {
		t.Error("Expected nil for non-existent session")
	}
}
