package graphql

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/99designs/gqlgen/graphql/handler/transport"
	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

func TestWebsocketImplementationRejectsInvalidUpgrade(t *testing.T) {
	t.Parallel()
	recorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/graphql", nil)
	conn, err := (websocketImplementation{}).Accept(recorder, req, transport.WebsocketAcceptOptions{})
	require.Error(t, err)
	require.Nil(t, conn)
	require.Equal(t, http.StatusBadRequest, recorder.Code)
}

func TestWebsocketConnectionNormalClose(t *testing.T) {
	t.Parallel()
	finished := make(chan error, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (websocketImplementation{}).Accept(w, r, transport.WebsocketAcceptOptions{Subprotocols: []string{"graphql-transport-ws"}})
		if err != nil {
			finished <- err
			return
		}
		defer conn.Close()
		_, _, err = conn.NextReader()
		finished <- err
	}))
	defer server.Close()
	dialer := websocket.Dialer{Subprotocols: []string{"graphql-transport-ws"}}
	conn, response, err := dialer.Dial("ws"+strings.TrimPrefix(server.URL, "http"), nil)
	if response != nil {
		defer response.Body.Close()
	}
	require.NoError(t, err)
	defer conn.Close()
	require.Equal(t, "graphql-transport-ws", conn.Subprotocol())
	require.NoError(t, conn.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, "")))
	select {
	case err := <-finished:
		require.ErrorIs(t, err, transport.ErrWebsocketClosed)
	case <-time.After(time.Second):
		t.Fatal("websocket handler did not finish")
	}
}
