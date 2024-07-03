package stream

import "github.com/A13xB0/GopherSocks/protocols"

// TCP
type TCPOptFunc func(config *protocols.TCPConfig)

func tcpDefaultConfig() protocols.TCPConfig {
	return protocols.TCPConfig{}
}

// UDP
type UDPOptFunc func(config *protocols.UDPConfig)

func udpDefaultConfig() protocols.UDPConfig {
	return protocols.UDPConfig{}
}

// Websockets
type WebsocketOptFunc func(config *protocols.WebsocketsConfig)

func websocketDefaultConfig() protocols.WebsocketsConfig {
	return protocols.WebsocketsConfig{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
}
