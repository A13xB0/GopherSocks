package streamProtocols

const (
	UDP = 1
	TCP = 2
)

type StreamType int

type Packet struct {
	Streamtype StreamType
	Addr       any
	Data       []byte
}
