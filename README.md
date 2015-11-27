To get up and running on OSX:

    cd $GOPATH # typically ~/gocode or ~/go
    mkdir -p {src,bin}
    cd src
    git clone git@github.com:matrix-org/matrix-websockets.git
    ln -s matrix-websockets/src/matrix_websockets # this feels wrong, but it works?
    cd matrix-websockets
    go get
    $GOPATH/bin/matrix-websockets
