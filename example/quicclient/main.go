package main

import (
	"context"
	"fmt"
	"log"
	"time"

	gophersocks "github.com/A13xB0/GopherSocks"
)

func main() {
	// Create a new QUIC client
	quicClient, err := gophersocks.NewQUICClient("localhost:8443")
	if err != nil {
		log.Fatal("Failed to create QUIC client:", err)
	}

	// Connect to the server
	ctx := context.Background()
	if err := quicClient.Connect(ctx); err != nil {
		log.Fatal("Failed to connect:", err)
	}
	defer quicClient.Close()

	// Send data
	message := []byte("Hello, QUIC server!")
	if err := quicClient.Send(message); err != nil {
		log.Fatal("Failed to send data:", err)
	}
	fmt.Printf("Sent: %s\n", message)

	// Receive response with timeout
	timeout := time.After(5 * time.Second)
	for {
		select {
		case <-timeout:
			log.Fatal("Timeout waiting for response")
		default:
			response, err := quicClient.Receive()
			if err != nil {
				log.Fatal("Failed to receive data:", err)
			}
			if response != nil {
				fmt.Printf("Received: %s\n", response)
				return
			}
			time.Sleep(100 * time.Millisecond) // Small delay between receive attempts
		}
	}
}
