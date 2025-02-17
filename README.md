![GopherSocks](docs/Banner.png)

# GopherSocks

GopherSocks is a versatile Go network stream wrapper that simplifies raw data transmission over TCP, UDP, and WebSocket protocols. It provides a unified interface for managing network connections, sessions, and data streaming with built-in connection management capabilities.

## Features

- 🔌 **Multi-Protocol Support**
  - TCP with length-delimited messaging and dual-stack (IPv4/IPv6) support
  - UDP with datagram handling and dual-stack (IPv4/IPv6) support
  - WebSocket support using Gorilla WebSocket
- 🔄 **Session Management**
  - Automatic connection tracking
  - Unique session identifiers
  - Connection lifecycle management
- 🛠 **Easy-to-Use Interface**
  - Consistent API across protocols
  - Simple listener setup
  - Flexible client configuration
- 🔧 **Customizable Options**
  - Configurable connection settings
  - Protocol-specific optimizations
  - Extensible architecture

## Installation

```bash
go get github.com/A13xB0/GopherSocks
```

## Usage

### Session Management Functions

GopherSocks provides several helper functions for managing sessions:

```go
// Get the unique session identifier
sessionID := session.GetSessionID()

// Get the client's address
clientAddr := session.GetClientAddr()

// Get the timestamp of last received data
lastReceived := session.GetLastReceived()

// Manually close a session
err := session.CloseSession()
if err != nil {
    log.Printf("Error closing session: %s\n", err)
}
```

### Dual-Stack (IPv4/IPv6) Example

```go
package main

import (
    "fmt"
    gophersocks "github.com/A13xB0/GopherSocks"
    "github.com/A13xB0/GopherSocks/listener"
)

func main() {
    // Create a dual-stack listener that accepts both IPv4 and IPv6 connections
    // Using "::" binds to all interfaces and enables dual-stack mode
    dualStackListener, err := gophersocks.NewTCPListener("::", 8080)
    if err != nil {
        panic(err)
    }

    // Set up session announcement handler
    dualStackListener.SetAnnounceNewSession(func(options any, session listener.Session) {
        // This handler will be called for both IPv4 and IPv6 connections
        addr := session.GetClientAddr()
        fmt.Printf("New connection from %v (Protocol: %v) - Session ID: %v\n",
            addr, addr.Network(), session.GetSessionID())
        
        go func() {
            for data := range session.Data() {
                fmt.Printf("Received data from %v: %s\n", addr, data)
                session.SendToClient(data)
            }
        }()
    }, nil)

    // Start listening - will accept both IPv4 and IPv6 connections
    if err := dualStackListener.StartListener(); err != nil {
        panic(err)
    }
}
```

This example demonstrates:
- Setting up a single listener that handles both IPv4 and IPv6 connections
- Using "::" to bind to all interfaces in dual-stack mode
- Identifying the protocol (IPv4/IPv6) of incoming connections
- Processing data from both types of connections

### TCP Listener Example

```go
package main

import (
    "fmt"
    gophersocks "github.com/A13xB0/GopherSocks"
    "github.com/A13xB0/GopherSocks/listener"
)

func main() {
    // Create a new TCP listener (IPv4)
    tListener, err := gophersocks.NewTCPListener("0.0.0.0", 8080)
    if err != nil {
        panic(err)
    }

    // Or use IPv6
    tListener6, err := gophersocks.NewTCPListener("::", 8080)
    if err != nil {
        panic(err)
    }

    // Set up session announcement handler
    tListener.SetAnnounceNewSession(handleNewSession, nil)

    // Start listening for connections
    if err := tListener.StartListener(); err != nil {
        panic(err)
    }
}

// Handle new session announcements
func handleNewSession(options any, session listener.Session) {
    fmt.Printf("New connection from %v - Session ID: %v\n", 
        session.GetClientAddr(), session.GetSessionID())
    
    // Start processing data for this session
    go processSessionData(session)
}

// Process incoming data for a session
func processSessionData(session listener.Session) {
    // Read data from the session's data channel
    for data := range session.Data() {
        // Print received data
        fmt.Printf("Received data: %s\n", data)

        // Echo the data back to the client
        if err := session.SendToClient(data); err != nil {
            fmt.Printf("Error sending to client: %s\n", err)
            return
        }
    }
}
```

This example demonstrates:
- Setting up a TCP listener
- Handling new client connections
- Processing incoming data
- Sending responses back to clients

### UDP Listener Example

```go
package main

import (
    "fmt"
    gophersocks "github.com/A13xB0/GopherSocks"
    "github.com/A13xB0/GopherSocks/listener"
)

func main() {
    // Create a new UDP listener (IPv4)
    udpListener, err := gophersocks.NewUDPListener("0.0.0.0", 8081)
    if err != nil {
        panic(err)
    }

    // Or use IPv6
    udpListener6, err := gophersocks.NewUDPListener("::", 8081)
    if err != nil {
        panic(err)
    }

    // Set up session announcement handler
    udpListener.SetAnnounceNewSession(handleUDPSession, nil)

    // Start listening for datagrams
    if err := udpListener.StartListener(); err != nil {
        panic(err)
    }
}

// Handle UDP sessions
func handleUDPSession(options any, session listener.Session) {
    fmt.Printf("New UDP session from %v - Session ID: %v\n", 
        session.GetClientAddr(), session.GetSessionID())
    
    // Process datagrams for this session
    go func() {
        for datagram := range session.Data() {
            fmt.Printf("Received datagram: %s\n", datagram)
            
            // Send response datagram
            if err := session.SendToClient(datagram); err != nil {
                fmt.Printf("Error sending datagram: %s\n", err)
                return
            }
        }
    }()
}
```

This example demonstrates:
- Setting up a UDP listener
- Handling UDP sessions
- Processing datagrams
- Sending response datagrams

### WebSocket Listener Example

```go
package main

import (
    "fmt"
    gophersocks "github.com/A13xB0/GopherSocks"
    "github.com/A13xB0/GopherSocks/listener"
)

func main() {
    // Create a new WebSocket listener
    wsListener, err := gophersocks.NewWebSocketListener("0.0.0.0", 8082)
    if err != nil {
        panic(err)
    }

    // Set up session announcement handler
    wsListener.SetAnnounceNewSession(handleWSSession, nil)

    // Start listening for WebSocket connections
    if err := wsListener.StartListener(); err != nil {
        panic(err)
    }
}

// Handle WebSocket sessions
func handleWSSession(options any, session listener.Session) {
    fmt.Printf("New WebSocket connection from %v - Session ID: %v\n", 
        session.GetClientAddr(), session.GetSessionID())
    
    // Process WebSocket messages
    go func() {
        for message := range session.Data() {
            fmt.Printf("Received message: %s\n", message)
            
            // Send response message
            if err := session.SendToClient(message); err != nil {
                fmt.Printf("Error sending message: %s\n", err)
                return
            }
        }
    }()
}
```

This example demonstrates:
- Setting up a WebSocket listener
- Handling WebSocket connections
- Processing WebSocket messages
- Sending response messages

## Project Status

GopherSocks is under active development. Current development priorities include:

- [ ] Adding line break delimiter support for TCP (currently only length-delimited)
- [ ] Expanding test coverage
- [ ] Implementing protocol configuration options
- [ ] Adding logger interface
- [ ] Enhancing client implementations
- [ ] Implementing goroutine workgroups
- [ ] Code documentation improvements

## Dependencies

- [github.com/google/uuid](https://github.com/google/uuid) - For unique session identification
- [github.com/gorilla/websocket](https://github.com/gorilla/websocket) - For WebSocket protocol support
- [golang.org/x/net](https://golang.org/x/net) - For extended networking capabilities

## Contributing

While this project is primarily for personal use and learning, suggestions and improvements are welcome through issues and pull requests.

## License

This project is licensed under the terms included in the [LICENSE](LICENSE) file.

## Disclaimer

This project is primarily for personal use and learning purposes. Some components may be experimental or lack comprehensive testing. Use in production environments at your own discretion.
