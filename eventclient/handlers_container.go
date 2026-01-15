package eventclient

import "log"

// HandleContainerDie removes all host entries for a dying container.
func HandleContainerDie(ectx *EventContext, containerName string) error {
	// Use pattern-based removal to remove all entries for this container
	// This works even when the container is already removed and we can't inspect it
	// It correctly handles both single-network mode (containerName) and multi-network mode
	// (containerName.networkName) by matching the first dot-delimited component
	removed := ectx.Hostfile.RemoveHostsWithName(containerName)

	// Write to disk if anything was removed
	if len(removed) > 0 {
		if err := ectx.WriteManager.RequestWrite(); err != nil {
			return err
		}
		log.Printf("removed host entries for container %s: %v", containerName, removed)
	}

	return nil
}
