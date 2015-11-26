package matrix_websockets

import (
	"github.com/gorilla/websocket"
	"log"
	"net/url"
	"time"
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
)

/* Each connection has three main goroutines:
 *
 * The writer reads messages from the 'messageSend' channel and writes them
 * out to the socket. It will stop when the 'quit' channel is closed. A
 * separate writer is required because we have to ensure that only one
 * goroutine calls the send methods concurrently.
 *
 * The reader reads messages from the socket, and processes them, writing
 * responses into the messageSend channel. It also has responsibility for
 * cleaning up: when it stops, it closes the socket and the 'quit' channel
 * (thus stopping writer and syncPump). The reader is stopped
 * on errors from the websocket, which can be due to the socket being closed,
 * a close response being received, or a ping timeout.
 *
 * The syncPump calls /sync on the upstream server, and writes responses into
 * the messageSend channel. It is stopped by the 'quit' channel being closed.
 * On error, it tells the writer to send a close request, to initiate the
 * shutdown process.
 */

type MatrixConnection struct {
	// The websocket connection
	ws *websocket.Conn

	// Buffered channel of outbound messages.
	messageSend chan []byte

	// Channel of outbound close requests. The writer will send a close
	// message to notify the client of the error, then exit.
	//
	// The channel is buffered so that if two things decide to close at the
	// same time, we don't end up with one of them blocking (the writer will
	// read at most one of the requests). XXX: is there a nicer way to do
	// this?
	closeSend chan websocket.CloseError

	// This gets closed when the reader stops, to ensure the sync pump and the
	// writer also stop.
	quit chan bool

	syncer SyncRequestor
}

type SyncRequestor interface {
	MakeRequest() ([]byte, error)
}

// create a new MatrixConnection for the received request
func NewConnection(syncer SyncRequestor, ws *websocket.Conn) *MatrixConnection {
	result := &MatrixConnection{
		ws:          ws,
		messageSend: make(chan []byte, 256),
		closeSend:   make(chan websocket.CloseError, 5),
		quit:        make(chan bool),
		syncer:      syncer,
	}

	return result
}

func (c *MatrixConnection) SendMessage(message []byte) {
	c.messageSend <- message
}

func (c *MatrixConnection) StartPumps() {
	go c.writePump()
	go c.syncPump()
	go c.reader()
}

// syncPump repeatedly calls /sync and writes the results to the messageSend
// channel.
func (c *MatrixConnection) syncPump() {
	log.Println("Starting sync pump")
	defer func() { log.Println("Sync pump stopped") }()

	for {
		// check that it's not time to exit
		select {
		case <-c.quit:
			return
		default:
		}

		body, err := c.syncer.MakeRequest()

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

			msg := websocket.CloseError{Code: websocket.CloseInternalServerErr,
				Text: errmsg}
			c.closeSend <- msg
			return
		}

		c.SendMessage(body)
	}
}

// writePump pumps messages out to the websocket connection, and takes
// responsibility for sending pings.
func (c *MatrixConnection) writePump() {
	defer func() { log.Println("Writer stopped") }()

	// start a ticker for sending pings
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()

	for {
		select {
		case <-c.quit:
			return

		case closeMessage := <-c.closeSend:
			c.writeClose(closeMessage)
			// there is nothing more that is useful for us to do. The
			// client should send a close response, which will shut down
			// the reader (and thence the sync pump). If the client fails
			// to respond, the reader will time out and shut everything
			// down anyway.
			return

		case message := <-c.messageSend:
			if err := c.write(websocket.TextMessage, message); err != nil {
				return
			}

		case <-ticker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}

// helper for writePump: writes a message with the given message type and payload.
func (c *MatrixConnection) write(mt int, payload []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	err := c.ws.WriteMessage(mt, payload)
	if err != nil {
		log.Println("Error sending message:", err)
	}
	return err
}

// helper for writePump: writes a close message
func (c *MatrixConnection) writeClose(closeReason websocket.CloseError) error {
	log.Println("Sending close request:", closeReason)
	msg := websocket.FormatCloseMessage(closeReason.Code, closeReason.Text)
	return c.write(websocket.CloseMessage, msg)
}

func (c *MatrixConnection) reader() {
	// close the socket when we exit
	defer c.ws.Close()

	// tell the syncPump to close when we exit
	defer close(c.quit)

	c.ws.SetReadLimit(maxMessageSize)
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
			break
		}
		go c.handleMessage(message)

	}
	log.Println("Reader stopped")
}

func (c *MatrixConnection) handleMessage(message []byte) {
	log.Println("Got message:", string(message))
}
