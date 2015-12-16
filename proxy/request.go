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

type resultObj interface{}

type jsonResponse struct {
	// this is a pointer so that it can be set to 'nil' to give a null result
	ID *string `json:"id"`

	Result resultObj `json:"result,omitempty"`

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
			Error: &MatrixErrorDetails{
				ErrCode: "M_NOT_JSON",
				Error:   err.Error(),
			},
		}
	} else {
		result, errdetails := handleRequestObject(&jr, client)
		resp = &jsonResponse{
			ID:     jr.ID,
			Result: result,
			Error:  errdetails,
		}
	}

	v, err := json.Marshal(resp)
	if err != nil {
		log.Print("Error marshalling:", err)
		return nil
	}
	return v
}

func handleRequestObject(req *jsonRequest, client *MatrixClient) (resultObj, *MatrixErrorDetails) {
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
	return nil, &MatrixErrorDetails{
		ErrCode: "M_BAD_JSON",
		Error:   "Unknown method",
	}
}

func handlePing(req *jsonRequest) (resultObj, *MatrixErrorDetails) {
	return &struct{}{}, nil
}

func handleSend(req *jsonRequest, client *MatrixClient) (resultObj, *MatrixErrorDetails) {
	type SendRequest struct {
		Room_ID    string
		Event_Type string
		Content    *json.RawMessage
	}
	type SendResponse struct {
		EventID string `json:"event_id"`
	}

	if req.ID == nil {
		return nil, &MatrixErrorDetails{
			ErrCode: "M_BAD_JSON",
			Error:   "Missing request ID",
		}
	}

	var sendParams SendRequest
	if err := json.Unmarshal(*req.Params, &sendParams); err != nil {
		log.Println("Invalid request:", err)
		return nil, errorToResponse(err)
	}

	if sendParams.Room_ID == "" {
		return nil, &MatrixErrorDetails{
			ErrCode: "M_BAD_JSON",
			Error:   "Missing room_id",
		}
	}

	if sendParams.Event_Type == "" {
		return nil, &MatrixErrorDetails{
			ErrCode: "M_BAD_JSON",
			Error:   "Missing event_type",
		}
	}

	if sendParams.Content == nil {
		return nil, &MatrixErrorDetails{
			ErrCode: "M_BAD_JSON",
			Error:   "Missing content",
		}
	}

	event_id, err := client.SendMessage(sendParams.Room_ID, sendParams.Event_Type, *req.ID,
		*sendParams.Content)

	if err != nil {
		return nil, errorToResponse(err)
	}

	return &SendResponse{EventID: event_id}, nil
}

// errorToResponse takes an error object and turns it into our best MatrixErrorDetails
func errorToResponse(err error) *MatrixErrorDetails {
	switch err.(type) {
	case *MatrixError:
		return &err.(*MatrixError).Details
	default:
		return &MatrixErrorDetails{
			ErrCode: "M_UNKNOWN",
			Error:   err.Error(),
		}
	}
}
