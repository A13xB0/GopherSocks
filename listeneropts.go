package gophersocks

import (
	"github.com/A13xB0/GopherSocks/listenerprotocols"
)

// TCP
type TCPOptFunc func(config *listenerprotocols.TCPConfig)

func tcpDefaultConfig() listenerprotocols.TCPConfig {
	return listenerprotocols.TCPConfig{}
}

// UDP
type UDPOptFunc func(config *listenerprotocols.UDPConfig)

func udpDefaultConfig() listenerprotocols.UDPConfig {
	return listenerprotocols.UDPConfig{}
}

// Websockets
type WebsocketOptFunc func(config *listenerprotocols.WebsocketsConfig)

func websocketDefaultConfig() listenerprotocols.WebsocketsConfig {
	return listenerprotocols.WebsocketsConfig{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
}
