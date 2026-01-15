package eventclient

import (
	"context"
	"fmt"
	"io"
	"log"
	"os/exec"
	"sync"
	"time"

	"github.com/larsks/dnsthing/hostfile"
	"github.com/moby/moby/client"
)

// DockerClient defines the Docker API methods we need.
// The real *client.Client implements this interface.
type DockerClient interface {
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
	Events(ctx context.Context, options client.EventsListOptions) client.EventsResult
}

// Config holds configuration for the event client
type Config struct {
	HostsPath             string
	Domain                string
	MultiNet              bool
	UpdateCommand         string
	MinimumUpdateInterval time.Duration
}

// writeManager handles throttled writes to the hostfile and update command execution
type writeManager struct {
	hf                    *hostfile.Hostfile
	updateCommand         string
	minimumUpdateInterval time.Duration
	mu                    sync.Mutex
	lastWrite             time.Time
	pendingWrite          bool
	timer                 *time.Timer
	ctx                   context.Context
}

// newWriteManager creates a new write manager
func newWriteManager(ctx context.Context, hf *hostfile.Hostfile, updateCommand string, minInterval time.Duration) *writeManager {
	return &writeManager{
		hf:                    hf,
		updateCommand:         updateCommand,
		minimumUpdateInterval: minInterval,
		ctx:                   ctx,
	}
}

// executeUpdateCommand runs the update command using /bin/sh
func (wm *writeManager) executeUpdateCommand() {
	if wm.updateCommand == "" {
		return
	}

	cmd := exec.CommandContext(wm.ctx, "/bin/sh", "-c", wm.updateCommand)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("ERROR: update command failed: %v", err)
		if len(output) > 0 {
			log.Printf("command output: %s", string(output))
		}
	} else {
		log.Printf("update command executed successfully")
		if len(output) > 0 {
			log.Printf("command output: %s", string(output))
		}
	}
}

// requestWrite requests a write to the hostfile, respecting the minimum update interval
func (wm *writeManager) requestWrite() error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	// If no minimum interval, write immediately
	if wm.minimumUpdateInterval == 0 {
		return wm.writeNow()
	}

	// Check if enough time has passed since last write
	timeSinceLastWrite := time.Since(wm.lastWrite)
	if timeSinceLastWrite >= wm.minimumUpdateInterval {
		// Enough time has passed, write immediately
		return wm.writeNow()
	}

	// Not enough time has passed, schedule a write
	wm.pendingWrite = true

	// Cancel existing timer if any
	if wm.timer != nil {
		wm.timer.Stop()
	}

	// Calculate when the next write should happen
	timeUntilNextWrite := wm.minimumUpdateInterval - timeSinceLastWrite

	// Schedule the write
	wm.timer = time.AfterFunc(timeUntilNextWrite, func() {
		wm.mu.Lock()
		defer wm.mu.Unlock()

		if wm.pendingWrite {
			if err := wm.writeNow(); err != nil {
				log.Printf("ERROR: scheduled write failed: %v", err)
			}
		}
	})

	log.Printf("write scheduled in %v", timeUntilNextWrite)
	return nil
}

// writeNow performs the actual write (must be called with mutex held)
func (wm *writeManager) writeNow() error {
	if err := wm.hf.Write(); err != nil {
		return err
	}

	wm.lastWrite = time.Now()
	wm.pendingWrite = false

	// Execute update command in background
	go wm.executeUpdateCommand()

	return nil
}

// flush ensures any pending writes are completed
func (wm *writeManager) flush() error {
	wm.mu.Lock()
	defer wm.mu.Unlock()

	// Cancel any pending timer
	if wm.timer != nil {
		wm.timer.Stop()
		wm.timer = nil
	}

	// If there's a pending write, do it now
	if wm.pendingWrite {
		return wm.writeNow()
	}

	return nil
}

// getContainerIPs retrieves all IP addresses for a container, organized by network name
func getContainerIPs(ctx context.Context, cli DockerClient, containerID string) (map[string]string, error) {
	inspect, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return nil, fmt.Errorf("failed to inspect container: %w", err)
	}

	ips := make(map[string]string)
	if inspect.Container.NetworkSettings != nil && inspect.Container.NetworkSettings.Networks != nil {
		for networkName, network := range inspect.Container.NetworkSettings.Networks {
			// Check IPv4 first
			if network.IPAddress.IsValid() {
				ips[networkName] = network.IPAddress.String()
			} else if network.GlobalIPv6Address.IsValid() {
				// Fallback to IPv6 if no IPv4
				ips[networkName] = network.GlobalIPv6Address.String()
			}
		}
	}

	return ips, nil
}

// getContainerIPForNetwork retrieves the container name and IP address for a specific network
func getContainerIPForNetwork(ctx context.Context, cli DockerClient, containerID string, networkName string) (containerName string, ip string, err error) {
	inspect, err := cli.ContainerInspect(ctx, containerID, client.ContainerInspectOptions{})
	if err != nil {
		return "", "", fmt.Errorf("failed to inspect container: %w", err)
	}

	// Extract container name (remove leading slash if present)
	name := inspect.Container.Name
	if len(name) > 0 && name[0] == '/' {
		name = name[1:]
	}

	// Look up the specific network
	if inspect.Container.NetworkSettings == nil || inspect.Container.NetworkSettings.Networks == nil {
		return "", "", fmt.Errorf("container has no network settings")
	}

	network, exists := inspect.Container.NetworkSettings.Networks[networkName]
	if !exists {
		return "", "", fmt.Errorf("container not connected to network %s", networkName)
	}

	// Get IPv4 or IPv6 address
	var ipAddr string
	if network.IPAddress.IsValid() {
		ipAddr = network.IPAddress.String()
	} else if network.GlobalIPv6Address.IsValid() {
		ipAddr = network.GlobalIPv6Address.String()
	} else {
		return "", "", fmt.Errorf("no IP address assigned for network %s", networkName)
	}

	return name, ipAddr, nil
}

// constructHostnames creates a map of hostnames to IP addresses based on container name,
// domain, network IPs, and multi-network mode
func constructHostnames(containerName, domain string, ips map[string]string, multiNet bool) map[string]string {
	hostnames := make(map[string]string)

	if len(ips) == 0 {
		return hostnames
	}

	if multiNet {
		// Multi-network mode: create entry for each network
		// Format: containerName.networkName[.domain]
		for networkName, ip := range ips {
			hostname := containerName + "." + networkName
			if domain != "" {
				hostname = hostname + "." + domain
			}
			hostnames[hostname] = ip
		}
	} else {
		// Single network mode: use first available IP
		// Format: containerName[.domain]
		hostname := containerName
		if domain != "" {
			hostname = hostname + "." + domain
		}
		// Get first IP from map
		for _, ip := range ips {
			hostnames[hostname] = ip
			break
		}
	}

	return hostnames
}

// syncRunningContainers adds host entries for all currently running containers
func syncRunningContainers(ctx context.Context, cli DockerClient, hf *hostfile.Hostfile, wm *writeManager, domain string, multiNet bool) error {
	result, err := cli.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	log.Printf("syncing %d running container(s)", len(result.Items))

	for _, container := range result.Items {
		// Get container name (remove leading slash if present)
		name := container.Names[0]
		if len(name) > 0 && name[0] == '/' {
			name = name[1:]
		}

		// Get container IP addresses
		ips, err := getContainerIPs(ctx, cli, container.ID)
		if err != nil {
			log.Printf("ERROR: failed to get IPs for container %s: %v", name, err)
			continue
		}

		if len(ips) == 0 {
			log.Printf("WARNING: container %s has no IP addresses, skipping", name)
			continue
		}

		// Construct hostnames
		hostnames := constructHostnames(name, domain, ips, multiNet)

		// Add hosts to file
		for hostname, ip := range hostnames {
			if err := hf.AddHost(hostname, ip); err != nil {
				log.Printf("ERROR: failed to add host %s -> %s: %v", hostname, ip, err)
			}
		}

		log.Printf("added host entries for container %s: %v", name, hostnames)
	}

	// Write all changes to disk
	if err := wm.requestWrite(); err != nil {
		return fmt.Errorf("failed to write hostfile: %w", err)
	}

	return nil
}

// handleNetworkConnect handles network connect events
func handleNetworkConnect(
	ctx context.Context,
	cli DockerClient,
	hf *hostfile.Hostfile,
	wm *writeManager,
	containerID string,
	networkName string,
	domain string,
	multiNet bool,
) error {
	// Get container name and IP for this specific network
	containerName, ip, err := getContainerIPForNetwork(ctx, cli, containerID, networkName)
	if err != nil {
		return fmt.Errorf("failed to get container IP for network %s: %w", networkName, err)
	}

	if multiNet {
		// Multi-network mode: create entry for this specific network
		// Format: containerName.networkName[.domain]
		hostname := containerName + "." + networkName
		if domain != "" {
			hostname = hostname + "." + domain
		}

		if err := hf.AddHost(hostname, ip); err != nil {
			return fmt.Errorf("failed to add host %s -> %s: %w", hostname, ip, err)
		}

		log.Printf("added host entry for container %s on network %s: %s -> %s", containerName, networkName, hostname, ip)
	} else {
		// Single network mode: only add if container doesn't already have an entry
		// Format: containerName[.domain]
		hostname := containerName
		if domain != "" {
			hostname = hostname + "." + domain
		}

		// Check if entry already exists
		if _, err := hf.LookupHost(hostname); err == nil {
			// Entry already exists, skip
			log.Printf("container %s already has entry, skipping network %s", containerName, networkName)
			return nil
		}

		// Entry doesn't exist, add it
		if err := hf.AddHost(hostname, ip); err != nil {
			return fmt.Errorf("failed to add host %s -> %s: %w", hostname, ip, err)
		}

		log.Printf("added host entry for container %s: %s -> %s", containerName, hostname, ip)
	}

	// Write to disk
	if err := wm.requestWrite(); err != nil {
		return fmt.Errorf("failed to write hostfile: %w", err)
	}

	return nil
}

// handleNetworkDisconnect handles network disconnect events
func handleNetworkDisconnect(
	ctx context.Context,
	cli DockerClient,
	hf *hostfile.Hostfile,
	wm *writeManager,
	containerID string,
	networkName string,
	domain string,
	multiNet bool,
) error {
	// Try to get container name (may fail if container is already removed)
	containerName, _, err := getContainerIPForNetwork(ctx, cli, containerID, networkName)
	if err != nil {
		// Container may already be removed, log warning and skip
		log.Printf("WARNING: cannot get container info for network disconnect (may already be removed): %v", err)
		return nil
	}

	// Construct hostname based on mode
	var hostname string
	if multiNet {
		// Multi-network mode: remove network-specific entry
		// Format: containerName.networkName[.domain]
		hostname = containerName + "." + networkName
		if domain != "" {
			hostname = hostname + "." + domain
		}
	} else {
		// Single network mode: remove base hostname
		// Format: containerName[.domain]
		hostname = containerName
		if domain != "" {
			hostname = hostname + "." + domain
		}
	}

	// Remove host from file
	if err := hf.RemoveHost(hostname); err != nil {
		// Host not found is OK (may have been manually removed)
		log.Printf("NOTE: could not remove host %s: %v", hostname, err)
	} else {
		log.Printf("removed host entry for container %s: %s", containerName, hostname)
	}

	// Write to disk
	if err := wm.requestWrite(); err != nil {
		return fmt.Errorf("failed to write hostfile: %w", err)
	}

	return nil
}

// handleContainerDie handles container die events by removing all entries for the container
func handleContainerDie(
	ctx context.Context,
	cli DockerClient,
	hf *hostfile.Hostfile,
	wm *writeManager,
	containerID string,
	containerName string,
	domain string,
	multiNet bool,
) error {
	// Use pattern-based removal to remove all entries for this container
	// This works even when the container is already removed and we can't inspect it
	// It correctly handles both single-network mode (containerName) and multi-network mode
	// (containerName.networkName) by matching the first dot-delimited component
	removed := hf.RemoveHostsWithName(containerName)

	// Write to disk if anything was removed
	if len(removed) > 0 {
		if err := wm.requestWrite(); err != nil {
			return fmt.Errorf("failed to write hostfile: %w", err)
		}
		log.Printf("removed host entries for container %s: %v", containerName, removed)
	}

	return nil
}

// runEventLoop processes Docker events until an error occurs or context is canceled.
// This function is designed to be retryable - errors are returned to the caller for retry logic.
func runEventLoop(
	ctx context.Context,
	cli DockerClient,
	hf *hostfile.Hostfile,
	wm *writeManager,
	cfg Config,
) error {
	// Get event stream - listen to both network and container events
	filters := client.Filters{}
	filters.Add("type", "network")
	filters.Add("type", "container")
	result := cli.Events(ctx, client.EventsListOptions{Filters: filters})

	log.Printf("monitoring Docker events, managing hosts file: %s", cfg.HostsPath)

	// Event loop
	for {
		select {
		case msg := <-result.Messages:
			var err error

			// Handle different event types
			switch msg.Type {
			case "network":
				// Network events: connect/disconnect
				networkName := msg.Actor.Attributes["name"]
				containerID := msg.Actor.Attributes["container"]

				switch msg.Action {
				case "connect":
					err = handleNetworkConnect(ctx, cli, hf, wm, containerID, networkName, cfg.Domain, cfg.MultiNet)
					if err != nil {
						log.Printf("ERROR: handling network connect: %v (will resync on reconnection)", err)
						// Continue processing events - don't kill daemon for single container failure
					}

				case "disconnect":
					err = handleNetworkDisconnect(ctx, cli, hf, wm, containerID, networkName, cfg.Domain, cfg.MultiNet)
					if err != nil {
						log.Printf("ERROR: handling network disconnect: %v", err)
						// Continue processing events - don't kill daemon for single container failure
					}
				}

			case "container":
				// Container events: die (for bulk cleanup)
				containerName := msg.Actor.Attributes["name"]
				containerID := msg.Actor.ID

				switch msg.Action {
				case "die":
					err = handleContainerDie(ctx, cli, hf, wm, containerID, containerName, cfg.Domain, cfg.MultiNet)
					if err != nil {
						log.Printf("ERROR: handling container die: %v", err)
						// Continue processing events - don't kill daemon for single container failure
					}
				}
			}

		case err := <-result.Err:
			// Event stream error - will be retried by outer loop
			if err == io.EOF {
				return fmt.Errorf("event stream closed")
			}
			return fmt.Errorf("error receiving event: %w", err)

		case <-ctx.Done():
			// Clean shutdown requested
			return ctx.Err()
		}
	}
}

// Run orchestrates the event loop and container syncing with automatic retry on connection failures.
func Run(
	ctx context.Context,
	cli DockerClient,
	hf *hostfile.Hostfile,
	cfg Config,
) error {
	// Create write manager
	wm := newWriteManager(ctx, hf, cfg.UpdateCommand, cfg.MinimumUpdateInterval)

	// Ensure pending writes are flushed on exit
	defer func() {
		if err := wm.flush(); err != nil {
			log.Printf("ERROR: failed to flush pending writes: %v", err)
		}
	}()

	// Initial sync with retry
	retryCfg := DefaultRetryConfig()
	err := RetryWithBackoff(ctx, retryCfg, "initial sync", func() error {
		return syncRunningContainers(ctx, cli, hf, wm, cfg.Domain, cfg.MultiNet)
	})
	if err != nil {
		return fmt.Errorf("failed to sync running containers: %w", err)
	}

	// Infinite retry loop for event stream
	return RetryWithBackoff(ctx, retryCfg, "event stream", func() error {
		// Resync on each reconnection to catch containers started/stopped during downtime
		if err := syncRunningContainers(ctx, cli, hf, wm, cfg.Domain, cfg.MultiNet); err != nil {
			return fmt.Errorf("failed to resync running containers: %w", err)
		}

		// Run event loop until error
		return runEventLoop(ctx, cli, hf, wm, cfg)
	})
}
