//go:build integration

package eventclient

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/larsks/dnsthing/hostfile"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// randomString generates a random string of specified length for unique container names
func randomString(length int) string {
	const charset = "abcdefghijklmnopqrstuvwxyz0123456789"
	b := make([]byte, length)
	for i := range b {
		b[i] = charset[rand.Intn(len(charset))]
	}
	return string(b)
}

// startDnsthing starts the dnsthing event client in a background goroutine
// and returns a cancel function to stop it
func startDnsthing(ctx context.Context, t *testing.T, hostsPath string, cfg Config) context.CancelFunc {
	t.Helper()

	// Create real Docker client
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("failed to create Docker client: %v", err)
	}

	// Create hostfile
	hf := hostfile.NewHostfile(hostsPath)

	// Run eventclient in background goroutine
	ctx, cancel := context.WithCancel(ctx)
	go func() {
		err := Run(ctx, cli, hf, cfg)
		if err != nil && err != context.Canceled {
			t.Logf("eventclient.Run() returned error: %v", err)
		}
	}()

	// Wait for initialization
	time.Sleep(500 * time.Millisecond)

	return cancel
}

// waitForHostEntry polls the hosts file waiting for an entry to appear or disappear
func waitForHostEntry(t *testing.T, hostsPath string, hostname string, shouldExist bool, timeout time.Duration) error {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		hf := hostfile.NewHostfile(hostsPath)
		err := hf.Read()
		if err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to read hosts file: %w", err)
		}

		_, err = hf.LookupHost(hostname)
		exists := (err == nil)

		if exists == shouldExist {
			return nil // Success
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Timeout - print current hosts file for debugging
	hf := hostfile.NewHostfile(hostsPath)
	if err := hf.Read(); err == nil {
		t.Logf("Current hosts file contents:")
		content, _ := os.ReadFile(hostsPath)
		t.Logf("%s", string(content))
	}

	return fmt.Errorf("timeout waiting for host %s (shouldExist=%v)", hostname, shouldExist)
}

// TestIntegrationContainerLifecycle tests the full lifecycle of a container
// to verify that entries are added and removed from the hosts file
func TestIntegrationContainerLifecycle(t *testing.T) {
	// Skip if running short tests
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create temp hosts file
	tmpDir := t.TempDir()
	hostsPath := filepath.Join(tmpDir, "hosts")

	// Start dnsthing with real Docker client
	ctx := context.Background()
	cfg := Config{
		HostsPath:             hostsPath,
		Domain:                "",
		MultiNet:              false,
		UpdateCommand:         "",
		MinimumUpdateInterval: 0,
	}
	cancel := startDnsthing(ctx, t, hostsPath, cfg)
	defer cancel()

	// Create Docker client for test operations
	cli, err := client.New(client.FromEnv)
	if err != nil {
		t.Fatalf("failed to create Docker client: %v", err)
	}

	// Start a test container
	containerName := "dnsthing-test-" + randomString(8)
	t.Logf("Creating container: %s", containerName)

	resp, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "infinity"},
		},
		Name: containerName,
	})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	// Ensure cleanup
	defer func() {
		t.Logf("Cleaning up container: %s", containerName)
		cli.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})
	}()

	// Start the container
	t.Logf("Starting container: %s", containerName)
	_, err = cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	if err != nil {
		t.Fatalf("failed to start container: %v", err)
	}

	// Wait for entry to appear in hosts file
	t.Logf("Waiting for entry to appear in hosts file...")
	err = waitForHostEntry(t, hostsPath, containerName, true, 10*time.Second)
	if err != nil {
		t.Fatalf("Entry did not appear: %v", err)
	}

	// Verify entry has correct IP
	hf := hostfile.NewHostfile(hostsPath)
	if err := hf.Read(); err != nil {
		t.Fatalf("failed to read hosts file: %v", err)
	}
	ip, err := hf.LookupHost(containerName)
	if err != nil {
		t.Fatalf("entry not found after appearing: %v", err)
	}
	t.Logf("✓ Container %s has IP %s in hosts file", containerName, ip)

	// Stop and remove the container
	t.Logf("Stopping container: %s", containerName)
	timeout := 10
	_, err = cli.ContainerStop(ctx, resp.ID, client.ContainerStopOptions{Timeout: &timeout})
	if err != nil {
		t.Fatalf("failed to stop container: %v", err)
	}

	t.Logf("Removing container: %s", containerName)
	_, err = cli.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{})
	if err != nil {
		t.Fatalf("failed to remove container: %v", err)
	}

	// Wait for entry to disappear from hosts file
	t.Logf("Waiting for entry to disappear from hosts file...")
	err = waitForHostEntry(t, hostsPath, containerName, false, 10*time.Second)
	if err != nil {
		t.Fatalf("Entry was not removed: %v", err)
	}

	t.Log("✓ SUCCESS: Entry was removed after container stopped")
}

func TestIntegrationNetworkDisconnect(t *testing.T) {
	ctx := context.Background()

	// Create temp hosts file
	tmpDir := t.TempDir()
	hostsPath := filepath.Join(tmpDir, "hosts")

	// Start dnsthing with multi-network mode
	cfg := Config{
		HostsPath:             hostsPath,
		Domain:                "",
		MultiNet:              true,
		UpdateCommand:         "",
		MinimumUpdateInterval: 0,
	}

	cancel := startDnsthing(ctx, t, hostsPath, cfg)
	defer cancel()

	// Create Docker client
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Fatalf("failed to create docker client: %v", err)
	}
	defer cli.Close()

	// Create a custom network
	networkName := "dnsthing-testnet-" + randomString(6)
	networkResp, err := cli.NetworkCreate(ctx, networkName, client.NetworkCreateOptions{})
	if err != nil {
		t.Fatalf("failed to create network: %v", err)
	}
	defer cli.NetworkRemove(ctx, networkResp.ID)

	// Create and start a container on the custom network
	containerName := "dnsthing-disconnect-test-" + randomString(8)
	resp, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sleep", "infinity"},
		},
		HostConfig: &container.HostConfig{
			NetworkMode: container.NetworkMode(networkName),
		},
		Name: containerName,
	})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}
	defer cli.ContainerRemove(ctx, resp.ID, client.ContainerRemoveOptions{Force: true})

	err = cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	if err != nil {
		t.Fatalf("failed to start container: %v", err)
	}

	// Wait for entry to appear
	expectedHostname := containerName + "." + networkName
	t.Logf("Waiting for entry: %s", expectedHostname)
	err = waitForHostEntry(t, hostsPath, expectedHostname, true, 5*time.Second)
	if err != nil {
		t.Fatalf("entry did not appear: %v", err)
	}
	t.Logf("✓ Entry appeared: %s", expectedHostname)

	// Read and display hosts file
	hf := hostfile.NewHostfile(hostsPath)
	err = hf.Read()
	if err != nil {
		t.Fatalf("failed to read hosts file: %v", err)
	}
	t.Logf("Hosts file before disconnect: %s", hostsPath)

	// Disconnect the container from the network
	err = cli.NetworkDisconnect(ctx, networkName, containerName, false)
	if err != nil {
		t.Fatalf("failed to disconnect container from network: %v", err)
	}
	t.Logf("Disconnected container from network")

	// Wait for entry to disappear
	t.Logf("Waiting for entry to disappear: %s", expectedHostname)
	err = waitForHostEntry(t, hostsPath, expectedHostname, false, 5*time.Second)
	if err != nil {
		t.Fatalf("entry was not removed after network disconnect: %v", err)
	}
	t.Logf("✓ SUCCESS: Entry was removed after network disconnect")
}
