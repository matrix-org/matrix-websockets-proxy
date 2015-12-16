package proxy

import (
	"bytes"
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
