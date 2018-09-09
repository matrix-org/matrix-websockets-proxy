# matrix-websockets-proxy

This project provides a websockets wrapper for a Matrix.org homeserver. See
https://github.com/matrix-org/matrix-doc/blob/master/drafts/websockets.rst
for information on the protocol it implements.

To run it, you will need a working `go` installation, including a correctly-set
[GOPATH](https://golang.org/doc/code.html#GOPATH). 

You can then download and build matrix-websockets-proxy with:

    go get github.com/matrix-org/matrix-websockets-proxy

The above will clone the repository into
`$GOPATH/src/github.com/matrix-org/matrix-websockets-proxy`, and build the
binary at `$GOPATH/bin/matrix-websockets`.

To run it, just do:

    $GOPATH/bin/matrix-websockets-proxy

To rebuild, use:

    go build github.com/matrix-org/matrix-websockets-proxy

To update to the latest state, run:

    go get -u github.com/matrix-org/matrix-websockets-proxy

### nginx-integration
To make this work you can integrate this proxy using nginx as follows:
```
        location /_matrix/client/unstable/stream {
                proxy_pass http://127.0.0.1:8009/stream;
                proxy_http_version 1.1;
                proxy_set_header Upgrade $http_upgrade;
                proxy_set_header Connection "upgrade";
                proxy_set_header Origin "";
        }
```
