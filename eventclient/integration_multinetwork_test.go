//go:build integration

package eventclient

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/larsks/dnsthing/hostfile"
	"github.com/moby/moby/api/types/container"
	"github.com/moby/moby/client"
)

// TestIntegrationMultiNetworkCleanup tests that entries are removed in multi-network mode
// This reproduces the bug from test-cleanup.sh
func TestIntegrationMultiNetworkCleanup(t *testing.T) {
	// Skip if running short tests
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create temp hosts file
	tmpDir := t.TempDir()
	hostsPath := filepath.Join(tmpDir, "hosts")

	// Start dnsthing with multi-network mode (same as test-cleanup.sh: -m flag)
	ctx := context.Background()
	cfg := Config{
		HostsPath:             hostsPath,
		Domain:                "",
		MultiNet:              true, // This is the -m flag
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

	// Create 3 test containers (like test-cleanup.sh)
	containerNames := []string{"dnsthing-test1-" + randomString(4), "dnsthing-test2-" + randomString(4), "dnsthing-test3-" + randomString(4)}
	containerIDs := make([]string, 3)

	for i, containerName := range containerNames {
		t.Logf("Creating container %d: %s", i+1, containerName)

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
		containerIDs[i] = resp.ID

		// Start the container
		_, err = cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
		if err != nil {
			t.Fatalf("failed to start container: %v", err)
		}
	}

	// Ensure cleanup of all containers
	defer func() {
		for i, id := range containerIDs {
			t.Logf("Cleaning up container %d", i+1)
			cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
		}
	}()

	// Wait for entries to appear in hosts file
	// In multi-network mode, entries should be: containerName.bridge
	for i, containerName := range containerNames {
		expectedHostname := containerName + ".bridge"
		t.Logf("Waiting for entry: %s", expectedHostname)
		err = waitForHostEntry(t, hostsPath, expectedHostname, true, 10*time.Second)
		if err != nil {
			t.Fatalf("Container %d entry did not appear: %v", i+1, err)
		}
		t.Logf("✓ Entry found: %s", expectedHostname)
	}

	// Print current hosts file
	content, _ := os.ReadFile(hostsPath)
	t.Logf("Hosts file after container creation:\n%s", string(content))

	// Remove containers (like test-cleanup.sh: docker rm -f)
	for i, id := range containerIDs {
		t.Logf("Forcefully removing container %d: %s", i+1, containerNames[i])
		_, err = cli.ContainerRemove(ctx, id, client.ContainerRemoveOptions{Force: true})
		if err != nil {
			t.Fatalf("failed to remove container %d: %v", i+1, err)
		}
	}

	// Give some time for events to be processed
	time.Sleep(2 * time.Second)

	// Print current hosts file after removal
	content, _ = os.ReadFile(hostsPath)
	t.Logf("Hosts file after container removal:\n%s", string(content))

	// Wait for entries to disappear from hosts file
	// This is where the bug manifests - entries should be removed but aren't
	for i, containerName := range containerNames {
		expectedHostname := containerName + ".bridge"
		t.Logf("Waiting for entry to disappear: %s", expectedHostname)
		err = waitForHostEntry(t, hostsPath, expectedHostname, false, 10*time.Second)
		if err != nil {
			// Read and print hosts file for debugging
			hf := hostfile.NewHostfile(hostsPath)
			hf.Read()
			content, _ := os.ReadFile(hostsPath)
			t.Logf("ERROR: Entry still present in hosts file:\n%s", string(content))
			t.Fatalf("Container %d entry was not removed: %v", i+1, err)
		}
		t.Logf("✓ Entry removed: %s", expectedHostname)
	}

	t.Log("✓ SUCCESS: All entries were removed after containers stopped")
}

// TestIntegrationMultiNetworkWithAutoRemove tests with --rm flag
// This is closer to the test-cleanup.sh scenario
func TestIntegrationMultiNetworkWithAutoRemove(t *testing.T) {
	// Skip if running short tests
	if testing.Short() {
		t.Skip("skipping integration test in short mode")
	}

	// Create temp hosts file
	tmpDir := t.TempDir()
	hostsPath := filepath.Join(tmpDir, "hosts")

	// Start dnsthing with multi-network mode
	ctx := context.Background()
	cfg := Config{
		HostsPath:             hostsPath,
		Domain:                "",
		MultiNet:              true,
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

	containerName := "dnsthing-autoremove-" + randomString(8)
	t.Logf("Creating container with auto-remove: %s", containerName)

	resp, err := cli.ContainerCreate(ctx, client.ContainerCreateOptions{
		Config: &container.Config{
			Image: "alpine:latest",
			Cmd:   []string{"sh", "-c", "sleep 2"},
		},
		HostConfig: &container.HostConfig{
			AutoRemove: true, // This is the --rm flag
		},
		Name: containerName,
	})
	if err != nil {
		t.Fatalf("failed to create container: %v", err)
	}

	// Start the container
	_, err = cli.ContainerStart(ctx, resp.ID, client.ContainerStartOptions{})
	if err != nil {
		t.Fatalf("failed to start container: %v", err)
	}

	// Wait for entry to appear
	expectedHostname := containerName + ".bridge"
	t.Logf("Waiting for entry: %s", expectedHostname)
	err = waitForHostEntry(t, hostsPath, expectedHostname, true, 10*time.Second)
	if err != nil {
		t.Fatalf("Entry did not appear: %v", err)
	}
	t.Logf("✓ Entry found: %s", expectedHostname)

	// Wait for container to auto-remove (it sleeps for 2 seconds)
	t.Log("Waiting for container to auto-remove...")
	time.Sleep(5 * time.Second)

	// Print current hosts file
	content, _ := os.ReadFile(hostsPath)
	t.Logf("Hosts file after auto-remove:\n%s", string(content))

	// Check if entry was removed
	err = waitForHostEntry(t, hostsPath, expectedHostname, false, 5*time.Second)
	if err != nil {
		content, _ := os.ReadFile(hostsPath)
		t.Logf("ERROR: Entry still present:\n%s", string(content))
		t.Fatalf("Entry was not removed after auto-remove: %v", err)
	}

	t.Log("✓ SUCCESS: Entry was removed after auto-remove")
}
