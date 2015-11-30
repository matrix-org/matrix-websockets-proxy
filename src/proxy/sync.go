package proxy

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
)

type Syncer struct {
	UpstreamURL string

	SyncParams url.Values

	// our client for the upstream connection
	client http.Client
}

// an error returned when the /sync endpoint returns a non-200.
type SyncError struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

func (s *SyncError) Error() string {
	return string(s.Body)
}

// MakeRequest sends the sync request, and returns the body of the response,
// or an error.
//
// It keeps track of the 'next_batch' from the result, and uses it to se the
// 'since' parameter for the next call.
//
// Note that this method is not thread-safe; there should be only one concurrent
// call per Syncer.
//
// If /sync returns a non-200 response, the error returned will be a SyncError.
func (s *Syncer) MakeRequest() ([]byte, error) {
	url := s.UpstreamURL + "?" + s.SyncParams.Encode()
	log.Println("request", url)
	resp, err := s.client.Get(url)

	if err != nil {
		log.Println("Error in sync", err)
		return nil, err
	}
	defer resp.Body.Close()

	body, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading sync error response: %v", err)
	}

	log.Println("Sync response:", resp.StatusCode)
	if resp.StatusCode != 200 {
		return nil, &SyncError{resp.StatusCode, resp.Header.Get("Content-Type"), body}
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

	s.SyncParams.Set("since", next_batch)
	return body, nil
}
