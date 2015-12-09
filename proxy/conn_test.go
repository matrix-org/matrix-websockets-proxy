package proxy

import (
	"github.com/golang/mock/gomock"
	"github.com/gorilla/websocket"
	"net/http"
	"testing"
	"net"
	"bufio"
	"time"
	"io"
	"github.com/matrix-org/matrix-websockets-proxy/mocks/io"
	"sync"
)

// testSendMessage tests that Conn.SendMessage causes a text message to be
// written to the web socket
func TestSendMessage(t *testing.T) {
	ctrl := gomock.NewController(t)
	wg := &sync.WaitGroup{}

	// make the websocket
	recvPipeRdr, _ := io.Pipe()
	writer, ws, err := makeWsConn(recvPipeRdr, ctrl)
	if err != nil {
		t.Errorf("Expected no error, got '%v'", err)
		return
	}

	// now make the proxy.Connection under test
	syncer := &testSyncer{}
	conn := New(syncer, ws)

	// call SendMessage. We don't expect it to do anything until Start() is
	// called, though
	msg := []byte("bobbins")
	conn.SendMessage(msg)

	// set up the expectations for the writer
	exp := append([]byte{websocket.TextMessage | 128, byte(len(msg))}, msg...)
	wg.Add(1)
	f := func(interface{}) { wg.Done() }
	writer.EXPECT().Write(gomock.Eq(exp)).Return(len(exp), nil).Do(f)

	// tell the connection to do its thing
	conn.Start()

	// wait for it to happen
	waitTimeout(wg, 1 * time.Second)
}

// waitTimeout waits for the waitgroup, but times out after the given period
func waitTimeout(wg *sync.WaitGroup, timeout time.Duration) bool {
	c := make(chan struct{})
	go func() {
		defer close(c)
		wg.Wait()
	}()
	select {
	case <-c:
		return false // completed normally
	case <-time.After(timeout):
		return true // timed out
	}
}

// makeWsConn constructs a websocket.Conn
func makeWsConn(reader io.ReadCloser, ctrl *gomock.Controller) (*mock_io.MockWriteCloser, *websocket.Conn, error) {
	// websocket doesn't make it easy for us to get hold of a Conn. We create
	// one by mocking the HttpResponseWriter and using websocket.Upgrader
	reqHeader := http.Header{}
	reqHeader.Set("Sec-Websocket-Version", "13")
	reqHeader.Set("Connection", "upgrade")
	reqHeader.Set("Upgrade", "websocket")
	reqHeader.Set("Sec-Websocket-Key", "123")

	req := &http.Request{
		Method: "GET",
		Header: reqHeader}

	writer := mock_io.NewMockWriteCloser(ctrl)

	w := newTestRestResponseWriter(reader, writer)

	respHeader := http.Header{}

	exp := []byte("HTTP/1.1 101 Switching Protocols\r\n" +
				  "Upgrade: websocket\r\n" +
				  "Connection: Upgrade\r\n" +
				  "Sec-WebSocket-Accept: V5hz1RKy1V4JclILDswC1e3Fek0=\r\n" +
				  "\r\n")
	writer.EXPECT().Write(gomock.Eq(exp))

	upgrader := websocket.Upgrader{}
	conn, err := upgrader.Upgrade(w, req, respHeader)

	return writer, conn, err
}


// testResponseWriter is a stubbed-out http.ResponseWriter to pass into
// websocket.Upgrader.Upgrader.
type testResponseWriter struct {
	header http.Header
	ResponseCode int
	netConn *testNetConn
}

func newTestRestResponseWriter(recv io.ReadCloser, send io.WriteCloser) *testResponseWriter {
	netConn := &testNetConn{recv, send}
	return &testResponseWriter{netConn: netConn}
}

func (rw *testResponseWriter) Header() http.Header {
	return rw.header
}

func (rw *testResponseWriter) Write(buf []byte) (int, error) {
	return rw.netConn.Write(buf)
}

func (rw *testResponseWriter) WriteHeader(rc int) {
	rw.ResponseCode = rc
}

func (rw *testResponseWriter) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	rdwr := bufio.NewReadWriter(bufio.NewReader(rw.netConn.Recv),
								bufio.NewWriter(rw.netConn.Send))
	return rw.netConn, rdwr, nil
}



// testNetConn is a stubbed-out net.Conn, mostly to be returned by
// testResponseWriter.Hijack()
type testNetConn struct {
	Recv io.ReadCloser
	Send io.WriteCloser
}

func (nc *testNetConn) Read(b []byte) (n int, err error) {
	return nc.Recv.Read(b)
}

func (nc *testNetConn) Write(b []byte) (n int, err error) {
	return nc.Send.Write(b)
}

func (nc *testNetConn) Close() error {
	err1 := nc.Recv.Close()
	err2 := nc.Send.Close()
	if err1 != nil {
		return err1
	}
	return err2
}

func (nc testNetConn) LocalAddr() net.Addr {
	return nil
}

func (nc testNetConn) RemoteAddr() net.Addr {
	return nil
}

func (nc testNetConn) SetDeadline(t time.Time) error {
	return nil
}

func (nc testNetConn) SetReadDeadline(t time.Time) error {
	return nil
}

func (nc testNetConn) SetWriteDeadline(t time.Time) error {
	return nil
}


// testSyncer is a simple implementation of syncRequester. It just returns
// results from its Result channel.
type testSyncer struct {
	Result chan syncResult
}

type syncResult struct {
	Result []byte
	Err error
}

func (s *testSyncer) MakeRequest() ([]byte, error) {
	r := <- s.Result
	return r.Result, r.Err
}