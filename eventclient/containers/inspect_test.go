package containers

import (
	"context"
	"errors"
	"net/netip"
	"strings"
	"testing"

	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// mockDockerClient implements the DockerClient interface for testing
type mockDockerClient struct {
	inspectFunc func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
}

func (m *mockDockerClient) ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
	if m.inspectFunc != nil {
		return m.inspectFunc(ctx, containerID, options)
	}
	return client.ContainerInspectResult{}, errors.New("mock inspect not implemented")
}

// mockNetworkSettings helper to create network settings with IP addresses
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

			got, err := GetContainerIPs(context.Background(), mock, tt.containerID)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetContainerIPs() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr {
				if len(got) != len(tt.want) {
					t.Errorf("GetContainerIPs() returned %d IPs, want %d", len(got), len(tt.want))
					return
				}
				for network, ip := range tt.want {
					if gotIP, exists := got[network]; !exists {
						t.Errorf("GetContainerIPs() missing network %s", network)
					} else if gotIP != ip {
						t.Errorf("GetContainerIPs() network %s has IP %s, want %s", network, gotIP, ip)
					}
				}
			}
		})
	}
}

func TestGetContainerIPForNetwork(t *testing.T) {
	tests := []struct {
		name            string
		containerID     string
		networkName     string
		mockResult      client.ContainerInspectResult
		mockErr         error
		wantName        string
		wantIP          string
		wantErr         bool
		wantErrContains string
	}{
		{
			name:        "valid network with IPv4",
			containerID: "container123",
			networkName: "bridge",
			mockResult: client.ContainerInspectResult{
				Container: container.InspectResponse{
					Name: "/web",
					NetworkSettings: mockNetworkSettings(map[string]string{
						"bridge": "172.17.0.2",
					}),
				},
			},
			wantName: "web",
			wantIP:   "172.17.0.2",
			wantErr:  false,
		},
		{
			name:        "container name without leading slash",
			containerID: "container456",
			networkName: "net1",
			mockResult: client.ContainerInspectResult{
				Container: container.InspectResponse{
					Name: "db",
					NetworkSettings: mockNetworkSettings(map[string]string{
						"net1": "172.18.0.5",
					}),
				},
			},
			wantName: "db",
			wantIP:   "172.18.0.5",
			wantErr:  false,
		},
		{
			name:        "network not found",
			containerID: "container789",
			networkName: "nonexistent",
			mockResult: client.ContainerInspectResult{
				Container: container.InspectResponse{
					Name: "/app",
					NetworkSettings: mockNetworkSettings(map[string]string{
						"bridge": "172.17.0.3",
					}),
				},
			},
			wantErr:         true,
			wantErrContains: "not connected to network",
		},
		{
			name:            "container inspect failure",
			containerID:     "badcontainer",
			networkName:     "bridge",
			mockErr:         errors.New("container not found"),
			wantErr:         true,
			wantErrContains: "failed to inspect",
		},
		{
			name:        "no network settings",
			containerID: "container999",
			networkName: "bridge",
			mockResult: client.ContainerInspectResult{
				Container: container.InspectResponse{
					Name:            "/test",
					NetworkSettings: nil,
				},
			},
			wantErr:         true,
			wantErrContains: "no network settings",
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

			gotName, gotIP, err := GetContainerIPForNetwork(context.Background(), mock, tt.containerID, tt.networkName)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetContainerIPForNetwork() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.wantErr {
				if tt.wantErrContains != "" && !strings.Contains(err.Error(), tt.wantErrContains) {
					t.Errorf("GetContainerIPForNetwork() error = %v, should contain %q", err, tt.wantErrContains)
				}
				return
			}

			if gotName != tt.wantName {
				t.Errorf("GetContainerIPForNetwork() name = %s, want %s", gotName, tt.wantName)
			}
			if gotIP != tt.wantIP {
				t.Errorf("GetContainerIPForNetwork() IP = %s, want %s", gotIP, tt.wantIP)
			}
		})
	}
}

func TestGetContainerName(t *testing.T) {
	tests := []struct {
		name        string
		containerID string
		mockResult  client.ContainerInspectResult
		mockErr     error
		wantName    string
		wantErr     bool
	}{
		{
			name:        "container with leading slash",
			containerID: "abc123",
			mockResult: client.ContainerInspectResult{
				Container: container.InspectResponse{
					Name: "/mycontainer",
				},
			},
			wantName: "mycontainer",
			wantErr:  false,
		},
		{
			name:        "container without leading slash",
			containerID: "def456",
			mockResult: client.ContainerInspectResult{
				Container: container.InspectResponse{
					Name: "webapp",
				},
			},
			wantName: "webapp",
			wantErr:  false,
		},
		{
			name:        "container not found",
			containerID: "missing",
			mockErr:     errors.New("container not found"),
			wantErr:     true,
		},
		{
			name:        "container with empty name",
			containerID: "emptyname",
			mockResult: client.ContainerInspectResult{
				Container: container.InspectResponse{
					Name: "",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mock := &mockDockerClient{
				inspectFunc: func(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error) {
					return tt.mockResult, tt.mockErr
				},
			}

			name, err := GetContainerName(context.Background(), mock, tt.containerID)

			if (err != nil) != tt.wantErr {
				t.Errorf("GetContainerName() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if !tt.wantErr && name != tt.wantName {
				t.Errorf("GetContainerName() = %v, want %v", name, tt.wantName)
			}
		})
	}
}
