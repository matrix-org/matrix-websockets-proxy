#!/bin/bash

set -ex

export GOPATH=`pwd`/gopath
cd $GOPATH/src/github.com/matrix-org/matrix-websockets-proxy
go get -t -v ./proxy
go build
go test -v ./proxy

unformatted=$(find . -name '*.go' -print0 | xargs -0 gofmt -l)
[ -z "$unformatted" ] || {
    echo "Unformatted files:"
    echo $unformatted
    exit 1
}
