package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/larsks/dnsthing/hostfile"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"github.com/spf13/pflag"
)

// DockerClient defines the Docker API methods we need.
// The real *client.Client implements this interface.
type DockerClient interface {
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
	Events(ctx context.Context, options client.EventsListOptions) client.EventsResult
}

// Config holds configuration for the daemon
type Config struct {
	HostsPath string
	Domain    string
	MultiNet  bool
}

// formatEvent formats an event message into a simple one-line string
func formatEvent(msg events.Message) string {
	// Format timestamp in RFC3339
	timestamp := time.Unix(msg.Time, 0).Format(time.RFC3339)

	// Get name from attributes if available
	name := msg.Actor.Attributes["name"]
	if name == "" {
		name = "unknown"
	}

	// Shorten ID to first 12 characters
	id := msg.Actor.ID
	if len(id) > 12 {
		id = id[:12]
	}

	// Format: [timestamp] type/action name=<name> id=<id>
	return fmt.Sprintf("[%s] %s/%s name=%s id=%s",
		timestamp, msg.Type, msg.Action, name, id)
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
func syncRunningContainers(ctx context.Context, cli DockerClient, hf *hostfile.Hostfile, domain string, multiNet bool) error {
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
	if err := hf.Write(); err != nil {
		return fmt.Errorf("failed to write hostfile: %w", err)
	}

	return nil
}

// handleContainerStart handles container start events
func handleContainerStart(
	ctx context.Context,
	cli DockerClient,
	hf *hostfile.Hostfile,
	containerID string,
	containerName string,
	domain string,
	multiNet bool,
) error {
	// Get container IP addresses
	ips, err := getContainerIPs(ctx, cli, containerID)
	if err != nil {
		return fmt.Errorf("failed to get IPs for container %s: %w", containerName, err)
	}

	if len(ips) == 0 {
		log.Printf("WARNING: container %s has no IP addresses, skipping", containerName)
		return nil // Not an error, just skip
	}

	// Construct hostnames
	hostnames := constructHostnames(containerName, domain, ips, multiNet)

	// Add hosts to file
	for hostname, ip := range hostnames {
		if err := hf.AddHost(hostname, ip); err != nil {
			return fmt.Errorf("failed to add host %s -> %s: %w", hostname, ip, err)
		}
	}

	// Write to disk
	if err := hf.Write(); err != nil {
		return fmt.Errorf("failed to write hostfile: %w", err)
	}

	log.Printf("added host entries for container %s: %v", containerName, hostnames)
	return nil
}

// handleContainerDie handles container die events
func handleContainerDie(
	ctx context.Context,
	cli DockerClient,
	hf *hostfile.Hostfile,
	containerID string,
	containerName string,
	domain string,
	multiNet bool,
) error {
	// Get container IP addresses to determine network names for multiNet mode
	ips, err := getContainerIPs(ctx, cli, containerID)
	if err != nil {
		// Container may already be removed, use best effort
		if multiNet {
			log.Printf("WARNING: cannot determine networks for container %s (may already be removed), skipping removal", containerName)
			return nil // Not a fatal error
		}
		// For single network mode, we can still construct the hostname
		ips = make(map[string]string)
	}

	// Construct hostnames
	hostnames := constructHostnames(containerName, domain, ips, multiNet)

	// For single-network mode with no IPs, construct the basic hostname
	if len(hostnames) == 0 && !multiNet {
		hostname := containerName
		if domain != "" {
			hostname = hostname + "." + domain
		}
		hostnames[hostname] = "" // IP doesn't matter for removal
	}

	// Remove hosts from file
	for hostname := range hostnames {
		if err := hf.RemoveHost(hostname); err != nil {
			// Host not found is OK
			log.Printf("NOTE: could not remove host %s: %v", hostname, err)
		}
	}

	// Write to disk
	if err := hf.Write(); err != nil {
		return fmt.Errorf("failed to write hostfile: %w", err)
	}

	hostnamesList := make([]string, 0, len(hostnames))
	for h := range hostnames {
		hostnamesList = append(hostnamesList, h)
	}
	log.Printf("removed host entries for container %s: %v", containerName, hostnamesList)
	return nil
}

// run orchestrates the event loop and container syncing
func run(
	ctx context.Context,
	cli DockerClient,
	hf *hostfile.Hostfile,
	cfg Config,
) error {
	// Sync all currently running containers
	if err := syncRunningContainers(ctx, cli, hf, cfg.Domain, cfg.MultiNet); err != nil {
		return fmt.Errorf("failed to sync running containers: %w", err)
	}

	// Get event stream
	filters := client.Filters{}
	filters.Add("type", "container")
	result := cli.Events(ctx, client.EventsListOptions{Filters: filters})

	log.Printf("monitoring Docker events, managing hosts file: %s", cfg.HostsPath)

	// Event loop
	for {
		select {
		case msg := <-result.Messages:
			name := msg.Actor.Attributes["name"]
			var err error

			switch msg.Action {
			case "start":
				err = handleContainerStart(ctx, cli, hf, msg.Actor.ID, name, cfg.Domain, cfg.MultiNet)
				if err != nil {
					log.Fatalf("ERROR: %v", err)
				}

			case "die":
				err = handleContainerDie(ctx, cli, hf, msg.Actor.ID, name, cfg.Domain, cfg.MultiNet)
				if err != nil {
					log.Fatalf("ERROR: %v", err)
				}
			}

		case err := <-result.Err:
			if err == io.EOF {
				return fmt.Errorf("event stream closed")
			}
			return fmt.Errorf("error receiving event: %w", err)

		case <-ctx.Done():
			return nil // Clean shutdown
		}
	}
}

func main() {
	// Parse command line arguments
	domain := pflag.StringP("domain", "d", "", "domain to append to container names")
	multiNet := pflag.BoolP("multiple-networks", "m", false, "create entries for all networks")
	pflag.Parse()

	if len(pflag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <hostfile-path>\n", os.Args[0])
		pflag.PrintDefaults()
		os.Exit(1)
	}

	cfg := Config{
		HostsPath: pflag.Args()[0],
		Domain:    *domain,
		MultiNet:  *multiNet,
	}

	// Create context with signal handling
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Create Docker client
	cli, err := client.New(client.FromEnv)
	if err != nil {
		log.Fatalf("error creating Docker client: %v", err)
	}
	defer cli.Close()

	// Initialize hostfile
	hf := hostfile.NewHostfile(cfg.HostsPath)
	if err := hf.Read(); err != nil {
		log.Printf("WARNING: could not read hostfile: %v (starting with empty state)", err)
	}

	// Run the daemon
	if err := run(ctx, cli, hf, cfg); err != nil {
		if err.Error() == "event stream closed" {
			fmt.Fprintln(os.Stderr, "Event stream closed.")
			return
		}
		log.Fatalf("error: %v", err)
	}

	fmt.Fprintln(os.Stderr, "\nShutting down...")
}
