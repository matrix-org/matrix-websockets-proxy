package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"net/http"
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

	syncer := syncer{}
	syncer.syncParams = r.URL.Query()
	syncer.syncParams.Set("timeout", "0")

	msg, err := syncer.MakeRequest()
	if err != nil {
		switch err.(type) {
		case *syncError:
			errp := err.(*syncError)
			log.Println("sync failed:", string(errp.body))
			w.Header().Set("Content-Type", errp.contentType)
			w.WriteHeader(errp.statusCode)
			w.Write(errp.body)
		default:
			log.Println("Error in sync", err)
			httpError(w, http.StatusInternalServerError)
		}
		return
	}
	syncer.syncParams.Set("timeout", fmt.Sprintf("%d", syncTimeout/time.Millisecond))

	upgrader := websocket.Upgrader{
		Subprotocols: []string{"m.json"},
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	c := newConnection(&syncer, ws)
	c.messageSend <- msg
	c.startPumps()
}
