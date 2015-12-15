package proxy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"time"
)

const (
	// timeout for upstream /sync requests (after which it will send back
	// an empty response)
	syncTimeout = 60 * time.Second
)

type MatrixClient struct {
	AccessToken string

	UpstreamURL string

	NextSyncBatch string

	// our client for the upstream connection
	httpClient http.Client
}

// an error returned when a matrix endpoint returns a non-200.
type MatrixError struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

func (s *MatrixError) Error() string {
	return string(s.Body)
}

// Sync sends the sync request, and returns the body of the response,
// or an error.
//
// It keeps track of the 'next_batch' from the result, and uses it to se the
// 'since' parameter for the next call.
//
// If 'waitForEvents' is set, we will set a non-zero timeout value so that the
// HTTP request blocks until events are ready to be read. Otherwise the call
// will return immediately, even if there are no new events.
//
// Note that this method is not thread-safe; there should be only one concurrent
// call per MatrixClient.
//
// If /sync returns a non-200 response, the error returned will be a MatrixError.
func (s *MatrixClient) Sync(waitForEvents bool) ([]byte, error) {
	timeout := syncTimeout
	if !waitForEvents {
		timeout = 0
	}
	params := url.Values{
		"access_token": {s.AccessToken},
		"timeout":      {fmt.Sprintf("%d", timeout/time.Millisecond)},
	}
	if s.NextSyncBatch != "" {
		params.Set("since", s.NextSyncBatch)
	}
	url := s.UpstreamURL + "_matrix/client/v2_alpha/sync?" + params.Encode()

	body, err := s.get(url)
	if err != nil {
		return nil, err
	}

	// we need the 'next_batch' token, so fish that out
	next_batch, err := extractNextBatch(body)
	if err != nil {
		return nil, err
	}
	log.Println("Got next_batch:", next_batch)
	s.NextSyncBatch = next_batch

	return body, nil
}

// extractNextBatch fishes the 'next_batch' member out of the JSON response from
// /sync.
func extractNextBatch(httpBody []byte) (string, error) {
	type syncResponse struct {
		NextBatch string `json:"next_batch"`
	}
	var sr syncResponse
	if err := json.Unmarshal(httpBody, &sr); err != nil {
		return "", err
	}

	if sr.NextBatch == "" {
		return "", fmt.Errorf("/sync response missing next_batch")
	}

	return sr.NextBatch, nil
}

// get makes an HTTP GET request to the given URL.
//
// It checks the response code, and if it isn't a 200, returns a MatrixError.
func (s *MatrixClient) get(url string) ([]byte, error) {
	log.Println("GET", url)
	resp, err := s.httpClient.Get(url)

	if err != nil {
		log.Println("HTTP error", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading request error response: %v", err)
	}

	log.Println("Response:", resp.StatusCode)
	if resp.StatusCode != 200 {
		return nil, &MatrixError{resp.StatusCode, resp.Header.Get("Content-Type"), body}
	}

	return body, nil
}
