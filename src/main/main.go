package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/websocket"
	"io"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"
)

const (
	listenPort = 8009

	// Time allowed to write a message to the peer.
	writeWait = 10 * time.Second

	// Time allowed to read the next pong message from the peer.
	pongWait = 60 * time.Second

	// Send pings to peer with this period. Must be less than pongWait.
	pingPeriod = (pongWait * 9) / 10

	// timeout for upstream /sync requests (after which it will send back
	// an empty response
	syncTimeout = 60 * time.Second

	// Maximum message size allowed from peer.
	maxMessageSize = 512

	upstreamUrl = "http://localhost:8008/_matrix/client/v2_alpha/sync"
	//upstreamUrl = "https://www.sw1v.org/_matrix/client/v2_alpha/sync"
)

func main() {
	fmt.Println("Starting websock server on port", listenPort)
	http.Handle("/test/", http.StripPrefix("/test/", http.FileServer(http.Dir("test"))))
	http.HandleFunc("/stream", serveStream)
	err := http.ListenAndServe(fmt.Sprintf(":%v", listenPort), nil)
	if err != nil {
		log.Fatal("ListenAndServe: ", err)
	}
}

func httpError(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

// handle a request to /stream
//
func serveStream(w http.ResponseWriter, r *http.Request) {
	log.Println("Got websocket request to", r.URL)

	if r.Method != "GET" {
		log.Println("Invalid method", r.Method)
		httpError(w, http.StatusMethodNotAllowed)
		return
	}

	c := newConnection(r)
	resp, err := c.sync()
	if err != nil {
		log.Println("Error in sync", err)
		httpError(w, http.StatusInternalServerError)
		return
	}

	defer resp.Body.Close()
	log.Println("Sync response:", resp.StatusCode)
	if resp.StatusCode != 200 {
		// send the error back to the client
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println("Error reading sync error response", err)
			httpError(w, http.StatusInternalServerError)
			return
		}
		log.Println("sync failed:", string(body))
		w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
		w.WriteHeader(resp.StatusCode)
		w.Write(body)
		return
	}

	upgrader := websocket.Upgrader{
		Subprotocols: []string{"m.json"},
	}

	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
	}
	c.ws = ws
	defer c.ws.Close()

	go c.writePump()

	c.handleSyncResponse(resp.Body)
	c.syncParams.Set("timeout", fmt.Sprintf("%d", syncTimeout/time.Millisecond))

	go c.syncPump()
	c.reader()
}

/* Each connection has three main goroutines:
 *
 * The writer reads messages from the 'messageSend' channel and writes them
 * out to the socket. It will stop when the socket is closed, or after a
 * close request is sent. A separate writer is required because we have to
 * ensure that only one goroutine calls the send methods concurrently.
 *
 * The reader reads messages from the socket, and processes them, writing
 * responses into the messageSend channel. It also has responsibility for
 * cleaning up: when it stops, it closes the socket and the 'quit' channel
 * (thus stopping writer and syncPump respectively). The reader is stopped
 * on errors from the websocket, which can be due to the socket being closed,
 * a close response being received, or a ping timeout.
 *
 * The syncPump calls /sync on the upstream server, and writes responses into
 * the messageSend channel. It is stopped by the 'quit' channel being closed.
 * On error, it tells the writer to send a close request, to initiate the
 * shutdown process.
 */

type matrixConnection struct {
	// The websocket connection. nil until we upgrade the connection.
	ws *websocket.Conn

	// our client for the upstream connection
	client *http.Client

	syncParams url.Values

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

	// This gets closed when the reader stops, to ensure the sync pump also
	// stops.
	quit chan bool
}

// create a new matrixConnection for the received request
func newConnection(r *http.Request) *matrixConnection {
	result := &matrixConnection{
		syncParams:  r.URL.Query(),
		messageSend: make(chan []byte, 256),
		closeSend:   make(chan websocket.CloseError, 5),
		quit:        make(chan bool),
	}

	result.syncParams.Set("timeout", "0")
	result.client = &http.Client{}

	return result
}

func (c *matrixConnection) sync() (*http.Response, error) {
	url := upstreamUrl + "?" + c.syncParams.Encode()
	log.Println("request", url)
	r, e := c.client.Get(url)
	return r, e
}

func (c *matrixConnection) reader() {
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

func (c *matrixConnection) handleMessage(message []byte) {
	log.Println("Got message:", string(message))
}

func (c *matrixConnection) handleSyncResponse(response io.ReadCloser) error {
	data, err := ioutil.ReadAll(response)
	if err != nil {
		log.Println("Error reading sync response")
		return err
	}
	response.Close()

	// we only need the 'next_batch' token, so just fish that one out
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(data, &parsed); err != nil {
		log.Println("Error parsing JSON:", err)
		return err
	}

	rm, ok := parsed["next_batch"]
	if !ok {
		log.Println("No next_batch in JSON")
		return fmt.Errorf("/sync response missing next_batch")
	}

	var next_batch string
	json.Unmarshal(rm, &next_batch)
	log.Println("Got next_batch:", next_batch)

	c.messageSend <- data

	c.syncParams.Set("since", next_batch)
	return nil
}

// poll /sync, and send the result over the websocket
func (c *matrixConnection) syncPoll() error {
	resp, err := c.sync()
	if err != nil {
		log.Println("Error performing sync", err)
		return err
	}
	if resp.StatusCode != 200 {
		body, err := ioutil.ReadAll(resp.Body)
		if err != nil {
			log.Println("Error reading sync error response", err)
			return err
		}
		log.Println("sync failed:", string(body))
		return errors.New(string(body))
	}
	if err := c.handleSyncResponse(resp.Body); err != nil {
		log.Println("Error in sync", err)
		return err
	}
	return nil
}

func (c *matrixConnection) syncPump() {
	log.Println("Starting sync pump")
	defer func() { log.Println("Sync pump stopped") }()

	for {
		// check that it's not time to exit
		select {
		case <-c.quit:
			return
		default:
		}

		if err := c.syncPoll(); err != nil {
			switch err.(type) {
			case *url.Error:
				err = err.(*url.Error).Err
			}

			errmsg := err.Error()
			if len(errmsg) > 100 {
				errmsg = errmsg[:100] + "..."
			}

			msg := websocket.CloseError{Code: websocket.CloseInternalServerErr,
				Text: errmsg}
			c.closeSend <- msg
			return
		}
	}
}

// write writes a message with the given message type and payload.
func (c *matrixConnection) write(mt int, payload []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	err := c.ws.WriteMessage(mt, payload)
	if err != nil {
		log.Println("Error sending message:", err)
	}
	return err
}

func (c *matrixConnection) writeClose(closeReason websocket.CloseError) error {
	log.Println("Sending close request:", closeReason)
	msg := websocket.FormatCloseMessage(closeReason.Code, closeReason.Text)
	return c.write(websocket.CloseMessage, msg)
}

// writePump pumps messages out to the websocket connection, and takes
// responsibility for sending pings.
func (c *matrixConnection) writePump() {
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
