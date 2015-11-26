package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
)

type syncer struct {
	// our client for the upstream connection
	client http.Client

	syncParams url.Values
}

// an error returned when the /sync endpoint returns a non-200.
type syncError struct {
	statusCode   int
	contentType  string
	body         []byte
}

func (s *syncError) Error() string {
	return string(s.body)
}

// MakeRequest sends the sync request, and returns the body of the response,
// or an error.
//
// If /sync returns a non-200 response, the error returned will be a syncError.
func (s *syncer) MakeRequest() ([]byte, error) {
	url := upstreamUrl + "?" + s.syncParams.Encode()
	log.Println("request", url)
	resp, err := s.client.Get(url)

	if err != nil {
		log.Println("Error in sync", err)
		return nil, err
	}

	body, err := ioutil.ReadAll(resp.Body)
	resp.Body.Close()

	if err != nil {
		return nil, fmt.Errorf("Error reading sync error response: %v", err)
	}

	log.Println("Sync response:", resp.StatusCode)
	if resp.StatusCode != 200 {
		return nil, &syncError{resp.StatusCode, resp.Header.Get("Content-Type"), body}
	}

	// we need the 'next_batch' token, so fish that out
	var parsed map[string]json.RawMessage
	if err := json.Unmarshal(body, &parsed); err != nil {
		log.Println("Error parsing JSON:", err)
		return nil, err
	}

	rm, ok := parsed["next_batch"]
	if !ok {
		log.Println("No next_batch in JSON")
		return nil, fmt.Errorf("/sync response missing next_batch")
	}

	var next_batch string
	json.Unmarshal(rm, &next_batch)
	log.Println("Got next_batch:", next_batch)

	s.syncParams.Set("since", next_batch)
	return body, nil
}
