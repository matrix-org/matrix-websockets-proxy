package main

import (
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
		log.Println("sync failed:", string(body), resp.Header)
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

	go c.syncPump(resp.Body)
	go c.writePump()
	c.reader()
}


type matrixConnection struct {
	// The websocket connection. nil until we upgrade the connection.
	ws *websocket.Conn

	// our client for the upstream connection
	client *http.Client

	syncParams url.Values 

	// Buffered channel of outbound messages.
	messageSend chan []byte
	
	// Channel of sync responses
	syncSend chan io.ReadCloser
}

// create a new matrixConnection for the received request
func newConnection(r *http.Request) *matrixConnection {
	result := &matrixConnection{
		syncParams: r.URL.Query(),
		messageSend: make(chan []byte, 256),
		syncSend: make(chan io.ReadCloser),
	}

	result.client = &http.Client{} 

	return result
}

func (c *matrixConnection) sync() (*http.Response, error) {
	url := upstreamUrl + "?" + c.syncParams.Encode()
		 
	fmt.Println("Upstream URL:", url)
	return c.client.Get(url)
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
		fmt.Println("Got message: ", string(message))
	}
	fmt.Println("Closing socket")
}


func (c *matrixConnection) syncPump(initialResponse io.ReadCloser) {
	c.syncSend <- initialResponse
}


func (c *matrixConnection) writeSync(syncResponse io.ReadCloser) error {
	defer syncResponse.Close()
	
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))

	wr, err := c.ws.NextWriter(websocket.TextMessage)
	if err != nil {
		return err
	}
	defer wr.Close()
	if _, err := io.Copy(wr, syncResponse); err != nil {
		return err
	}
	return nil
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
	defer c.ws.Close()
	ticker := time.NewTicker(pingPeriod)
	defer ticker.Stop()
	for {
		select {
		case message, ok := <- c.messageSend:
			if !ok {
				c.writeClose()
				return
			}
			if err := c.write(websocket.TextMessage, message); err != nil {
				return
			}
		case reader, ok := <- c.syncSend:
			if !ok {
				c.writeClose()
				return
			}
			if err := c.writeSync(reader); err != nil {
				return
			}
		case <-ticker.C:
			if err := c.write(websocket.PingMessage, []byte{}); err != nil {
				return
			}
		}
	}
}
