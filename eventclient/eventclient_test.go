package eventclient

import (
	"context"
	"errors"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/larsks/dnsthing/hostfile"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
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

// mockDockerClient implements the DockerClient interface for testing
type mockDockerClient struct {
	inspectFunc func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	listFunc    func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
	eventsFunc  func(ctx context.Context, options client.EventsListOptions) client.EventsResult
}

func (m *mockDockerClient) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, containerID, options)
	}
	return client.ContainerInspectResult{}, errors.New("mock inspect not implemented")
}

func (m *mockDockerClient) ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
	if m.listFunc != nil {
		return m.listFunc(ctx, options)
	}
	return client.ContainerListResult{}, errors.New("mock list not implemented")
}

func (m *mockDockerClient) Events(ctx context.Context, options client.EventsListOptions) client.EventsResult {
	if m.eventsFunc != nil {
		return m.eventsFunc(ctx, options)
	}
	return client.EventsResult{}
}

// helper function to create a temporary hostfile for testing
func createTempHostfile(t *testing.T) (*hostfile.Hostfile, string) {
	t.Helper()
	tmpDir := t.TempDir()
	hostsPath := filepath.Join(tmpDir, "hosts")
	hf := hostfile.NewHostfile(hostsPath)
	return hf, hostsPath
}

// helper to create a mock network settings with IP addresses
func mockNetworkSettings(networks map[string]string) *container.NetworkSettings {
	ns := &container.NetworkSettings{
		Networks: make(map[string]*network.EndpointSettings),
	}
	for networkName, ipStr := range networks {
		ip, _ := netip.ParseAddr(ipStr)
		ns.Networks[networkName] = &network.EndpointSettings{
			IPAddress: ip,
		}
	}
	return ns
}

func TestGetContainerIPs(t *testing.T) {
	tests := []struct {
		name        string
		containerID string
		mockResult  client.ContainerInspectResult
		mockErr     error
		want        map[string]string
		wantErr     bool
	}{
		{
			name:        "single network with IPv4",
			containerID: "container123",
			mockResult: client.ContainerInspectResult{
				Container: container.InspectResponse{
					NetworkSettings: mockNetworkSettings(map[string]string{
						"bridge": "172.17.0.2",
					}),
				},
			},
			want: map[string]string{
				"bridge": "172.17.0.2",
			},
			wantErr: false,
		},
		{
			name:        "multiple networks",
			containerID: "container456",
			mockResult: client.ContainerInspectResult{
				Container: container.InspectResponse{
					NetworkSettings: mockNetworkSettings(map[string]string{
						"net1": "172.18.0.2",
						"net2": "172.19.0.3",
					}),
				},
			},
			want: map[string]string{
				"net1": "172.18.0.2",
				"net2": "172.19.0.3",
			},
			wantErr: false,
		},
		{
			name:        "no networks",
			containerID: "container789",
			mockResult: client.ContainerInspectResult{
				Container: container.InspectResponse{
					NetworkSettings: &container.NetworkSettings{},
				},
			},
			want:    map[string]string{},
			wantErr: false,
		},
		{
			name:        "inspect error",
			containerID: "badcontainer",
			mockErr:     errors.New("container not found"),
			want:        nil,
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDockerClient{
				inspectFunc: func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
					if containerID != tt.containerID {
						t.Errorf("unexpected containerID: got %s, want %s", containerID, tt.containerID)
					}
					return tt.mockResult, tt.mockErr
				},
			}

			got, err := getContainerIPs(context.Background(), mock, tt.containerID)

			if (err != nil) != tt.wantErr {
				t.Errorf("getContainerIPs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("getContainerIPs() returned %d IPs, want %d", len(got), len(tt.want))
					return
				}
				for network, ip := range tt.want {
					if gotIP, exists := got[network]; !exists {
						t.Errorf("getContainerIPs() missing network %s", network)
					} else if gotIP != ip {
						t.Errorf("getContainerIPs() network %s has IP %s, want %s", network, gotIP, ip)
					}
				}
			}
		})
	}
}

func TestSyncRunningContainers(t *testing.T) {
	tests := []struct {
		name         string
		mockList     []container.Summary
		mockListErr  error
		mockInspects map[string]client.ContainerInspectResult
		domain       string
		multiNet     bool
		wantHosts    map[string]string
		wantErr      bool
	}{
		{
			name: "sync two running containers",
			mockList: []container.Summary{
				{
					ID:    "container1",
					Names: []string{"/web"},
				},
				{
					ID:    "container2",
					Names: []string{"/db"},
				},
			},
			mockInspects: map[string]client.ContainerInspectResult{
				"container1": {
					Container: container.InspectResponse{
						NetworkSettings: mockNetworkSettings(map[string]string{
							"bridge": "172.17.0.2",
						}),
					},
				},
				"container2": {
					Container: container.InspectResponse{
						NetworkSettings: mockNetworkSettings(map[string]string{
							"bridge": "172.17.0.3",
						}),
					},
				},
			},
			domain:   "",
			multiNet: false,
			wantHosts: map[string]string{
				"web": "172.17.0.2",
				"db":  "172.17.0.3",
			},
			wantErr: false,
		},
		{
			name: "sync with domain",
			mockList: []container.Summary{
				{
					ID:    "container1",
					Names: []string{"/web"},
				},
			},
			mockInspects: map[string]client.ContainerInspectResult{
				"container1": {
					Container: container.InspectResponse{
						NetworkSettings: mockNetworkSettings(map[string]string{
							"bridge": "172.17.0.2",
						}),
					},
				},
			},
			domain:   "example.org",
			multiNet: false,
			wantHosts: map[string]string{
				"web.example.org": "172.17.0.2",
			},
			wantErr: false,
		},
		{
			name:        "list error",
			mockListErr: errors.New("failed to list containers"),
			wantErr:     true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hf, _ := createTempHostfile(t)
			ctx := context.Background()
			wm := newWriteManager(ctx, hf, "", 0) // No update command, no throttling for tests

			mock := &mockDockerClient{
				listFunc: func(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error) {
					if tt.mockListErr != nil {
						return client.ContainerListResult{}, tt.mockListErr
					}
					return client.ContainerListResult{Items: tt.mockList}, nil
				},
				inspectFunc: func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
					if result, ok := tt.mockInspects[containerID]; ok {
						return result, nil
					}
					return client.ContainerInspectResult{}, errors.New("container not found")
				},
			}

			err := syncRunningContainers(ctx, mock, hf, wm, tt.domain, tt.multiNet)

			if (err != nil) != tt.wantErr {
				t.Errorf("syncRunningContainers() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify hostfile contents
				for hostname, expectedIP := range tt.wantHosts {
					gotIP, err := hf.LookupHost(hostname)
					if err != nil {
						t.Errorf("syncRunningContainers() missing host %s: %v", hostname, err)
					} else if gotIP != expectedIP {
						t.Errorf("syncRunningContainers() host %s has IP %s, want %s", hostname, gotIP, expectedIP)
					}
				}
			}
		})
	}
}

func TestHandleContainerStart(t *testing.T) {
	tests := []struct {
		name          string
		containerID   string
		containerName string
		domain        string
		multiNet      bool
		mockInspect   client.ContainerInspectResult
		mockErr       error
		wantHosts     map[string]string
		wantErr       bool
	}{
		{
			name:          "successful start single network",
			containerID:   "container123",
			containerName: "web",
			domain:        "",
			multiNet:      false,
			mockInspect: client.ContainerInspectResult{
				Container: container.InspectResponse{
					NetworkSettings: mockNetworkSettings(map[string]string{
						"bridge": "172.17.0.2",
					}),
				},
			},
			wantHosts: map[string]string{
				"web": "172.17.0.2",
			},
			wantErr: false,
		},
		{
			name:          "successful start with domain",
			containerID:   "container456",
			containerName: "db",
			domain:        "example.org",
			multiNet:      false,
			mockInspect: client.ContainerInspectResult{
				Container: container.InspectResponse{
					NetworkSettings: mockNetworkSettings(map[string]string{
						"bridge": "172.17.0.3",
					}),
				},
			},
			wantHosts: map[string]string{
				"db.example.org": "172.17.0.3",
			},
			wantErr: false,
		},
		{
			name:          "multi-network mode",
			containerID:   "container789",
			containerName: "app",
			domain:        "example.org",
			multiNet:      true,
			mockInspect: client.ContainerInspectResult{
				Container: container.InspectResponse{
					NetworkSettings: mockNetworkSettings(map[string]string{
						"net1": "172.18.0.2",
						"net2": "172.19.0.3",
					}),
				},
			},
			wantHosts: map[string]string{
				"app.net1.example.org": "172.18.0.2",
				"app.net2.example.org": "172.19.0.3",
			},
			wantErr: false,
		},
		{
			name:          "container with no IPs - not an error",
			containerID:   "container000",
			containerName: "nonet",
			domain:        "",
			multiNet:      false,
			mockInspect: client.ContainerInspectResult{
				Container: container.InspectResponse{
					NetworkSettings: &container.NetworkSettings{},
				},
			},
			wantHosts: map[string]string{},
			wantErr:   false,
		},
		{
			name:          "inspect error",
			containerID:   "badcontainer",
			containerName: "bad",
			mockErr:       errors.New("container not found"),
			wantErr:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hf, _ := createTempHostfile(t)
			ctx := context.Background()
			wm := newWriteManager(ctx, hf, "", 0) // No update command, no throttling for tests

			mock := &mockDockerClient{
				inspectFunc: func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
					if containerID != tt.containerID {
						t.Errorf("unexpected containerID: got %s, want %s", containerID, tt.containerID)
					}
					return tt.mockInspect, tt.mockErr
				},
			}

			err := handleContainerStart(ctx, mock, hf, wm, tt.containerID, tt.containerName, tt.domain, tt.multiNet)

			if (err != nil) != tt.wantErr {
				t.Errorf("handleContainerStart() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Verify hostfile contents
				for hostname, expectedIP := range tt.wantHosts {
					gotIP, err := hf.LookupHost(hostname)
					if err != nil {
						t.Errorf("handleContainerStart() missing host %s: %v", hostname, err)
					} else if gotIP != expectedIP {
						t.Errorf("handleContainerStart() host %s has IP %s, want %s", hostname, gotIP, expectedIP)
					}
				}

				// Verify no extra hosts were added
				if len(tt.wantHosts) == 0 {
					// For the no-IPs case, verify the file is empty or doesn't have our hostname
					if ip, err := hf.LookupHost(tt.containerName); err == nil {
						t.Errorf("handleContainerStart() unexpectedly added host %s with IP %s", tt.containerName, ip)
					}
				}
			}
		})
	}
}

func TestHandleContainerDie(t *testing.T) {
	tests := []struct {
		name               string
		containerID        string
		containerName      string
		domain             string
		multiNet           bool
		initialHosts       map[string]string
		mockInspect        client.ContainerInspectResult
		mockErr            error
		wantRemainingHosts map[string]string
		wantErr            bool
	}{
		{
			name:          "remove single network entry",
			containerID:   "container123",
			containerName: "web",
			domain:        "",
			multiNet:      false,
			initialHosts: map[string]string{
				"web": "172.17.0.2",
				"db":  "172.17.0.3",
			},
			mockInspect: client.ContainerInspectResult{
				Container: container.InspectResponse{
					NetworkSettings: mockNetworkSettings(map[string]string{
						"bridge": "172.17.0.2",
					}),
				},
			},
			wantRemainingHosts: map[string]string{
				"db": "172.17.0.3",
			},
			wantErr: false,
		},
		{
			name:          "remove with domain",
			containerID:   "container456",
			containerName: "web",
			domain:        "example.org",
			multiNet:      false,
			initialHosts: map[string]string{
				"web.example.org": "172.17.0.2",
				"db.example.org":  "172.17.0.3",
			},
			mockInspect: client.ContainerInspectResult{
				Container: container.InspectResponse{
					NetworkSettings: mockNetworkSettings(map[string]string{
						"bridge": "172.17.0.2",
					}),
				},
			},
			wantRemainingHosts: map[string]string{
				"db.example.org": "172.17.0.3",
			},
			wantErr: false,
		},
		{
			name:          "remove multi-network entries",
			containerID:   "container789",
			containerName: "app",
			domain:        "example.org",
			multiNet:      true,
			initialHosts: map[string]string{
				"app.net1.example.org": "172.18.0.2",
				"app.net2.example.org": "172.19.0.3",
				"db.example.org":       "172.17.0.4",
			},
			mockInspect: client.ContainerInspectResult{
				Container: container.InspectResponse{
					NetworkSettings: mockNetworkSettings(map[string]string{
						"net1": "172.18.0.2",
						"net2": "172.19.0.3",
					}),
				},
			},
			wantRemainingHosts: map[string]string{
				"db.example.org": "172.17.0.4",
			},
			wantErr: false,
		},
		{
			name:          "container already removed - single network mode",
			containerID:   "container999",
			containerName: "removed",
			domain:        "",
			multiNet:      false,
			initialHosts: map[string]string{
				"removed": "172.17.0.5",
			},
			mockErr:            errors.New("container not found"),
			wantRemainingHosts: map[string]string{},
			wantErr:            false, // Not a fatal error in single network mode
		},
		{
			name:          "container already removed - multi network mode",
			containerID:   "container888",
			containerName: "removed",
			domain:        "",
			multiNet:      true,
			initialHosts: map[string]string{
				"other": "172.17.0.6",
			},
			mockErr: errors.New("container not found"),
			wantRemainingHosts: map[string]string{
				"other": "172.17.0.6",
			},
			wantErr: false, // Not a fatal error, just skips removal
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			hf, _ := createTempHostfile(t)
			ctx := context.Background()
			wm := newWriteManager(ctx, hf, "", 0) // No update command, no throttling for tests

			// Populate initial hosts
			for hostname, ip := range tt.initialHosts {
				if err := hf.AddHost(hostname, ip); err != nil {
					t.Fatalf("failed to setup test: %v", err)
				}
			}
			if err := hf.Write(); err != nil {
				t.Fatalf("failed to write initial hosts: %v", err)
			}

			mock := &mockDockerClient{
				inspectFunc: func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
					if containerID != tt.containerID {
						t.Errorf("unexpected containerID: got %s, want %s", containerID, tt.containerID)
					}
					return tt.mockInspect, tt.mockErr
				},
			}

			err := handleContainerDie(ctx, mock, hf, wm, tt.containerID, tt.containerName, tt.domain, tt.multiNet)

			if (err != nil) != tt.wantErr {
				t.Errorf("handleContainerDie() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				// Reload the hostfile to verify what's on disk
				if err := hf.Read(); err != nil && !os.IsNotExist(err) {
					t.Fatalf("failed to read hostfile: %v", err)
				}

				// Verify remaining hosts
				for hostname, expectedIP := range tt.wantRemainingHosts {
					gotIP, err := hf.LookupHost(hostname)
					if err != nil {
						t.Errorf("handleContainerDie() missing remaining host %s: %v", hostname, err)
					} else if gotIP != expectedIP {
						t.Errorf("handleContainerDie() host %s has IP %s, want %s", hostname, gotIP, expectedIP)
					}
				}

				// Verify removed hosts are gone
				for hostname := range tt.initialHosts {
					if _, shouldRemain := tt.wantRemainingHosts[hostname]; !shouldRemain {
						if _, err := hf.LookupHost(hostname); err == nil {
							t.Errorf("handleContainerDie() failed to remove host %s", hostname)
						}
					}
				}
			}
		})
	}
}

func TestWriteManagerNoThrottling(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := newWriteManager(ctx, hf, "", 0) // No throttling

	// Add a host and request write
	if err := hf.AddHost("test1", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}

	// Write should happen immediately
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("requestWrite failed: %v", err)
	}

	// Verify the host was written
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hostfile: %v", err)
	}

	ip, err := hf.LookupHost("test1")
	if err != nil {
		t.Errorf("host not found after write: %v", err)
	} else if ip != "192.168.1.1" {
		t.Errorf("got IP %s, want 192.168.1.1", ip)
	}
}

func TestWriteManagerWithThrottling(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := newWriteManager(ctx, hf, "", 200*time.Millisecond) // 200ms throttle

	// First write should be immediate
	if err := hf.AddHost("test1", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}

	start := time.Now()
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("first requestWrite failed: %v", err)
	}
	firstWriteDuration := time.Since(start)

	// First write should be very fast (< 50ms)
	if firstWriteDuration > 50*time.Millisecond {
		t.Errorf("first write took %v, expected < 50ms", firstWriteDuration)
	}

	// Second write should be delayed
	if err := hf.AddHost("test2", "192.168.1.2"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}

	secondWriteStart := time.Now()
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("second requestWrite failed: %v", err)
	}
	immediateReturnDuration := time.Since(secondWriteStart)

	// The second write should have been scheduled (returned quickly)
	if immediateReturnDuration > 50*time.Millisecond {
		t.Errorf("requestWrite should return immediately, took %v", immediateReturnDuration)
	}

	// Wait for the scheduled write to complete
	time.Sleep(250 * time.Millisecond)

	// Verify both hosts were written
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hostfile: %v", err)
	}

	if _, err := hf.LookupHost("test1"); err != nil {
		t.Errorf("test1 not found: %v", err)
	}
	if _, err := hf.LookupHost("test2"); err != nil {
		t.Errorf("test2 not found after scheduled write: %v", err)
	}
}

func TestWriteManagerBatching(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := newWriteManager(ctx, hf, "", 300*time.Millisecond) // 300ms throttle

	// First write - immediate
	if err := hf.AddHost("test1", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	// Add multiple hosts rapidly (within throttle interval)
	time.Sleep(50 * time.Millisecond)
	if err := hf.AddHost("test2", "192.168.1.2"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	time.Sleep(50 * time.Millisecond)
	if err := hf.AddHost("test3", "192.168.1.3"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("third write failed: %v", err)
	}

	// Wait for scheduled write
	time.Sleep(350 * time.Millisecond)

	// Verify all hosts were written
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hostfile: %v", err)
	}

	for i := 1; i <= 3; i++ {
		hostname := "test" + string(rune('0'+i))
		if _, err := hf.LookupHost(hostname); err != nil {
			t.Errorf("%s not found: %v", hostname, err)
		}
	}
}

func TestWriteManagerUpdateCommand(t *testing.T) {
	hf, hostsPath := createTempHostfile(t)
	ctx := context.Background()

	// Create a temp file for command output
	outputPath := filepath.Join(filepath.Dir(hostsPath), "output.txt")
	defer os.Remove(outputPath)

	// Use a command that writes to the output file
	updateCmd := "echo 'updated' >> " + outputPath
	wm := newWriteManager(ctx, hf, updateCmd, 0)

	// Request a write
	if err := hf.AddHost("test", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("requestWrite failed: %v", err)
	}

	// Wait for background command to complete
	time.Sleep(100 * time.Millisecond)

	// Verify command was executed
	output, err := os.ReadFile(outputPath)
	if err != nil {
		t.Fatalf("failed to read command output: %v", err)
	}

	if !strings.Contains(string(output), "updated") {
		t.Errorf("update command was not executed, output: %s", string(output))
	}
}

func TestWriteManagerUpdateCommandFailure(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()

	// Use a command that will fail
	updateCmd := "/bin/sh -c 'exit 1'"
	wm := newWriteManager(ctx, hf, updateCmd, 0)

	// Request a write - should not fail even though command fails
	if err := hf.AddHost("test", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("requestWrite should not fail when update command fails: %v", err)
	}

	// Wait for background command to complete
	time.Sleep(100 * time.Millisecond)

	// Verify host was still written despite command failure
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hostfile: %v", err)
	}
	if _, err := hf.LookupHost("test"); err != nil {
		t.Errorf("host should be written even when update command fails: %v", err)
	}
}

func TestWriteManagerNoUpdateCommand(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := newWriteManager(ctx, hf, "", 0) // No update command

	// Request a write
	if err := hf.AddHost("test", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("requestWrite failed: %v", err)
	}

	// Verify host was written
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hostfile: %v", err)
	}
	if _, err := hf.LookupHost("test"); err != nil {
		t.Errorf("host not found: %v", err)
	}
}

func TestWriteManagerFlush(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := newWriteManager(ctx, hf, "", 500*time.Millisecond) // Long throttle

	// First write - immediate
	if err := hf.AddHost("test1", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("first write failed: %v", err)
	}

	// Second write - will be pending
	time.Sleep(50 * time.Millisecond)
	if err := hf.AddHost("test2", "192.168.1.2"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("second write failed: %v", err)
	}

	// Flush immediately (don't wait for timer)
	if err := wm.flush(); err != nil {
		t.Fatalf("flush failed: %v", err)
	}

	// Verify both hosts were written immediately
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hostfile: %v", err)
	}

	if _, err := hf.LookupHost("test1"); err != nil {
		t.Errorf("test1 not found: %v", err)
	}
	if _, err := hf.LookupHost("test2"); err != nil {
		t.Errorf("test2 not found after flush: %v", err)
	}
}

func TestWriteManagerFlushNoPending(t *testing.T) {
	hf, _ := createTempHostfile(t)
	ctx := context.Background()
	wm := newWriteManager(ctx, hf, "", 100*time.Millisecond)

	// Flush with no pending writes - should not error
	if err := wm.flush(); err != nil {
		t.Errorf("flush with no pending writes should not error: %v", err)
	}
}

func TestWriteManagerContextCancellation(t *testing.T) {
	hf, hostsPath := createTempHostfile(t)
	ctx, cancel := context.WithCancel(context.Background())

	// Create a temp file for command output
	outputPath := filepath.Join(filepath.Dir(hostsPath), "output.txt")
	defer os.Remove(outputPath)

	// Use a long-running command that should be cancelled
	updateCmd := "sleep 10 && echo 'should not appear' >> " + outputPath
	wm := newWriteManager(ctx, hf, updateCmd, 0)

	// Start a write
	if err := hf.AddHost("test", "192.168.1.1"); err != nil {
		t.Fatalf("failed to add host: %v", err)
	}
	if err := wm.requestWrite(); err != nil {
		t.Fatalf("requestWrite failed: %v", err)
	}

	// Cancel context immediately
	cancel()

	// Wait a bit
	time.Sleep(200 * time.Millisecond)

	// Verify command was cancelled (file should not exist or be empty)
	output, err := os.ReadFile(outputPath)
	if err == nil && strings.Contains(string(output), "should not appear") {
		t.Errorf("command should have been cancelled, but output was written: %s", string(output))
	}
}
