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
		t.Errorf("Expected next batch 's361093_69_4_8353_1', got '%v'", next_batch)
	}
}
