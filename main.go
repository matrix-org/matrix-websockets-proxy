// matrix-websockets-proxy provides a websockets interface to a Matrix
// homeserver implementing the REST API.
//
// It listens on a TCP port (localhost:8009 by default), and turns any
// incoming connections into REST requests to the configured homeserver.
//
// It exposes only the one HTTP endpoint '/stream'. It is intended
// that an SSL-aware reverse proxy (such as Apache or nginx) be used in front
// of it, to direct most requests to the homeserver, but websockets requests
// to this proxy.
//
// You can also visit http://localhost:8009/test/test.html, which is a very
// simple client for testing the websocket interface.
//
package main

import (
	"flag"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"runtime"
	"time"

	"github.com/gorilla/websocket"
	"github.com/matrix-org/matrix-websockets-proxy/proxy"
)

const (
	// timeout for upstream /sync requests (after which it will send back
	// an empty response)
	syncTimeout = 60 * time.Second
)

var port = flag.Int("port", 8009, "TCP port to listen on")
var upstreamURL = flag.String("upstream", "http://localhost:8008/", "URL of upstream server")
var testHTML *string

func init() {
	_, srcfile, _, _ := runtime.Caller(0)
	def := filepath.Join(filepath.Dir(srcfile), "test")
	testHTML = flag.String("testdir", def, "Path to the HTML test resources")
}

func main() {
	flag.Parse()

	fmt.Println("Starting websock server on port", *port)
	http.Handle("/test/", http.StripPrefix("/test/", http.FileServer(http.Dir(*testHTML))))
	http.HandleFunc("/stream", serveStream)
	err := http.ListenAndServe(fmt.Sprintf(":%d", *port), nil)
	log.Fatal("ListenAndServe: ", err)
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

	r.URL.Query().Set("timeout", "0")
	syncer := &proxy.Syncer{
		UpstreamURL: *upstreamURL + "_matrix/client/v2_alpha/sync",
		SyncParams:  r.URL.Query(),
	}

	msg, err := syncer.MakeRequest()
	if err != nil {
		switch err.(type) {
		case *proxy.SyncError:
			errp := err.(*proxy.SyncError)
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

	c := proxy.New(syncer, ws)
	c.SendMessage(msg)
	c.Start()
}

func httpError(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}
