package proxy

import (
	"bytes"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"
)

func TestExtractNextBatch(t *testing.T) {
	input := `{
		"next_batch": "s361093_69_4_8353_1",
		"presence": {
			"events": [
				"event"
			]
		},
		"rooms": {
			"join": {
				"!MbvBAvLuvGjNMUuRws:matrix.org": {}
			}
		}
	}`

	next_batch, err := extractNextBatch([]byte(input))
	if err != nil {
		t.Errorf("Expected no error, got '%v'", err)
	}
	if next_batch != "s361093_69_4_8353_1" {
		t.Errorf("Expected next batch 's361093_69_4_8353_1', got '%v'",
			next_batch)
	}
}

func TestExtractNextBatchErrors(t *testing.T) {
	tests := []struct {
		input         string
		expectedError string
	}{
		{"{}", "/sync response missing next_batch"},
		{"{", "unexpected end of JSON input"},
	}

	for _, tt := range tests {
		next_batch, err := extractNextBatch([]byte(tt.input))
		if err == nil || err.Error() != tt.expectedError {
			t.Errorf("Input %v: expected error '%v', got '%v'", tt.input,
				tt.expectedError, err)
		}
		if next_batch != "" {
			t.Errorf("Input %v: expected empty result, got '%v'", tt.input,
				next_batch)
		}
	}
}

// request to an invalid URL
func TestUrlError(t *testing.T) {
	client := NewClient("", "")
	resp, err := client.do("GET", "abc", nil, nil)
	if resp != nil {
		t.Error("Expected no response; got", string(resp))
	}
	switch err.(type) {
	case *url.Error:
		// good enough
	default:
		t.Error("Bad error type", reflect.TypeOf(err))
	}
}

// request to an endpoint which returns a non-matrix error
func TestHttpError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/abc" {
			t.Error("Bad URL path", r.URL)
		}
		http.Error(w, "out of foos", 500)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "")
	resp, err := client.do("GET", "abc", nil, nil)
	if resp != nil {
		t.Error("Expected no response; got", string(resp))
	}
	switch err.(type) {
	case *HttpError:
		httpError := err.(*HttpError)
		if !strings.HasPrefix(httpError.ContentType, "text/plain") {
			t.Error("Bad content type", httpError.ContentType)
		}
		if httpError.StatusCode != 500 {
			t.Error("Bad status code", httpError.StatusCode)
		}
		if string(httpError.Body) != "out of foos\n" {
			t.Error("Bad body", string(httpError.Body))
		}
	default:
		t.Error("Bad error type", reflect.TypeOf(err))
	}
}

// request to an endpoint which returns a matrix error
func TestMatrixError(t *testing.T) {
	errjson := []byte(`{"errcode": "M_UNKNOWN", "error": "elderberries"}`)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/abc" {
			t.Error("Bad URL path", r.URL)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(400)
		w.Write(errjson)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "")
	resp, err := client.do("GET", "abc", nil, nil)
	if resp != nil {
		t.Error("Expected no response; got", string(resp))
	}
	switch err.(type) {
	case *MatrixError:
		mxError := err.(*MatrixError)
		if !strings.HasPrefix(mxError.ContentType, "application/json") {
			t.Error("Bad content type", mxError.ContentType)
		}
		if mxError.StatusCode != 400 {
			t.Error("Bad status code", mxError.StatusCode)
		}
		if !bytes.Equal(mxError.Body, errjson) {
			t.Error("Bad body", string(mxError.Body))
		}
		if mxError.Details.ErrCode != "M_UNKNOWN" {
			t.Error("Bad errcode", mxError.Details.ErrCode)
		}
		if mxError.Details.Error != "elderberries" {
			t.Error("Bad error", mxError.Details.Error)
		}
	default:
		t.Error("Bad error type", reflect.TypeOf(err))
	}
}

func TestSend(t *testing.T) {
	respjson := []byte(`{"event_id": "EVENT_ID"}`)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/_matrix/client/r0/rooms/ROOM_ID/send/EVENT_TYPE/TXN_ID"
		if r.URL.Path != wantPath {
			t.Error("Bad URL path want:", wantPath, "got:", r.URL.Path)
		}

		wantParams := "access_token=ACCESS+TOKEN"
		if r.URL.RawQuery != wantParams {
			t.Error("Bad query string want:", wantParams, "got:", r.URL.RawQuery)
		}

		if r.Header.Get("Content-Type") != "application/json" {
			t.Error("Bad content-type", r.Header.Get("Content-Type"))
		}

		if body, err := ioutil.ReadAll(r.Body); err != nil {
			t.Error("Error reading request body", err)
		} else if string(body) != "CONTENT" {
			t.Error("Bad body", string(body))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(respjson)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ACCESS TOKEN")
	eventID, err := client.SendMessage("ROOM_ID", "EVENT_TYPE", "TXN_ID",
		[]byte("CONTENT"))

	if err != nil {
		t.Error("Error from SendMessage", err)
	}

	if eventID != "EVENT_ID" {
		t.Error("Bad event id", eventID)
	}
}
