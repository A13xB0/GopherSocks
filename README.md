![GopherSocks](docs/Banner.png)

# GopherSocks

GopherSocks is a versatile Go network stream wrapper that simplifies raw data transmission over TCP, UDP, and WebSocket protocols. It provides a unified interface for managing network connections, sessions, and data streaming with built-in connection management capabilities.

## Features

- 🔌 **Multi-Protocol Support**
  - TCP with length-delimited messaging
  - UDP with datagram handling
  - WebSocket support using Gorilla WebSocket
- 🔄 **Session Management**
  - Automatic connection tracking
  - Unique session identifiers
  - Connection lifecycle management
  - Graceful shutdown handling
- 🛠 **Easy-to-Use Interface**
  - Consistent API across protocols
  - Simple listener setup
  - Flexible client configuration
  - Fluent configuration API
- 🔧 **Advanced Features**
  - Configurable connection settings
  - Protocol-specific optimizations
  - Extensible architecture
  - Context-based cancellation
  - Connection timeouts
  - Maximum message size limits
  - Connection pooling
  - Structured error handling
  - Built-in logging interface

## Installation

```bash
go get github.com/A13xB0/GopherSocks
```

## Usage

### Configuration Options

GopherSocks provides a flexible configuration system using the Option pattern:

```go
// Create a listener with custom configuration
listener, err := gophersocks.NewTCPListener(
    "0.0.0.0",
    8080,
    context.Background(),
    // Configure options
    gophersocks.WithMaxLength(1024*1024), // 1MB max message size
    gophersocks.WithBufferSize(100),      // Channel buffer size
    gophersocks.WithTimeouts(30, 30),     // 30 second read/write timeouts
    gophersocks.WithMaxConnections(1000), // Maximum concurrent connections
)
```

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
session.CloseSession()
```

### TCP Listener Example

```go
package main

import (
    "context"
    "fmt"
    "time"
    gophersocks "github.com/A13xB0/GopherSocks"
    "github.com/A13xB0/GopherSocks/listener"
)

func main() {
    // Create context for graceful shutdown
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Create a new TCP listener with configuration
    tListener, err := gophersocks.NewTCPListener(
        "0.0.0.0",
        8080,
        ctx,
        gophersocks.WithMaxLength(1024*1024),
        gophersocks.WithBufferSize(100),
        gophersocks.WithTimeouts(30, 30),
    )
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
    defer session.CloseSession() // Ensure session cleanup

    for data := range session.Data() {
        fmt.Printf("Received data: %s\n", data)

        if err := session.SendToClient(data); err != nil {
            fmt.Printf("Error sending to client: %s\n", err)
            return
        }
    }
}
```

### UDP Listener Example

```go
package main

import (
    "context"
    "fmt"
    gophersocks "github.com/A13xB0/GopherSocks"
    "github.com/A13xB0/GopherSocks/listener"
)

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Create a new UDP listener with configuration
    udpListener, err := gophersocks.NewUDPListener(
        "0.0.0.0",
        8081,
        ctx,
        gophersocks.WithMaxLength(65507), // Maximum UDP datagram size
        gophersocks.WithBufferSize(1000), // Larger buffer for UDP
        gophersocks.WithTimeouts(60, 60), // Longer timeouts for UDP
    )
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
    
    go func() {
        defer session.CloseSession() // Ensure session cleanup

        for datagram := range session.Data() {
            fmt.Printf("Received datagram: %s\n", datagram)
            
            if err := session.SendToClient(datagram); err != nil {
                fmt.Printf("Error sending datagram: %s\n", err)
                return
            }
        }
    }()
}
```

### WebSocket Listener Example

```go
package main

import (
    "context"
    "fmt"
    gophersocks "github.com/A13xB0/GopherSocks"
    "github.com/A13xB0/GopherSocks/listener"
)

func main() {
    ctx, cancel := context.WithCancel(context.Background())
    defer cancel()

    // Create a new WebSocket listener with configuration
    wsListener, err := gophersocks.NewWebSocketListener(
        "0.0.0.0",
        8082,
        ctx,
        gophersocks.WithMaxLength(1024*1024),
        gophersocks.WithBufferSize(100),
        gophersocks.WithTimeouts(30, 30),
    )
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
    
    go func() {
        defer session.CloseSession() // Ensure session cleanup

        for message := range session.Data() {
            fmt.Printf("Received message: %s\n", message)
            
            if err := session.SendToClient(message); err != nil {
                fmt.Printf("Error sending message: %s\n", err)
                return
            }
        }
    }()
}
```

## Project Status

GopherSocks is under active development. Recent improvements include:

✅ Implemented flexible configuration system using Option pattern  
✅ Added context-based cancellation support  
✅ Added connection timeouts and deadlines  
✅ Implemented maximum message size limits  
✅ Added connection pooling with max connections  
✅ Improved session cleanup and resource management  
✅ Added structured error handling  
✅ Implemented logging interface  
✅ Enhanced graceful shutdown handling  

Current development priorities:
- [ ] Adding TLS support
- [ ] Implementing connection backoff/retry
- [ ] Adding metrics and monitoring
- [ ] Enhancing client implementations
- [ ] Expanding test coverage
- [ ] Adding protocol compression support
- [ ] Implementing connection keep-alive
- [ ] Adding protocol multiplexing

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
