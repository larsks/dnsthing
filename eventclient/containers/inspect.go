package containers

import (
	"context"
	"fmt"
	"strings"

	"github.com/moby/moby/api/types/network"
	"github.com/moby/moby/client"
)

// DockerClient defines the Docker API methods we need for container inspection.
type DockerClient interface {
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
}

// TrimContainerName removes the leading slash from Docker container names.
// This eliminates duplication that previously occurred at multiple call sites.
func TrimContainerName(name string) string {
	return strings.TrimPrefix(name, "/")
}

// getIPAddress returns IPv4 if valid, otherwise IPv6, or empty string.
// This eliminates duplication of IP extraction logic.
func getIPAddress(endpoint *network.EndpointSettings) string {
	if endpoint.IPAddress.IsValid() {
		return endpoint.IPAddress.String()
	} else if endpoint.GlobalIPv6Address.IsValid() {
		return endpoint.GlobalIPv6Address.String()
	}
	return ""
}

// GetContainerIPs retrieves all IP addresses for a container, organized by network name.
func GetContainerIPs(ctx context.Context, cli DockerClient, containerID string) (map[string]string, error) {
	inspect, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	ips := make(map[string]string)
	if inspect.Container.NetworkSettings != nil && inspect.Container.NetworkSettings.Networks != nil {
		for networkName, network := range inspect.Container.NetworkSettings.Networks {
			if ip := getIPAddress(network); ip != "" {
				ips[networkName] = ip
			}
		}
	}

	return ips, nil
}

// GetContainerIPForNetwork retrieves the container name and IP address for a specific network.
func GetContainerIPForNetwork(ctx context.Context, cli DockerClient, containerID string, networkName string) (containerName string, ip string, err error) {
	inspect, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to inspect container: %w", err)
	}

	// Extract container name (remove leading slash if present)
	name := TrimContainerName(inspect.Container.Name)

	// Look up the specific network
	if inspect.Container.NetworkSettings == nil || inspect.Container.NetworkSettings.Networks == nil {
		return "", "", fmt.Errorf("container has no network settings")
	}

	network, exists := inspect.Container.NetworkSettings.Networks[networkName]
	if !exists {
		return "", "", fmt.Errorf("container not connected to network %s", networkName)
	}

	// Get IPv4 or IPv6 address
	ipAddr := getIPAddress(network)
	if ipAddr == "" {
		return "", "", fmt.Errorf("no IP address assigned for network %s", networkName)
	}

	return name, ipAddr, nil
}

// GetContainerName retrieves the container name by inspecting the container.
// This function works even if the container has been disconnected from networks,
// as the container name is a property of the container itself.
func GetContainerName(ctx context.Context, cli DockerClient, containerID string) (string, error) {
	inspect, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", fmt.Errorf("failed to inspect container: %w", err)
	}

	containerName := TrimContainerName(inspect.Container.Name)

	if containerName == "" {
		return "", fmt.Errorf("container has empty name")
	}

	return containerName, nil
}
