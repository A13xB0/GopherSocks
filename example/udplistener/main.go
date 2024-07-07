package main

import (
	"fmt"
	gophersocks "github.com/A13xB0/GopherSocks"
	"github.com/A13xB0/GopherSocks/protocols"
	"os"
	"os/signal"
	"syscall"
)

func main() {
	// Setup Listener
	host := "0.0.0.0"
	port := uint16(8001)
	uListener, err := gophersocks.NewUDPListener(host, port)
	if err != nil {
		panic(err)
	}

	// Setup signal handler
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	go stop(uListener, sigs)
	//Use stream handler announce to handle new sessions (not neccessary but adds simplicity)
	uListener.SetAnnounceNewSession(announceStreamHandler, nil)

	//Start Listening
	fmt.Printf("Starting listener on %s:%d\n", host, port)
	if err := uListener.StartReceiveStream(); err != nil {
		panic(err)
	}
}

// Do something with the active sessions, in this example I am going to start a goroutine for each session and just print and echo the data received
func echo(session protocols.Session) {
	for data := range session.Data() {
		fmt.Println(data)
		err := session.SendToClient(data)
		if err != nil {
			fmt.Printf("Session (%s) Error: %s\n", session.GetSessionID(), err)
			return
		}
	}
}

func announceStreamHandler(options any, session protocols.Session) {
	fmt.Printf("New session created %v - Session ID: %v\n", session.GetClientAddr(), session.GetSessionID())
	go echo(session)
}

func stop(listener gophersocks.Listener, sigs chan os.Signal) {
	sig := <-sigs
	fmt.Println("Got signal:", sig)
	err := listener.StopReceiveStream()
	if err != nil {
		return
	}
}
