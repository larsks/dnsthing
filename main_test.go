package main

import (
	"strings"
	"testing"
	"time"

	"github.com/moby/moby/api/types/events"
)

func TestFormatEvent(t *testing.T) {
	tests := []struct {
		name       string
		msg        events.Message
		wantType   string
		wantAction string
		wantName   string
		wantID     string
	}{
		{
			name: "container start event with name",
			msg: events.Message{
				Type:   "container",
				Action: "start",
				Actor: events.Actor{
					ID: "abc123def456789012345678",
					Attributes: map[string]string{
						"name": "myapp",
					},
				},
				Time: 1234567890,
			},
			wantType:   "container",
			wantAction: "start",
			wantName:   "myapp",
			wantID:     "abc123def456",
		},
		{
			name: "image pull event",
			msg: events.Message{
				Type:   "image",
				Action: "pull",
				Actor: events.Actor{
					ID: "nginx:latest",
					Attributes: map[string]string{
						"name": "nginx:latest",
					},
				},
				Time: 1234567890,
			},
			wantType:   "image",
			wantAction: "pull",
			wantName:   "nginx:latest",
			wantID:     "nginx:latest",
		},
		{
			name: "event without name attribute",
			msg: events.Message{
				Type:   "network",
				Action: "create",
				Actor: events.Actor{
					ID:         "net123456789",
					Attributes: map[string]string{},
				},
				Time: 1234567890,
			},
			wantType:   "network",
			wantAction: "create",
			wantName:   "unknown",
			wantID:     "net123456789",
		},
		{
			name: "volume event",
			msg: events.Message{
				Type:   "volume",
				Action: "create",
				Actor: events.Actor{
					ID: "vol123",
					Attributes: map[string]string{
						"name": "myvolume",
					},
				},
				Time: 1234567890,
			},
			wantType:   "volume",
			wantAction: "create",
			wantName:   "myvolume",
			wantID:     "vol123",
		},
		{
			name: "container stop event",
			msg: events.Message{
				Type:   "container",
				Action: "stop",
				Actor: events.Actor{
					ID: "short123",
					Attributes: map[string]string{
						"name": "webapp",
					},
				},
				Time: 1234567890,
			},
			wantType:   "container",
			wantAction: "stop",
			wantName:   "webapp",
			wantID:     "short123",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := formatEvent(tt.msg)

			// Verify timestamp format (should be RFC3339)
			if !strings.Contains(got, "[") || !strings.Contains(got, "]") {
				t.Errorf("formatEvent() missing timestamp brackets")
			}

			// Verify type/action
			expectedTypeAction := tt.wantType + "/" + tt.wantAction
			if !strings.Contains(got, expectedTypeAction) {
				t.Errorf("formatEvent() missing type/action, want %s in %s", expectedTypeAction, got)
			}

			// Verify name
			expectedName := "name=" + tt.wantName
			if !strings.Contains(got, expectedName) {
				t.Errorf("formatEvent() missing name, want %s in %s", expectedName, got)
			}

			// Verify ID
			expectedID := "id=" + tt.wantID
			if !strings.Contains(got, expectedID) {
				t.Errorf("formatEvent() missing id, want %s in %s", expectedID, got)
			}
		})
	}
}

func TestFormatEventTimestamp(t *testing.T) {
	// Test that timestamp is correctly formatted in RFC3339
	msg := events.Message{
		Type:   "container",
		Action: "start",
		Actor: events.Actor{
			ID: "test123",
			Attributes: map[string]string{
				"name": "testcontainer",
			},
		},
		Time: 1234567890,
	}

	got := formatEvent(msg)

	// Parse the expected timestamp
	expectedTime := time.Unix(1234567890, 0).Format(time.RFC3339)
	if !strings.Contains(got, expectedTime) {
		t.Errorf("formatEvent() timestamp format incorrect, want %s in %s", expectedTime, got)
	}
}

func TestFormatEventIDTruncation(t *testing.T) {
	// Test that long IDs are truncated to 12 characters
	longID := "abcdefghijklmnopqrstuvwxyz123456789"
	msg := events.Message{
		Type:   "container",
		Action: "create",
		Actor: events.Actor{
			ID: longID,
			Attributes: map[string]string{
				"name": "test",
			},
		},
		Time: 1234567890,
	}

	got := formatEvent(msg)

	// Should contain the first 12 chars
	expectedID := "id=" + longID[:12]
	if !strings.Contains(got, expectedID) {
		t.Errorf("formatEvent() ID not truncated correctly, want %s in %s", expectedID, got)
	}

	// Should NOT contain the full long ID
	fullID := "id=" + longID
	if strings.Contains(got, fullID) {
		t.Errorf("formatEvent() ID not truncated, found full ID in %s", got)
	}
}
