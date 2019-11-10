// Inspired by https://github.com/gorilla/websocket/tree/master/examples/chat with
// adaptations for multiple rooms ("threads" in Textile parlance) and authentication.
package ws

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
	"github.com/mr-tron/base58"
	ma "github.com/multiformats/go-multiaddr"
	sym "github.com/textileio/go-textile-core/crypto/symmetric"
	"github.com/textileio/go-textile-core/thread"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageSize = 512

	// Duration to wait for a message request to complete.
	rpcCallTimeout = time.Second * 10
)

var (
	upgrader = websocket.Upgrader{
		ReadBufferSize:  1024,
		WriteBufferSize: 1024,
		CheckOrigin: func(r *http.Request) bool {
			// @todo: auth with follow and/or read key
			return true
		},
	}

	newline = []byte{'\n'}
	space   = []byte{' '}
)

// rpcCaller defines a method name and an arg list for an rpc method.
type rpcCaller struct {
	ID     string   `json:"id"`
	Method string   `json:"method"`
	Args   []string `json:"args"`
}

// rpcResponse wraps an rpc method response and error.
type rpcResponse struct {
	ID     string `json:"id"`
	Status string `json:"status"`
	Body   string `json:"body,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Client is a middleman between the websocket connection and the hub.
type Client struct {
	hub *Hub

	// The websocket connection.
	conn *websocket.Conn

	// Buffered channel of outbound messages.
	send chan []byte

	// Active threads.
	threads map[thread.ID]struct{}
}

// addThread from an address.
func (c *Client) addThread(ctx context.Context, arg ...string) (interface{}, error) {
	if arg[1] == "" {
		return nil, fmt.Errorf("follow key is required with address")
	}

	followKey, err := decodeKey(arg[1])
	if err != nil {
		return nil, err
	}
	var readKey *sym.Key
	if arg[2] != "" {
		readKey, err = decodeKey(arg[2])
		if err != nil {
			return nil, err
		}
	}
	maddr, err := ma.NewMultiaddr(arg[0])
	if err != nil {
		return nil, err
	}

	info, err := c.hub.service.AddThread(ctx, maddr, followKey, readKey)
	if err != nil {
		return nil, err
	}

	log.Debugf("added thread %s", info.ID)
	return info, err
}

// subscribe to thread updates.
func (c *Client) subscribe(ctx context.Context, arg ...string) (interface{}, error) {
	for _, t := range arg {
		id, err := thread.Decode(t)
		if err != nil {
			return nil, err
		} else {
			c.threads[id] = struct{}{}

			log.Debugf("client requested thread %s", id.String())
		}
	}
	return nil, nil
}

// readPump pumps messages from the websocket connection to the hub.
//
// The application runs readPump in a per-connection goroutine. The application
// ensures that there is at most one reader on a connection by executing all
// reads from this goroutine.
func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error {
		_ = c.conn.SetReadDeadline(time.Now().Add(pongWait))
		return nil
	})
	for {
		_, message, err := c.conn.ReadMessage()
		if err != nil {
			if websocket.IsUnexpectedCloseError(
				err,
				websocket.CloseGoingAway,
				websocket.CloseAbnormalClosure) {
				log.Errorf("error reading message: %s", err)
			}
			break
		}
		message = bytes.TrimSpace(bytes.Replace(message, newline, space, -1))

		callerID := "unknown"
		var caller rpcCaller
		var result interface{}
		if err = json.Unmarshal(message, &caller); err == nil {
			callerID = caller.ID

			ctx, cancel := context.WithTimeout(context.Background(), rpcCallTimeout)
			switch caller.Method {
			case "addThread":
				result, err = c.addThread(ctx, caller.Args...)
			case "pullThread":
				err = fmt.Errorf("todo")
			case "deleteThread":
				err = fmt.Errorf("todo")
			case "addFollower":
				err = fmt.Errorf("todo")
			case "addRecord":
				err = fmt.Errorf("todo")
			case "getRecord":
				err = fmt.Errorf("todo")
			case "subscribe":
				result, err = c.subscribe(ctx, caller.Args...)
			default:
				err = fmt.Errorf("unknown method: %s", caller.Method)
			}

			cancel()
		}

		res := &rpcResponse{ID: callerID}
		if err != nil {
			res.Status = "error"
			res.Error = err.Error()
		} else {
			res.Status = "ok"
			if result != nil {
				body, _ := json.Marshal(result)
				res.Body = string(body)
			}
		}

		resb, _ := json.Marshal(res)
		c.send <- resb
	}
}

// writePump pumps messages from the hub to the websocket connection.
//
// A goroutine running writePump is started for each connection. The
// application ensures that there is at most one writer to a connection by
// executing all writes from this goroutine.
// @todo: Handle write errors.
func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The hub closed the channel.
				_ = c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}

			w, err := c.conn.NextWriter(websocket.TextMessage)
			if err != nil {
				return
			}
			_, _ = w.Write(message)

			// Add queued chat messages to the current websocket message.
			n := len(c.send)
			for i := 0; i < n; i++ {
				_, _ = w.Write(newline)
				_, _ = w.Write(<-c.send)
			}

			if err := w.Close(); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// serveWs handles websocket requests from the peer.
func serveWs(hub *Hub, w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Error(err)
		return
	}
	client := &Client{
		hub:     hub,
		conn:    conn,
		send:    make(chan []byte, 256),
		threads: make(map[thread.ID]struct{}),
	}
	client.hub.register <- client

	log.Debug("client connected")

	// Allow collection of memory referenced by the caller by doing all work in
	// new goroutines.
	go client.writePump()
	go client.readPump()
}

// decodeKey from a string into a symmetric key.
func decodeKey(k string) (*sym.Key, error) {
	b, err := base58.Decode(k)
	if err != nil {
		return nil, err
	}
	return sym.NewKey(b)
}
