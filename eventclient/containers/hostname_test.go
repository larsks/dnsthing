package containers

import (
	"testing"
)

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
			got := ConstructHostnames(tt.containerName, tt.domain, tt.ips, tt.multiNet)

			// Special handling for single network mode with multiple IPs
			// since map iteration order is non-deterministic
			if !tt.multiNet && len(tt.ips) > 1 {
				// Just verify we got exactly one entry with correct hostname
				if len(got) != 1 {
					t.Errorf("ConstructHostnames() in single network mode should return 1 entry, got %d", len(got))
					return
				}
				// Verify the hostname is correct
				expectedHostname := tt.containerName
				if tt.domain != "" {
					expectedHostname = expectedHostname + "." + tt.domain
				}
				if _, exists := got[expectedHostname]; !exists {
					t.Errorf("ConstructHostnames() missing expected hostname %s", expectedHostname)
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
					t.Errorf("ConstructHostnames() IP %s not in valid set %v", got[expectedHostname], tt.ips)
				}
				return
			}

			// For other cases, compare directly
			if len(got) != len(tt.want) {
				t.Errorf("ConstructHostnames() returned %d entries, want %d. Got: %v, Want: %v", len(got), len(tt.want), got, tt.want)
				return
			}

			for hostname, ip := range tt.want {
				if gotIP, exists := got[hostname]; !exists {
					t.Errorf("ConstructHostnames() missing hostname %s", hostname)
				} else if gotIP != ip {
					t.Errorf("ConstructHostnames() hostname %s has IP %s, want %s", hostname, gotIP, ip)
				}
			}
		})
	}
}

func TestBuildSingleNetworkHostname(t *testing.T) {
	tests := []struct {
		name          string
		containerName string
		domain        string
		want          string
	}{
		{
			name:          "without domain",
			containerName: "web",
			domain:        "",
			want:          "web",
		},
		{
			name:          "with domain",
			containerName: "web",
			domain:        "example.org",
			want:          "web.example.org",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildSingleNetworkHostname(tt.containerName, tt.domain)
			if got != tt.want {
				t.Errorf("BuildSingleNetworkHostname() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestBuildMultiNetworkHostname(t *testing.T) {
	tests := []struct {
		name          string
		containerName string
		networkName   string
		domain        string
		want          string
	}{
		{
			name:          "without domain",
			containerName: "web",
			networkName:   "bridge",
			domain:        "",
			want:          "web.bridge",
		},
		{
			name:          "with domain",
			containerName: "web",
			networkName:   "net1",
			domain:        "example.org",
			want:          "web.net1.example.org",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildMultiNetworkHostname(tt.containerName, tt.networkName, tt.domain)
			if got != tt.want {
				t.Errorf("BuildMultiNetworkHostname() = %v, want %v", got, tt.want)
			}
		})
	}
}
