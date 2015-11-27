package matrix_websockets

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
	maxMessageSize = 512

	goroutineCount = 4
)

type message struct {
	messageType int
	body        []byte
}

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

	send chan message

	// All goroutines reading from or writing to ws should stop doing so if this channel is closed.
	quit chan struct{}

	syncer SyncRequestor

	// Goroutines should write to this channel if they encounter an error which should close the websocket.
	closeChan chan struct{}
}

type SyncRequestor interface {
	MakeRequest() ([]byte, error)
}

// create a new MatrixConnection for the received request
func NewConnection(syncer SyncRequestor, ws *websocket.Conn) *MatrixConnection {
	result := &MatrixConnection{
		ws:        ws,
		send:      make(chan message, 256),
		quit:      make(chan struct{}),
		closeChan: make(chan struct{}, goroutineCount),
		syncer:    syncer,
	}

	return result
}

func (c *MatrixConnection) SendMessage(body []byte) {
	c.send <- message{
		websocket.TextMessage,
		body,
	}
}

func (c *MatrixConnection) StartPumps() {
	go c.closer()
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

			c.send <- message{
				websocket.CloseMessage,
				websocket.FormatCloseMessage(websocket.CloseInternalServerErr, errmsg),
			}
			c.doClose()
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

		case message := <-c.send:
			if err := c.write(message.messageType, message.body); err != nil {
				c.doClose()
			}
			if message.messageType == websocket.CloseMessage {
				c.doClose()
			}

		case <-ticker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				c.doClose()
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

func (c *MatrixConnection) reader() {
	defer log.Println("Reader stopped")
	c.ws.SetReadLimit(maxMessageSize)
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
			c.doClose()
			return
		}
		go c.handleMessage(message)
	}
}

func (c *MatrixConnection) doClose() {
	c.closeChan <- struct{}{}
}

func (c *MatrixConnection) closer() {
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error { c.ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })

	select {
	case <-c.closeChan:
		close(c.quit)
	}

	c.ws.Close()

}

func (c *MatrixConnection) handleMessage(message []byte) {
	log.Println("Got message:", string(message))
}
