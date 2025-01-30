package main

import (
	"fmt"
	"os"
	"os/signal"
	"syscall"

	gophersocks "github.com/A13xB0/GopherSocks"
	"github.com/A13xB0/GopherSocks/listener"
)

func main() {
	// Setup Listener with configuration
	host := "0.0.0.0"
	port := uint16(8001)

	// Create WebSocket listener with custom configuration
	wsListener, err := gophersocks.NewWebSocketListener(
		host,
		port,
		// Configure options
		gophersocks.WithMaxLength(1024*1024), // 1MB max message size
		gophersocks.WithBufferSize(100),      // Channel buffer size
		gophersocks.WithTimeouts(30, 30),     // 30 second read/write timeouts
	)
	if err != nil {
		panic(err)
	}

	// Setup signal handler for graceful shutdown
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go stop(wsListener, sigs)

	// Set up session handler
	wsListener.SetAnnounceNewSession(announceStreamHandler, nil)

	// Start Listening
	fmt.Printf("Starting WebSocket listener on ws://%s:%d/ws\n", host, port)
	if err := wsListener.StartListener(); err != nil {
		panic(err)
	}
}

// echo handles the WebSocket session data by echoing received messages back to the client
func echo(session listener.Session) {
	for data := range session.Data() {
		fmt.Printf("Received message from session %s: %s\n",
			session.GetSessionID(),
			string(data),
		)

		if err := session.SendToClient(data); err != nil {
			fmt.Printf("Session (%s) Error: %s\n", session.GetSessionID(), err)
			return
		}

		fmt.Printf("Echoed message back to session %s\n", session.GetSessionID())
	}
}

// announceStreamHandler is called when a new WebSocket session is established
func announceStreamHandler(options any, session listener.Session) {
	fmt.Printf("New WebSocket connection from %v - Session ID: %v\n",
		session.GetClientAddr(),
		session.GetSessionID(),
	)
	go echo(session)
}

// stop handles graceful shutdown when a signal is received
func stop(listener gophersocks.Listener, sigs chan os.Signal) {
	sig := <-sigs
	fmt.Printf("\nReceived signal: %v, shutting down...\n", sig)

	if err := listener.StopListener(); err != nil {
		fmt.Printf("Error during shutdown: %v\n", err)
		return
	}

	fmt.Println("Server shutdown complete")
}
