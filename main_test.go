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

func TestConstructHostnames(t *testing.T) {
	tests := []struct {
		name          string
		containerName string
		domain        string
		ips           map[string]string
		multiNet      bool
		want          map[string]string
	}{
		{
			name:          "single network without domain",
			containerName: "web",
			domain:        "",
			ips:           map[string]string{"bridge": "172.17.0.2"},
			multiNet:      false,
			want:          map[string]string{"web": "172.17.0.2"},
		},
		{
			name:          "single network with domain",
			containerName: "web",
			domain:        "example.org",
			ips:           map[string]string{"bridge": "172.17.0.2"},
			multiNet:      false,
			want:          map[string]string{"web.example.org": "172.17.0.2"},
		},
		{
			name:          "multiple networks single mode without domain",
			containerName: "app",
			domain:        "",
			ips: map[string]string{
				"net1": "172.18.0.2",
				"net2": "172.19.0.3",
			},
			multiNet: false,
			want: map[string]string{
				"app": "172.18.0.2", // or net2, depends on map iteration
			},
		},
		{
			name:          "multiple networks multi mode without domain",
			containerName: "app",
			domain:        "",
			ips: map[string]string{
				"net1": "172.18.0.2",
				"net2": "172.19.0.3",
			},
			multiNet: true,
			want: map[string]string{
				"app.net1": "172.18.0.2",
				"app.net2": "172.19.0.3",
			},
		},
		{
			name:          "multiple networks multi mode with domain",
			containerName: "web",
			domain:        "example.org",
			ips: map[string]string{
				"net1": "172.18.0.2",
				"net2": "172.19.0.3",
			},
			multiNet: true,
			want: map[string]string{
				"web.net1.example.org": "172.18.0.2",
				"web.net2.example.org": "172.19.0.3",
			},
		},
		{
			name:          "no IPs",
			containerName: "nonet",
			domain:        "example.org",
			ips:           map[string]string{},
			multiNet:      false,
			want:          map[string]string{},
		},
		{
			name:          "empty IPs map multi mode",
			containerName: "test",
			domain:        "",
			ips:           map[string]string{},
			multiNet:      true,
			want:          map[string]string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := constructHostnames(tt.containerName, tt.domain, tt.ips, tt.multiNet)

			// Special handling for single network mode with multiple IPs
			// since map iteration order is non-deterministic
			if !tt.multiNet && len(tt.ips) > 1 {
				// Just verify we got exactly one entry with correct hostname
				if len(got) != 1 {
					t.Errorf("constructHostnames() in single network mode should return 1 entry, got %d", len(got))
					return
				}
				// Verify the hostname is correct
				expectedHostname := tt.containerName
				if tt.domain != "" {
					expectedHostname = expectedHostname + "." + tt.domain
				}
				if _, exists := got[expectedHostname]; !exists {
					t.Errorf("constructHostnames() missing expected hostname %s", expectedHostname)
				}
				// Verify the IP is one of the valid IPs
				foundValidIP := false
				for _, validIP := range tt.ips {
					if got[expectedHostname] == validIP {
						foundValidIP = true
						break
					}
				}
				if !foundValidIP {
					t.Errorf("constructHostnames() IP %s not in valid set %v", got[expectedHostname], tt.ips)
				}
				return
			}

			// For other cases, compare directly
			if len(got) != len(tt.want) {
				t.Errorf("constructHostnames() returned %d entries, want %d. Got: %v, Want: %v", len(got), len(tt.want), got, tt.want)
				return
			}

			for hostname, ip := range tt.want {
				if gotIP, exists := got[hostname]; !exists {
					t.Errorf("constructHostnames() missing hostname %s", hostname)
				} else if gotIP != ip {
					t.Errorf("constructHostnames() hostname %s has IP %s, want %s", hostname, gotIP, ip)
				}
			}
		})
	}
}
