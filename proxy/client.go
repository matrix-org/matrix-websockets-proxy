package proxy

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"
)

const (
	// timeout for upstream /sync requests (after which it will send back
	// an empty response)
	syncTimeout = 60 * time.Second
)

type MatrixClient struct {
	AccessToken string
	UserId      string

	// base-url for requests (e.g. "http//localhost:8008")
	url string

	Filter        string
	NextSyncBatch string

	// our client for the upstream connection
	httpClient http.Client
}

func NewClient(url string, accessToken string) *MatrixClient {
	if !strings.HasSuffix(url, "/") {
		url += "/"
	}
	return &MatrixClient{url: url, AccessToken: accessToken}
}

// an error returned when a matrix endpoint returns a non-200 whose body is
// not a matrix error
type HttpError struct {
	StatusCode  int
	ContentType string
	Body        []byte
}

func (s *HttpError) Error() string {
	return string(s.Body)
}

// an error returned when a matrix endpoint returns a standard matrix error
type MatrixError struct {
	HttpError
	Details MatrixErrorDetails
}

func (e *MatrixError) Error() string {
	return fmt.Sprintf("%s (%s)", e.Details.ErrCode, e.Details.Error)
}

type MatrixErrorDetails struct {
	ErrCode string `json:"errcode"`
	Error   string `json:"error"`
}

func (s *MatrixClient) GetUserId() (string, error) {
	if s.UserId != "" {
		return s.UserId, nil
	}

	params := url.Values{
		"access_token": {s.AccessToken},
	}
	resp, err := s.get("_matrix/client/r0/account/whoami", params)
	if err != nil {
		return "", fmt.Errorf("Error while fetching whoami")
	}

	type WhoAmIResponse struct {
		UserID string `json:"user_id"`
	}

	var response WhoAmIResponse
	if err := json.Unmarshal(resp, &response); err != nil {
		return "", fmt.Errorf("Could not Unmarshal response: %s", err)
	}

	s.UserId = response.UserID
	return s.UserId, nil
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
	if s.Filter != "" {
		params.Set("filter", s.Filter)
	}

	body, err := s.get("_matrix/client/r0/sync", params)
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

func (s *MatrixClient) SendMessage(roomID string, eventType string,
	txnID string, content []byte) (string, error) {
	return s.sendMessageOrState(false, roomID, eventType, txnID, content)
}

func (s *MatrixClient) SendReadMarkers(roomID string, content []byte) ([]byte, error) {
	path := fmt.Sprintf("_matrix/client/r0/rooms/%s/read_markers",
		url.QueryEscape(roomID))

	params := url.Values{
		"access_token": {s.AccessToken},
	}

	resp, err := s.do("POST", path, params, content)

	if err != nil {
		return nil, err
	}

	return resp, nil
}

func (s *MatrixClient) SendState(roomID string, eventType string,
	stateKey string, content []byte) (string, error) {
	return s.sendMessageOrState(true, roomID, eventType, stateKey, content)
}

func (s *MatrixClient) SendTyping(roomID string, content []byte) ([]byte, error) {
	userID, err := s.GetUserId()
	if err != nil {
		return nil, err
	}

	path := fmt.Sprintf("_matrix/client/r0/rooms/%s/typing/%s",
		url.QueryEscape(roomID),
		url.QueryEscape(userID))

	params := url.Values{
		"access_token": {s.AccessToken},
	}

	resp, err := s.do("PUT", path, params, content)

	if err != nil {
		if err.(*MatrixError).HttpError.StatusCode == 429 {
			// Message got blocked because of rate-limiting
			// => ignore it (TODO is this intended?)
			log.Println("SendTyping got rate-limited: ignore");
			return []byte("{}"), nil
		}
		return nil, err
	}

	return resp, nil
}

func (s *MatrixClient) sendMessageOrState(state bool,
	roomID string, eventType string, key string, content []byte) (string, error) {
	type Response struct {
		Event_ID string
	}

	requestType := "send"
	if state {
		requestType = "state"
	}
	path := fmt.Sprintf("_matrix/client/r0/rooms/%s/%s/%s/%s",
		url.QueryEscape(roomID),
		requestType,
		url.QueryEscape(eventType),
		url.QueryEscape(key))

	params := url.Values{
		"access_token": {s.AccessToken},
	}
	resp, err := s.do("PUT", path, params, content)

	if err != nil {
		return "", err
	}

	var sr Response
	if err := json.Unmarshal(resp, &sr); err != nil {
		return "", err
	}
	return sr.Event_ID, nil
}

// get makes an HTTP GET request to the given endpoint.
//
// It checks the response code, and if it isn't a 200, returns an HttpError or
// MatrixError.
func (s *MatrixClient) get(path string, queryParams url.Values) ([]byte, error) {
	return s.do("GET", path, queryParams, nil)
}

var accessTokenRegexp = regexp.MustCompile("(access_token=)[^&]+")

// do makes an HTTP request to the given URL.
//
// It checks the response code, and if it isn't a 200, returns a MatrixError or
// HttpError
func (s *MatrixClient) do(method string, path string, queryParams url.Values, body []byte) ([]byte, error) {
	url := s.url + path

	if queryParams != nil {
		url += "?" + queryParams.Encode()
	}
	redactedUrl := accessTokenRegexp.ReplaceAllString(url, "$1<redacted>")
	log.Println("Send request:", method, redactedUrl)

	bodyReader := bytes.NewBuffer(body)
	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		log.Println("HTTP error", err)
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.httpClient.Do(req)
	if err != nil {
		log.Println("HTTP error", err)
		return nil, err
	}
	defer resp.Body.Close()

	respBody, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("error reading request error response: %v", err)
	}

	log.Println("Response:", resp.StatusCode)
	if resp.StatusCode == 200 {
		return respBody, nil
	}

	contentType := resp.Header.Get("Content-Type")
	httpError := HttpError{resp.StatusCode, contentType, respBody}

	if contentType == "application/json" {
		matrixErr := MatrixError{HttpError: httpError}

		err = json.Unmarshal(respBody, &matrixErr.Details)

		if err == nil {
			return nil, &matrixErr
		}
		log.Println("Unable to unmarshall json error", err)
	}

	return nil, &httpError
}
