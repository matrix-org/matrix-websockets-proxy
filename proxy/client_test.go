package proxy

import (
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
