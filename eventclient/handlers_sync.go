package eventclient

import (
	"fmt"
	"log"

	"github.com/larsks/dnsthing/eventclient/containers"
	"github.com/moby/moby/client"
)

// SyncRunningContainers adds host entries for all currently running containers.
func SyncRunningContainers(ectx *EventContext) error {
	result, err := ectx.Client.ContainerList(ectx.Ctx, client.ContainerListOptions{})
	if err != nil {
		return fmt.Errorf("failed to list containers: %w", err)
	}

	log.Printf("syncing %d running container(s)", len(result.Items))

	for _, container := range result.Items {
		// Get container name (remove leading slash if present)
		name := containers.TrimContainerName(container.Names[0])

		// Get container IP addresses
		ips, err := containers.GetContainerIPs(ectx.Ctx, ectx.Client, container.ID)
		if err != nil {
			log.Printf("ERROR: failed to get IPs for container %s: %v", name, err)
			continue
		}

		if len(ips) == 0 {
			log.Printf("WARNING: container %s has no IP addresses, skipping", name)
			continue
		}

		// Construct hostnames
		hostnames := containers.ConstructHostnames(name, ectx.Config.Domain, ips, ectx.Config.MultiNet)

		// Add hosts to file
		for hostname, ip := range hostnames {
			if err := ectx.Hostfile.AddHost(hostname, ip); err != nil {
				log.Printf("ERROR: failed to add host %s -> %s: %v", hostname, ip, err)
			}
		}

		log.Printf("added host entries for container %s: %v", name, hostnames)
	}

	// Write all changes to disk
	return ectx.WriteManager.RequestWrite()
}
