package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/larsks/dnsthing/hostfile"
	"github.com/moby/moby/client"
	"github.com/spf13/pflag"
)

// getContainerIPs retrieves all IP addresses for a container, organized by network name
func getContainerIPs(ctx context.Context, cli *client.Client, containerID string) (map[string]string, error) {
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
func syncRunningContainers(ctx context.Context, cli *client.Client, hf *hostfile.Hostfile, domain string, multiNet bool) {
	result, err := cli.ContainerList(ctx, client.ContainerListOptions{})
	if err != nil {
		log.Fatalf("ERROR: failed to list containers: %v", err)
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
		log.Fatalf("ERROR: failed to write hostfile: %v", err)
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
	hostsPath := pflag.Args()[0]

	// Create context with signal handling for graceful shutdown
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
	hf := hostfile.NewHostfile(hostsPath)
	if err := hf.Read(); err != nil {
		log.Printf("WARNING: could not read hostfile: %v (starting with empty state)", err)
	}

	// Sync all currently running containers
	syncRunningContainers(ctx, cli, hf, *domain, *multiNet)

	// Get event stream
	filters := client.Filters{}
	filters.Add("type", "container")
	result := cli.Events(ctx, client.EventsListOptions{Filters: filters})

	log.Printf("monitoring Docker events, managing hosts file: %s", hostsPath)

	// Event loop
	for {
		select {
		case msg := <-result.Messages:
			name := msg.Actor.Attributes["name"]
			switch msg.Action {
			case "start":
				// Get container IP addresses
				ips, err := getContainerIPs(ctx, cli, msg.Actor.ID)
				if err != nil {
					log.Printf("ERROR: failed to get IPs for container %s: %v", name, err)
					continue
				}

				if len(ips) == 0 {
					log.Printf("WARNING: container %s has no IP addresses, skipping", name)
					continue
				}

				// Construct hostnames
				hostnames := constructHostnames(name, *domain, ips, *multiNet)

				// Add hosts to file
				hasErrors := false
				for hostname, ip := range hostnames {
					if err := hf.AddHost(hostname, ip); err != nil {
						log.Printf("ERROR: failed to add host %s -> %s: %v", hostname, ip, err)
						hasErrors = true
					}
				}

				// Write to disk
				if !hasErrors {
					if err := hf.Write(); err != nil {
						log.Fatalf("ERROR: failed to write hostfile: %v", err)
					}
					log.Printf("added host entries for container %s: %v", name, hostnames)
				}

			case "die":
				// Get container IP addresses to determine network names for multiNet mode
				ips, err := getContainerIPs(ctx, cli, msg.Actor.ID)
				if err != nil {
					// Container may already be removed, use best effort
					if *multiNet {
						log.Printf("WARNING: cannot determine networks for container %s (may already be removed), skipping removal", name)
						continue
					}
					// For single network mode, we can still construct the hostname
					ips = make(map[string]string)
				}

				// Construct hostnames (even with empty ips map for single-network mode)
				hostnames := constructHostnames(name, *domain, ips, *multiNet)

				// For single-network mode with no IPs, construct the basic hostname
				if len(hostnames) == 0 && !*multiNet {
					hostname := name
					if *domain != "" {
						hostname = hostname + "." + *domain
					}
					hostnames[hostname] = "" // IP doesn't matter for removal
				}

				// Remove hosts from file
				hasErrors := false
				for hostname := range hostnames {
					if err := hf.RemoveHost(hostname); err != nil {
						// Host not found is OK (may not have been added due to earlier errors)
						log.Printf("NOTE: could not remove host %s: %v", hostname, err)
					}
				}

				// Write to disk
				if !hasErrors {
					if err := hf.Write(); err != nil {
						log.Fatalf("ERROR: failed to write hostfile: %v", err)
					}
					hostnamesList := make([]string, 0, len(hostnames))
					for h := range hostnames {
						hostnamesList = append(hostnamesList, h)
					}
					log.Printf("removed host entries for container %s: %v", name, hostnamesList)
				}
			}

		case err := <-result.Err:
			if err == io.EOF {
				fmt.Fprintln(os.Stderr, "Event stream closed.")
				return
			}
			log.Fatalf("error receiving event: %v", err)

		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\nShutting down...")
			return
		}
	}
}
