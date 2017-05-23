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

	"github.com/gorilla/websocket"
	"github.com/krombel/matrix-websockets-proxy/proxy"
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

	client := proxy.NewClient(*upstreamURL, r.URL.Query().Get("access_token"))
	client.NextSyncBatch = r.URL.Query().Get("since")
	client.Filter = r.URL.Query().Get("filter")

	msg, err := client.Sync(false)
	if err != nil {
		log.Println("Error in sync", err)
		switch err.(type) {
		case *proxy.MatrixError:
			handleHttpError(w, err.(*proxy.MatrixError).HttpError)
		case *proxy.HttpError:
			handleHttpError(w, *(err.(*proxy.HttpError)))
		default:
			httpError(w, http.StatusInternalServerError)
		}
		return
	}

	upgrader := websocket.Upgrader{
		Subprotocols: []string{"m.json"},
	}
	ws, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Println(err)
		return
	}

	c := proxy.New(client, ws)
	c.SendMessage(msg)
	c.Start()
}

func httpError(w http.ResponseWriter, status int) {
	http.Error(w, http.StatusText(status), status)
}

func handleHttpError(w http.ResponseWriter, errp proxy.HttpError) {
	w.Header().Set("Content-Type", errp.ContentType)
	w.WriteHeader(errp.StatusCode)
	w.Write(errp.Body)
}
