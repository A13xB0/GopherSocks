# GopherSocks

GopherSocks is a robust, feature-rich networking library for Go that provides high-performance TCP, UDP, and WebSocket servers with enhanced session management capabilities.

## Features

- **Multiple Protocol Support**
  - TCP with length-prefixed messages
  - UDP with automatic session management
  - WebSocket with binary message support

- **Advanced Session Management**
  - Unique session IDs
  - Last received time tracking
  - Automatic session cleanup
  - Session announcement middleware

- **Configurable Options**
  - Maximum message length
  - Buffer sizes
  - Read/Write timeouts
  - Maximum concurrent connections

- **Error Handling**
  - Protocol-specific error types
  - Detailed error messages
  - Error cause tracking

- **Logging**
  - Configurable logging interface
  - Debug, Info, Warning, and Error levels
  - Default logger included

## Installation

```bash
go get github.com/A13xB0/GopherSocks
```

## Quick Start

### TCP Server

```go
package main

import (
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "time"

    gophersocks "github.com/A13xB0/GopherSocks"
)

func main() {
    // Create TCP listener with custom configuration
    tcpListener, err := gophersocks.NewTCPListener(
        "0.0.0.0",
        8001,
        gophersocks.WithMaxLength(1024*1024), // 1MB max message size
        gophersocks.WithBufferSize(100),      // Channel buffer size
        gophersocks.WithTimeouts(30, 30),     // 30 second read/write timeouts
        gophersocks.WithMaxConnections(1000), // Maximum concurrent connections
    )
    if err != nil {
        fmt.Printf("Failed to create TCP listener: %v\n", err)
        os.Exit(1)
    }

    // Setup signal handler for graceful shutdown
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

    // Set up session handler
    tcpListener.SetAnnounceNewSession(func(options any, session gophersocks.Session) {
        fmt.Printf("New TCP connection from %v - Session ID: %v\n",
            session.GetClientAddr(),
            session.GetSessionID(),
        )

        // Handle session data
        go func() {
            for data := range session.Data() {
                fmt.Printf("Received data from session %s: %s\n",
                    session.GetSessionID(),
                    string(data),
                )

                // Echo the data back
                if err := session.SendToClient(data); err != nil {
                    fmt.Printf("Failed to send data: %v\n", err)
                    return
                }
            }
        }()
    }, nil)

    // Start listening
    if err := tcpListener.StartListener(); err != nil {
        fmt.Printf("Failed to start listener: %v\n", err)
        os.Exit(1)
    }

    // Wait for shutdown signal
    <-sigs
    tcpListener.StopListener()
}
```

### UDP Server

```go
package main

import (
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "time"

    gophersocks "github.com/A13xB0/GopherSocks"
)

func main() {
    // Create UDP server with custom configuration
    udpListener, err := gophersocks.NewUDPListener(
        "0.0.0.0",
        8002,
        gophersocks.WithMaxLength(1024),      // 1KB max message size
        gophersocks.WithBufferSize(100),      // Channel buffer size
        gophersocks.WithTimeouts(30, 30),     // 30 second read/write timeouts
        gophersocks.WithMaxConnections(1000), // Maximum concurrent sessions
    )
    if err != nil {
        fmt.Printf("Failed to create UDP listener: %v\n", err)
        os.Exit(1)
    }

    // Setup signal handler
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

    // Set up session handler
    udpListener.SetAnnounceNewSession(func(options any, session gophersocks.Session) {
        fmt.Printf("New UDP session from %v - Session ID: %v\n",
            session.GetClientAddr(),
            session.GetSessionID(),
        )

        // Handle session data
        go func() {
            for data := range session.Data() {
                fmt.Printf("Received data from session %s: %s\n",
                    session.GetSessionID(),
                    string(data),
                )

                // Echo the data back
                if err := session.SendToClient(data); err != nil {
                    fmt.Printf("Failed to send data: %v\n", err)
                    return
                }
            }
        }()
    }, nil)

    // Start listening
    if err := udpListener.StartListener(); err != nil {
        fmt.Printf("Failed to start listener: %v\n", err)
        os.Exit(1)
    }

    // Wait for shutdown signal
    <-sigs
    udpListener.StopListener()
}
```

### WebSocket Server

```go
package main

import (
    "fmt"
    "os"
    "os/signal"
    "syscall"
    "time"

    gophersocks "github.com/A13xB0/GopherSocks"
)

func main() {
    // Create WebSocket server with custom configuration
    wsListener, err := gophersocks.NewWebSocketListener(
        "0.0.0.0",
        8003,
        gophersocks.WithMaxLength(1024*1024),    // 1MB max message size
        gophersocks.WithBufferSize(100),         // Channel buffer size
        gophersocks.WithTimeouts(30, 30),        // 30 second read/write timeouts
        gophersocks.WithMaxConnections(1000),    // Maximum concurrent connections
        gophersocks.WithWebSocketBufferSizes(1024, 1024),
        gophersocks.WithWebSocketPath("/ws"),
    )
    if err != nil {
        fmt.Printf("Failed to create WebSocket listener: %v\n", err)
        os.Exit(1)
    }

    // Setup signal handler
    sigs := make(chan os.Signal, 1)
    signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)

    // Set up session handler
    wsListener.SetAnnounceNewSession(func(options any, session gophersocks.Session) {
        fmt.Printf("New WebSocket connection from %v - Session ID: %v\n",
            session.GetClientAddr(),
            session.GetSessionID(),
        )

        // Handle session data
        go func() {
            for data := range session.Data() {
                fmt.Printf("Received data from session %s: %s\n",
                    session.GetSessionID(),
                    string(data),
                )

                // Echo the data back
                if err := session.SendToClient(data); err != nil {
                    fmt.Printf("Failed to send data: %v\n", err)
                    return
                }
            }
        }()
    }, nil)

    // Start listening
    if err := wsListener.StartListener(); err != nil {
        fmt.Printf("Failed to start listener: %v\n", err)
        os.Exit(1)
    }

    // Wait for shutdown signal
    <-sigs
    wsListener.StopListener()
}
```

## Configuration Options

### Common Options
- `WithMaxLength(length uint32)`: Set maximum message length
- `WithBufferSize(size int)`: Set channel buffer size
- `WithTimeouts(read, write time.Duration)`: Set read/write timeouts
- `WithMaxConnections(max int)`: Set maximum concurrent connections
- `WithLogger(logger Logger)`: Set custom logger implementation

### WebSocket-Specific Options
- `WithWebSocketBufferSizes(readSize, writeSize int)`: Set WebSocket buffer sizes
- `WithWebSocketPath(path string)`: Set WebSocket endpoint path

## Session Management

Each connection is managed as a session with:
- Unique ID
- Client address
- Data channel for receiving messages
- Last received time tracking
- Context for cancellation
- Clean shutdown handling

## Error Handling

The library provides specific error types:
- `ConnectionError`: Network connection issues
- `ConfigError`: Configuration validation errors
- `ProtocolError`: Protocol-specific errors
- `SessionError`: Session management issues

## Examples

See the `example/` directory for complete working examples:
- `example/tcplistener/`: TCP server example
- `example/udplistener/`: UDP server example
- `example/wslistener/`: WebSocket server example

## Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.
