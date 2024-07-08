package gophersocks

import (
	"github.com/A13xB0/GopherSocks/listener"
)

// TCP
type TCPOptFunc func(config *listener.TCPConfig)

func tcpDefaultConfig() listener.TCPConfig {
	return listener.TCPConfig{
		MaxLength: 1024,
	}
}

func WithTCPPacketMaxLength(length uint32) TCPOptFunc {
	return func(config *listener.TCPConfig) {
		config.MaxLength = length
	}
}

// UDP
type UDPOptFunc func(config *listener.UDPConfig)

func udpDefaultConfig() listener.UDPConfig {
	return listener.UDPConfig{}
}

// Websockets
type WebsocketOptFunc func(config *listener.WebsocketsConfig)

func websocketDefaultConfig() listener.WebsocketsConfig {
	return listener.WebsocketsConfig{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	}
}

func WithWebsocketReadBufferSize(buffer int) WebsocketOptFunc {
	return func(config *listener.WebsocketsConfig) {
		config.ReadBufferSize = buffer
	}
}

func WithWebsocketWriteBufferSize(buffer int) WebsocketOptFunc {
	return func(config *listener.WebsocketsConfig) {
		config.WriteBufferSize = buffer
	}
}
