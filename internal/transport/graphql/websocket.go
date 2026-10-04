package graphql

import (
	"context"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gorilla/websocket"
)

type websocketImplementation struct{}

type websocketConnection struct {
	*websocket.Conn
	closed chan struct{}
	once   sync.Once
}

func (websocketImplementation) Accept(w http.ResponseWriter, r *http.Request, options transport.WebsocketAcceptOptions) (transport.WebsocketConn, error) {
	upgrader := websocket.Upgrader{Subprotocols: options.Subprotocols}
	conn, err := upgrader.Upgrade(w, r, options.ResponseHeader)
	if err != nil {
		return nil, err
	}
	connection := &websocketConnection{Conn: conn, closed: make(chan struct{})}
	go connection.closeOnCancel(r.Context())
	return connection, nil
}

func (c *websocketConnection) Close() error {
	c.once.Do(func() { close(c.closed) })
	return c.Conn.Close()
}

func (c *websocketConnection) NextReader() (int, io.Reader, error) {
	kind, reader, err := c.Conn.NextReader()
	if websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseNoStatusReceived) {
		err = transport.ErrWebsocketClosed
	}
	return kind, reader, err
}

func (c *websocketConnection) WriteClose(code int, message string) error {
	return c.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(code, message), time.Now().Add(time.Second))
}

func (c *websocketConnection) closeOnCancel(ctx context.Context) {
	select {
	case <-ctx.Done():
		_ = c.WriteClose(websocket.CloseNormalClosure, "server shutdown")
		_ = c.Close()
	case <-c.closed:
	}
}
