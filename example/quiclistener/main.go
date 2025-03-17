package main

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"log"
	"math/big"
	"time"

	"github.com/A13xB0/GopherSocks/listener"
)

func generateTLSConfig() *tls.Config {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		log.Fatal(err)
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
		log.Fatal(err)
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
		log.Fatal(err)
	}

	return &tls.Config{
		Certificates: []tls.Certificate{tlsCert},
		NextProtos:   []string{"quic-echo"},
	}
}

func main() {
	ctx := context.Background()

	// Create a new QUIC listener with custom options
	server, err := listener.NewQUIC("localhost", 8443, generateTLSConfig(), ctx,
		listener.WithMaxLength(1024*1024),               // 1MB max message size
		listener.WithBufferSize(1000),                   // Channel buffer size
		listener.WithTimeouts(time.Minute, time.Minute), // Read/Write timeouts
	)
	if err != nil {
		log.Fatal(err)
	}

	// Set up middleware to handle new sessions
	server.SetAnnounceNewSession(func(opts any, session listener.Session) {
		fmt.Printf("New connection from %s\n", session.GetClientAddr())

		// Start a goroutine to handle incoming messages
		go func() {
			for {
				select {
				case data := <-session.Data():
					fmt.Printf("Received from %s: %s\n", session.GetClientAddr(), string(data))
					// Echo the data back
					if err := session.SendToClient(data); err != nil {
						fmt.Printf("Error sending to client: %v\n", err)
						return
					}
				}
			}
		}()
	}, nil)

	fmt.Println("QUIC server listening on localhost:8443")
	if err := server.StartListener(); err != nil {
		log.Fatal(err)
	}

	// Keep the server running
	select {}
}
