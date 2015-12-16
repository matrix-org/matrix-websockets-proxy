// request.go contains functions for dealing with requests receieved from
// websocket clients.
package proxy

import (
	"encoding/json"
	"log"
)

type jsonRequest struct {
	ID     *string
	Method string
	Params *json.RawMessage
}

type jsonResponse struct {
	// this is a pointer so that it can be set to 'nil' to give a null result
	ID *string `json:"id"`

	Result interface{} `json:"result,omitempty"`

	// this is a pointer so that we can set it to be nil to omit it from the output.
	Error *MatrixErrorDetails `json:"error,omitempty"`
}

// handleRequest gets the correct response for a received message, and returns
// the json encoding
func handleRequest(request []byte, client *MatrixClient) []byte {
	var resp *jsonResponse
	var jr jsonRequest

	if err := json.Unmarshal(request, &jr); err != nil {
		log.Println("Invalid request:", err)
		resp = &jsonResponse{
			ID: jr.ID,
			Error: &MatrixErrorDetails{
				ErrCode: "M_NOT_JSON",
				Error:   err.Error(),
			},
		}
	} else {
		resp = handleRequestObject(&jr, client)
	}

	v, err := json.Marshal(resp)
	if err != nil {
		log.Print("Error marshalling:", err)
		return nil
	}
	return v
}

func handleRequestObject(req *jsonRequest, client *MatrixClient) *jsonResponse {
	// treat absent params the same as empty ones
	if req.Params == nil {
		req.Params = new(json.RawMessage)
		req.Params.UnmarshalJSON([]byte("{}"))
	}

	switch req.Method {
	case "ping":
		return handlePing(req)
	case "send":
		return handleSend(req, client)
	}

	// unknown method
	log.Println("Unknown method:", req.Method)
	return &jsonResponse{
		ID: req.ID,
		Error: &MatrixErrorDetails{
			ErrCode: "M_BAD_JSON",
			Error:   "Unknown method",
		},
	}
}

func handlePing(req *jsonRequest) *jsonResponse {
	return &jsonResponse{
		ID:     req.ID,
		Result: &struct{}{},
	}
}

func handleSend(req *jsonRequest, client *MatrixClient) *jsonResponse {
	type SendRequest struct {
		Room_ID    string
		Event_Type string
		Content    *json.RawMessage
	}
	type SendResponse struct {
		EventID string `json:"event_id"`
	}

	if req.ID == nil {
		return &jsonResponse{
			ID: req.ID,
			Error: &MatrixErrorDetails{
				ErrCode: "M_BAD_JSON",
				Error:   "Missing request ID",
			},
		}
	}

	var sendParams SendRequest
	if err := json.Unmarshal(*req.Params, &sendParams); err != nil {
		log.Println("Invalid request:", err)
		return &jsonResponse{
			ID: req.ID,
			Error: &MatrixErrorDetails{
				ErrCode: "M_BAD_JSON",
				Error:   err.Error(),
			},
		}
	}

	if sendParams.Room_ID == "" {
		return &jsonResponse{
			ID: req.ID,
			Error: &MatrixErrorDetails{
				ErrCode: "M_BAD_JSON",
				Error:   "Missing room_id",
			},
		}
	}

	if sendParams.Event_Type == "" {
		return &jsonResponse{
			ID: req.ID,
			Error: &MatrixErrorDetails{
				ErrCode: "M_BAD_JSON",
				Error:   "Missing event_type",
			},
		}
	}

	if sendParams.Content == nil {
		return &jsonResponse{
			ID: req.ID,
			Error: &MatrixErrorDetails{
				ErrCode: "M_BAD_JSON",
				Error:   "Missing content",
			},
		}
	}

	event_id, err := client.SendMessage(sendParams.Room_ID, sendParams.Event_Type, *req.ID,
		*sendParams.Content)

	if err != nil {
		switch err.(type) {
		case *MatrixError:
			return &jsonResponse{
				ID:    req.ID,
				Error: &err.(*MatrixError).Details,
			}
		default:
			return &jsonResponse{
				ID: req.ID,
				Error: &MatrixErrorDetails{
					ErrCode: "M_UNKNOWN",
					Error:   err.Error(),
				},
			}
		}
	}

	return &jsonResponse{
		ID:     req.ID,
		Result: &SendResponse{EventID: event_id},
	}
}
