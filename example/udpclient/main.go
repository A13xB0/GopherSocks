package main

import (
	"context"
	"fmt"
	"log"
	"time"

	gophersocks "github.com/A13xB0/GopherSocks"
)

func main() {
	// Create a new UDP client with custom options
	udpClient, err := gophersocks.NewUDPClient("localhost:8002",
		gophersocks.WithClientBufferSize(2048),
		gophersocks.WithClientTimeouts(time.Second*30, time.Second*30),
	)
	if err != nil {
		log.Fatal("Failed to create UDP client:", err)
	}

	// Connect to the server
	ctx := context.Background()
	if err := udpClient.Connect(ctx); err != nil {
		log.Fatal("Failed to connect:", err)
	}
	defer udpClient.Close()

	// Send data
	message := []byte("Hello, UDP server!")
	if err := udpClient.Send(message); err != nil {
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
			response, err := udpClient.Receive()
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
