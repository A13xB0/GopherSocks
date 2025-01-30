package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	gophersocks "github.com/A13xB0/GopherSocks"
	"github.com/A13xB0/GopherSocks/listener"
)

func main() {
	// Setup Listener with configuration
	host := "0.0.0.0"
	port := uint16(8001)

	// Create UDP listener with custom configuration
	udpListener, err := gophersocks.NewUDPListener(
		host,
		port,
		// Configure options
		gophersocks.WithMaxLength(65507),     // Maximum UDP datagram size
		gophersocks.WithBufferSize(1000),     // Larger buffer for UDP datagrams
		gophersocks.WithTimeouts(60, 60),     // Longer timeouts for UDP
		gophersocks.WithMaxConnections(1000), // Maximum concurrent sessions
	)
	if err != nil {
		fmt.Printf("Failed to create UDP listener: %v\n", err)
		os.Exit(1)
	}

	// Setup signal handler for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go handleShutdown(udpListener, sigs)

	// Set up session handler
	udpListener.SetAnnounceNewSession(handleNewSession, nil)

	// Start Listening
	fmt.Printf("Starting UDP listener on %s:%d\n", host, port)
	if err := udpListener.StartListener(); err != nil {
		fmt.Printf("Failed to start listener: %v\n", err)
		os.Exit(1)
	}
}

// handleSession processes datagrams for a UDP session
func handleSession(session listener.Session) {
	sessionID := session.GetSessionID()
	clientAddr := session.GetClientAddr()

	for data := range session.Data() {
		fmt.Printf("Received datagram from session %s (%v): %s\n",
			sessionID,
			clientAddr,
			string(data),
		)

		// Echo the datagram back
		if err := session.SendToClient(data); err != nil {
			fmt.Printf("Failed to send datagram to session %s: %v\n", sessionID, err)
			return
		}

		fmt.Printf("Echoed datagram back to session %s\n", sessionID)

		// Check last received time to detect stale sessions
		lastReceived := session.GetLastRecieved()
		if time.Since(lastReceived) > time.Minute {
			fmt.Printf("Session %s idle for too long, closing\n", sessionID)
			session.CloseSession()
			return
		}
	}

	fmt.Printf("Session %s closed\n", sessionID)
}

// handleNewSession is called when a new UDP session is established
func handleNewSession(options any, session listener.Session) {
	fmt.Printf("New UDP session from %v - Session ID: %v\n",
		session.GetClientAddr(),
		session.GetSessionID(),
	)

	// Start processing datagrams for this session
	go handleSession(session)
}

// handleShutdown performs graceful shutdown when a signal is received
func handleShutdown(listener gophersocks.Listener, sigs chan os.Signal) {
	sig := <-sigs
	fmt.Printf("\nReceived signal: %v, initiating graceful shutdown...\n", sig)

	// Create a timeout context for shutdown
	done := make(chan bool)
	go func() {
		if err := listener.StopListener(); err != nil {
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
