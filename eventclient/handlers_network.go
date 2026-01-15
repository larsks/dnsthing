package eventclient

import (
	"fmt"
	"log"

	"github.com/larsks/dnsthing/eventclient/containers"
)

// HandleNetworkConnect processes network connection events.
func HandleNetworkConnect(ectx *EventContext, containerID, networkName string) error {
	// Get container name and IP for this specific network
	containerName, ip, err := containers.GetContainerIPForNetwork(ectx.Ctx, ectx.Client, containerID, networkName)
	if err != nil {
		return fmt.Errorf("failed to get container IP for network %s: %w", networkName, err)
	}

	if ectx.Config.MultiNet {
		// Multi-network mode: create entry for this specific network
		// Format: containerName.networkName[.domain]
		hostname := containers.BuildMultiNetworkHostname(containerName, networkName, ectx.Config.Domain)

		if err := ectx.Hostfile.AddHost(hostname, ip); err != nil {
			return fmt.Errorf("failed to add host %s -> %s: %w", hostname, ip, err)
		}

		log.Printf("added host entry for container %s on network %s: %s -> %s", containerName, networkName, hostname, ip)
	} else {
		// Single network mode: only add if container doesn't already have an entry
		// Format: containerName[.domain]
		hostname := containers.BuildSingleNetworkHostname(containerName, ectx.Config.Domain)

		// Check if entry already exists
		if _, err := ectx.Hostfile.LookupHost(hostname); err == nil {
			// Entry already exists, skip
			log.Printf("container %s already has entry, skipping network %s", containerName, networkName)
			return nil
		}

		// Entry doesn't exist, add it
		if err := ectx.Hostfile.AddHost(hostname, ip); err != nil {
			return fmt.Errorf("failed to add host %s -> %s: %w", hostname, ip, err)
		}

		log.Printf("added host entry for container %s: %s -> %s", containerName, hostname, ip)
	}

	// Write to disk
	return ectx.WriteManager.RequestWrite()
}

// HandleNetworkDisconnect processes network disconnection events.
func HandleNetworkDisconnect(ectx *EventContext, containerID, networkName string) error {
	// Get container name (works even if network is already disconnected)
	containerName, err := containers.GetContainerName(ectx.Ctx, ectx.Client, containerID)
	if err != nil {
		// Container may be completely removed, skip silently
		// The container die handler will clean up entries
		return nil
	}

	// Construct hostname based on mode
	var hostname string
	if ectx.Config.MultiNet {
		// Multi-network mode: remove network-specific entry
		// Format: containerName.networkName[.domain]
		hostname = containers.BuildMultiNetworkHostname(containerName, networkName, ectx.Config.Domain)
	} else {
		// Single network mode: remove base hostname
		// Format: containerName[.domain]
		hostname = containers.BuildSingleNetworkHostname(containerName, ectx.Config.Domain)
	}

	// Remove host from file
	if err := ectx.Hostfile.RemoveHost(hostname); err != nil {
		// Host not found is OK (may have been manually removed)
		log.Printf("NOTE: could not remove host %s: %v", hostname, err)
	} else {
		log.Printf("removed host entry for container %s: %s", containerName, hostname)
	}

	// Write to disk
	return ectx.WriteManager.RequestWrite()
}
