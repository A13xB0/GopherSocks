package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/A13xB0/GopherSocks/listener"
)

func main() {
	// Setup Listener with configuration
	host := "0.0.0.0"
	port := uint16(8443)

	// Create context for the listener
	ctx := context.Background()

	// Create QUIC listener with custom configuration
	quicListener, err := listener.NewQUIC(
		host,
		port,
		ctx,
		// Configure options
		listener.WithMaxLength(1024*1024), // 1MB max message size
		listener.WithBufferSize(1000),     // Channel buffer size
		listener.WithTimeouts(30, 30),     // 30 second read/write timeouts
		listener.WithMaxConnections(1000), // Maximum concurrent connections
	)
	if err != nil {
		fmt.Printf("Failed to create QUIC listener: %v\n", err)
		os.Exit(1)
	}

	// Setup signal handler for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go handleShutdown(quicListener, sigs)

	// Set up session handler
	quicListener.SetAnnounceNewSession(handleNewSession, nil)

	// Start Listening
	fmt.Printf("Starting QUIC listener on %s:%d\n", host, port)
	if err := quicListener.StartListener(); err != nil {
		fmt.Printf("Failed to start listener: %v\n", err)
		os.Exit(1)
	}
	select {}
}

// handleSession processes data for a QUIC session
func handleSession(session listener.Session) {
	sessionID := session.GetSessionID()
	clientAddr := session.GetClientAddr()

	for data := range session.Data() {
		fmt.Printf("Received data from session %s (%v): %s\n",
			sessionID,
			clientAddr,
			string(data),
		)

		// Echo the data back
		if err := session.SendToClient(data); err != nil {
			fmt.Printf("Failed to send data to session %s: %v\n", sessionID, err)
			return
		}

		fmt.Printf("Echoed data back to session %s\n", sessionID)
	}

	fmt.Printf("Session %s closed\n", sessionID)
}

// handleNewSession is called when a new QUIC session is established
func handleNewSession(options any, session listener.Session) {
	fmt.Printf("New QUIC connection from %v - Session ID: %v\n",
		session.GetClientAddr(),
		session.GetSessionID(),
	)

	// Start processing data for this session
	go handleSession(session)
}

// handleShutdown performs graceful shutdown when a signal is received
func handleShutdown(l listener.Listener, sigs chan os.Signal) {
	sig := <-sigs
	fmt.Printf("\nReceived signal: %v, initiating graceful shutdown...\n", sig)

	// Create a timeout context for shutdown
	done := make(chan bool)
	go func() {
		if err := l.StopListener(); err != nil {
			fmt.Printf("Error during shutdown: %v\n", err)
		}
		done <- true
	}()

	// Wait for shutdown with timeout
	select {
	case <-done:
		fmt.Println("Server shutdown complete")
	case <-time.After(10 * time.Second):
		fmt.Println("Server shutdown timed out")
	}
}
