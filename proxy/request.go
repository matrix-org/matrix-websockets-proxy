package proxy

import (
	"encoding/json"
	"log"
)

type jsonRequest struct {
	ID     *string
	Method string
	Params map[string]interface{}
}

type jsonError struct {
	ErrCode string `json:"errcode"`
	Error   string `json:"error"`
}

type jsonResponse struct {
	// this is a pointer so that it can be set to 'nil' to give a null result
	ID *string `json:"id"`

	// these are pointers so that we can set them to be empty to omit them
	// from the output.
	Result *map[string]interface{} `json:"result,omitempty"`
	Error  *jsonError              `json:"error,omitempty"`
}

// handleRequest gets the correct response for a received message, and returns
// the json encoding
func handleRequest(request []byte) []byte {
	var resp *jsonResponse
	var jr jsonRequest

	if err := json.Unmarshal(request, &jr); err != nil {
		log.Println("Invalid request:", err)
		resp = &jsonResponse{
			ID: jr.ID,
			Error: &jsonError{
				ErrCode: "M_NOT_JSON",
				Error:   err.Error(),
			},
		}
	} else {
		resp = handleRequestObject(&jr)
	}

	v, err := json.Marshal(resp)
	if err != nil {
		log.Print("Error marshalling:", err)
		return nil
	}
	return v
}

func handleRequestObject(req *jsonRequest) *jsonResponse {
	switch req.Method {
	case "ping":
		return handlePing(req)
	}

	// unknown method
	log.Println("Unknown method:", req.Method)
	return &jsonResponse{
		ID: req.ID,
		Error: &jsonError{
			ErrCode: "M_BAD_JSON",
			Error:   "Unknown method",
		},
	}
}

func handlePing(req *jsonRequest) *jsonResponse {
	return &jsonResponse{
		ID:     req.ID,
		Result: &map[string]interface{}{},
	}
}
