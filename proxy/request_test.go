package proxy

import (
	"encoding/json"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strings"
	"testing"
)

func TestBadMessage(t *testing.T) {
	client := MatrixClient{}
	req := ""
	resp := handleRequest([]byte(req), &client)
	respStr := string(resp)

	// some initial checks that the json is as we expect
	if !regexp.MustCompile(`"id":\s*null`).Match(resp) {
		t.Error("id != null:", respStr)
	}
	if strings.Contains(respStr, "result") {
		t.Error("response contains result:", respStr)
	}

	var respObj jsonResponse
	if err := json.Unmarshal(resp, &respObj); err != nil {
		t.Error("JSON error", err)
	} else {
		if respObj.Error.ErrCode != "M_NOT_JSON" {
			t.Error("Bad errcode:", respObj.Error.ErrCode)
		}
	}
}

func TestPing(t *testing.T) {
	client := MatrixClient{}
	req := `{"id": "1234", "method": "ping"}`
	resp := handleRequest([]byte(req), &client)
	respStr := string(resp)

	if strings.Contains(respStr, "error") {
		t.Error("response contains error:", respStr)
	}
	if !regexp.MustCompile(`"result":\s*\{\}`).Match(resp) {
		t.Error("response does not contain empty result:", respStr)
	}

	var respObj jsonResponse
	if err := json.Unmarshal(resp, &respObj); err != nil {
		t.Error("JSON error", err)
	} else {
		if *respObj.ID != "1234" {
			t.Error("Bad response ID:", respObj.ID)
		}
	}
}

func TestSendErrors(t *testing.T) {
	var respObj jsonResponse
	client := MatrixClient{}

	var req string
	var resp []byte

	// no room_id
	req = `{"id": "1234", "method": "send"}`
	resp = handleRequest([]byte(req), &client)

	if err := json.Unmarshal(resp, &respObj); err != nil {
		t.Error("JSON error", err)
	} else {
		if *respObj.ID != "1234" {
			t.Error("Bad response ID:", respObj.ID)
		}
		if respObj.Error.ErrCode != "M_BAD_JSON" {
			t.Error("Bad errcode:", respObj.Error.ErrCode)
		}
	}

	// no event_type
	req = `{"id": "1234", "method": "send", "params": {"room_id": "xyz"}}`
	resp = handleRequest([]byte(req), &client)

	if err := json.Unmarshal(resp, &respObj); err != nil {
		t.Error("JSON error", err)
	} else {
		if *respObj.ID != "1234" {
			t.Error("Bad response ID:", respObj.ID)
		}
		if respObj.Error.ErrCode != "M_BAD_JSON" {
			t.Error("Bad errcode:", respObj.Error.ErrCode)
		}
	}
}

func TestSendRequest(t *testing.T) {
	respjson := []byte(`{"event_id": "EVENT_ID"}`)

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		wantPath := "/_matrix/client/r0/rooms/ROOM_ID/send/EVENT_TYPE/1234"
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
		} else if string(body) != "{}" {
			t.Error("Bad body", string(body))
		}

		w.Header().Set("Content-Type", "application/json")
		w.Write(respjson)
	}))
	defer ts.Close()

	client := NewClient(ts.URL, "ACCESS TOKEN")

	req := `{"id": "1234", "method": "send", "params": {
		"room_id": "ROOM_ID", "content": {}, "event_type": "EVENT_TYPE"}}`
	resp := handleRequest([]byte(req), client)

	var respObj jsonResponse
	if err := json.Unmarshal(resp, &respObj); err != nil {
		t.Error("JSON error", err)
	} else {
		if *respObj.ID != "1234" {
			t.Error("Bad response ID:", respObj.ID)
		}
		if respObj.Error != nil {
			t.Error("Error from send:", respObj.Error)
		}
		if respObj.Result == nil {
			t.Error("Nil result from send")
		} else {
			results := respObj.Result.(map[string]interface{})
			if results["event_id"] != "EVENT_ID" {
				t.Error("Bad results:", results)
			}
		}
	}
}
