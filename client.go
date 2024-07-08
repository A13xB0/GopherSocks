package gophersocks

type Client interface {
	Connect() error
	SendToServer()
}
