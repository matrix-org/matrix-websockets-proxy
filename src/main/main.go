package main

import (
  "encoding/json"
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
	fmt.Println("Got websocket request to", r.URL)

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
	fmt.Println("Sync response:", resp.StatusCode)
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

	upgrader := websocket.Upgrader {
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
	c.syncParams.Set("timeout", "6000")

	go c.syncPump()
	c.reader()
}

/* Each connection has three goroutines:
 *
 * The writer reads messages from the 'messageSend' channel and writes them
 * out to the socket. It will stop when the socket is closed, or the
 * quit channel is closed. It also has responsibility for cleaning up:
 * When it stops, it closes the socket and the 'quit' channel (thus stopping
 * the reader and syncPump respectively).
 *
 * The reader reads messages from the socket, and processes them, writing
 * responses into the messageSend channel. It is stopped by the socket being
 * closed. (TODO: stop the writer on errors). (TODO: we need to exchange this
 * with a per-request handler).
 *
 * The syncPump calls /sync on the upstream server, and writes responses into
 * the messageSend channel. It is stopped by the 'quit' channel being closed.
 * (TODO: stop the writer on errors).
 */


type matrixConnection struct {
	// The websocket connection. nil until we upgrade the connection.
	ws *websocket.Conn

	// our client for the upstream connection
	client *http.Client

	syncParams url.Values

	// Buffered channel of outbound messages.
	messageSend chan []byte

	// This gets closed when any of the goroutines stop, to ensure the others
	// do too.
	quit chan bool
}

// create a new matrixConnection for the received request
func newConnection(r *http.Request) *matrixConnection {
	result := &matrixConnection{
		syncParams: r.URL.Query(),
		messageSend: make(chan []byte, 256),
		quit: make(chan bool),
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
	c.ws.SetReadLimit(maxMessageSize)
	c.ws.SetReadDeadline(time.Now().Add(pongWait))
	c.ws.SetPongHandler(func(string) error { c.ws.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		_, message, err := c.ws.ReadMessage()
		if err != nil {
			break
		}
		fmt.Println("Got message:", string(message))
	}
	fmt.Println("Reader stopped")
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


func (c *matrixConnection) syncPump() {
	log.Println("Starting sync pump")
	defer func() { log.Println("Sync pump stopped") } ()

	for {
		// check that the writer hasn't shut us down
		select {
			case <- c.quit:
				return;
			default:
		}

		resp, err := c.sync()
		if err != nil {
			log.Println("Error in sync", err)
			return
		}
		if resp.StatusCode != 200 {
			body, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				log.Println("Error reading sync error response", err)
				return
			}
			log.Println("sync failed:", string(body), resp.Header)
			return
		}
		if err := c.handleSyncResponse(resp.Body); err != nil {
			log.Println("Error in sync", err)
			return
		}
	}
}

// write writes a message with the given message type and payload.
func (c *matrixConnection) write(mt int, payload []byte) error {
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	return c.ws.WriteMessage(mt, payload)
}

func (c *matrixConnection) writeClose() error {
	return c.write(websocket.CloseMessage, []byte{})
}


// writePump pumps messages out to the websocket connection, and takes
// responsibility for sending pings.
func (c *matrixConnection) writePump() {
	defer func() { log.Println("Writer stopped") } ()
	defer close(c.quit)
	defer c.ws.Close()
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
			case message := <- c.messageSend:
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
