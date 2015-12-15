package proxy

import (
	"encoding/json"
	"regexp"
	"strings"
	"testing"
)

func TestBadMessage(t *testing.T) {
	req := ""
	resp := handleRequest([]byte(req))
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
	req := `{"id": "1234", "method": "ping"}`
	resp := handleRequest([]byte(req))
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
