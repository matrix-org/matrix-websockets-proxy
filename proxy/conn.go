package proxy

import (
	"log"
	"net/url"
	"time"

	"github.com/gorilla/websocket"
)

const (
	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// Maximum message size allowed from peer.
	maxMessageBytes = 1024
)

type message struct {
	messageType int
	body        []byte
}

// A Connection represents a single websocket.
//
// Each connection has three main goroutines:
//
// The writer reads messages from the 'messageSend' channel and writes them
// out to the socket. It will stop when the 'quit' channel is closed. A
// separate writer is required because we have to ensure that only one
// goroutine calls the send methods concurrently.
//
// The reader reads messages from the socket, and processes them, writing
// responses into the messageSend channel. It also has responsibility for
// cleaning up: when it stops, it closes the socket and the 'quit' channel
// (thus stopping writer and syncPump). The reader is stopped
// on errors from the websocket, which can be due to the socket being closed,
// a close response being received, or a ping timeout.
//
// The syncPump calls /sync on the upstream server, and writes responses into
// the messageSend channel. It is stopped by the 'quit' channel being closed.
// On error, it tells the writer to send a close request, to initiate the
// shutdown process.
//
type Connection struct {
	ws *websocket.Conn

	send chan message

	// This gets closed when the reader stops, to ensure the sync pump and the
	// writer also stop.
	quit chan struct{}

	client *MatrixClient
}

// New creates a new Connection for an incoming websocket upgrade request
func New(client *MatrixClient, ws *websocket.Conn) *Connection {
	if client == nil {
		log.Fatalln("nil value passed as client to proxy.New()")
	}
	if ws == nil {
		log.Fatalln("nil value passed as ws to proxy.New()")
	}

	return &Connection{
		ws:     ws,
		send:   make(chan message, 256),
		quit:   make(chan struct{}),
		client: client,
	}
}

func (c *Connection) SendMessage(body []byte) {
	c.send <- message{
		websocket.TextMessage,
		body,
	}
}

func (c *Connection) SendClose(closeCode int, text string) {
	// XXX: we're allowed to send control frames from any thread, so it
	// might be easier to write the message directly to the web socket.
	c.send <- message{
		websocket.CloseMessage,
		websocket.FormatCloseMessage(closeCode, text),
	}
}

func (c *Connection) Start() {
	go c.writePump()
	go c.syncPump()
	go c.reader()
}

// syncPump repeatedly calls /sync and writes the results to the messageSend
// channel.
func (c *Connection) syncPump() {
	log.Println("Starting sync pump")
	defer log.Println("Sync pump stopped")

	for {
		// check that it's not time to exit
		select {
		case <-c.quit:
			return
		default:
		}

		body, err := c.client.Sync(true)

		if err != nil {
			log.Println("Error performing sync", err)

			// unpack url.Error, whose stringification contains a lot of
			// useless info
			switch err.(type) {
			case *url.Error:
				err = err.(*url.Error).Err
			}

			// we are constrained to 125 characters in the close message, so
			// trim the error string.
			errmsg := err.Error()
			if len(errmsg) > 100 {
				errmsg = errmsg[:100] + "..."
			}

			c.SendClose(websocket.CloseInternalServerErr, errmsg)
			return
		}

		c.SendMessage(body)
	}
}

// writePump pumps messages out to the websocket connection, and takes
// responsibility for sending pings.
func (c *Connection) writePump() {
	defer func() { log.Println("Writer stopped") }()

	// start a ticker for sending pings
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-c.quit:
			return

		case message := <-c.send:
			if err := c.write(message.messageType, message.body); err != nil {
				return
			}
			if message.messageType == websocket.CloseMessage {
				// any further attempts to write messages will fail with an
				// error, so we may as well give up now
				return
			}

		case <-ticker.C:
			if err := c.write(websocket.PingMessage, nil); err != nil {
				return
			}
		}
	}
}

// helper for writePump: writes a message with the given message type and payload.
func (c *Connection) write(messageType int, payload []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	err := c.ws.WriteMessage(messageType, payload)
	if err != nil {
		log.Println("Error sending message:", err)
	}
	return err
}

func (c *Connection) reader() {
	defer log.Println("Reader stopped")

	// close the socket when we exit
	defer c.ws.Close()

	// tell the syncPump to close when we exit
	defer close(c.quit)

	c.ws.SetReadLimit(maxMessageBytes)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error { c.ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			switch err.(type) {
			case *websocket.CloseError:
				closeErr := err.(*websocket.CloseError)
				log.Printf("Socket closed %v; stopping reader\n", *closeErr)
			default:
				log.Println("Error in reader:", err)
			}
			return
		}
		go c.handleMessage(message)
	}
}

// handleMessage processes a message received from the websocket: it determines
// the correct response, and sends it.
func (c *Connection) handleMessage(message []byte) {
	log.Println("Got message:", string(message))

	if response := handleRequest(message, c.client); response != nil {
		c.SendMessage(response)
	}
}
