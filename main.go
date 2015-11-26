package main

import (
	"fmt"
	"github.com/gorilla/websocket"
	"log"
	"matrix_websockets"
	"net/http"
	"time"
)

const (
	listenPort = 8009

	// timeout for upstream /sync requests (after which it will send back
	// an empty response)
	syncTimeout = 60 * time.Second

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

	syncer := matrix_websockets.Syncer{}
	syncer.UpstreamUrl = upstreamUrl
	syncer.SyncParams = r.URL.Query()
	syncer.SyncParams.Set("timeout", "0")

	msg, err := syncer.MakeRequest()
	if err != nil {
		switch err.(type) {
		case *matrix_websockets.SyncError:
			errp := err.(*matrix_websockets.SyncError)
			log.Println("sync failed:", string(errp.Body))
			w.Header().Set("Content-Type", errp.ContentType)
			w.WriteHeader(errp.StatusCode)
			w.Write(errp.Body)
		default:
			log.Println("Error in sync", err)
			httpError(w, http.StatusInternalServerError)
		}
		return
	}
	syncer.SyncParams.Set("timeout", fmt.Sprintf("%d", syncTimeout/time.Millisecond))

	upgrader := websocket.Upgrader{
		Subprotocols: []string{"m.json"},
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	c := matrix_websockets.NewConnection(&syncer, ws)
	c.SendMessage(msg)
	c.StartPumps()
}
