package listener

import (
	"fmt"
	"github.com/gorilla/websocket"
	"golang.org/x/net/context"
	"net/url"
	"reflect"
	"testing"
	"time"
)

const (
	wsHost = "127.0.0.1"
	wsPort = 9002
)

func TestWSListenerReceiveSingleMessage(t *testing.T) {
	//Setup
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	tListener, err := NewWebSocket(wsHost, wsPort, ctx, WebsocketsConfig{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
	})
	if err != nil {
		t.Fatal(err)
	}
	newSessionChan := make(chan Session)
	tListener.SetAnnounceNewSession(utilityGetSessionWS, newSessionChan)
	errChan := make(chan error)
	go func() {
		errChan <- tListener.StartListener()
	}()
	time.Sleep(1 * time.Second)
	if len(errChan) > 0 {
		t.Fatal(<-errChan)
	}
	// Test
	var want []byte = []byte("Hello World!")
	var got []byte
	//Send Packet
	u := url.URL{Scheme: "ws", Host: fmt.Sprintf("%s:%d", wsHost, wsPort), Path: "/"}
	c, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		t.Fatal("dial:", err)
	}
	defer c.Close()

	err = c.WriteMessage(websocket.BinaryMessage, want)
	if err != nil {
		t.Fatal("write:", err)
		return
	}
	// Receive Packet
	session := <-newSessionChan
	got = <-session.Data()
	if !reflect.DeepEqual(want, got) {
		t.Fatalf("want: %s, got: %s", want, got)
	}
}

func utilityGetSessionWS(options any, session Session) {
	options.(chan Session) <- session
}
