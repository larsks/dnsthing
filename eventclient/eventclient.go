package eventclient

import (
	"context"
	"fmt"
	"io"
	"log"

	"github.com/larsks/dnsthing/eventclient/write"
	"github.com/larsks/dnsthing/hostfile"
	"github.com/moby/moby/client"
)

// Event type and action constants
const (
	eventTypeNetwork   = "network"
	eventTypeContainer = "container"
	actionConnect      = "connect"
	actionDisconnect   = "disconnect"
	actionDie          = "die"
)

// DockerClient defines the Docker API methods we need.
// The real *client.Client implements this interface.
type DockerClient interface {
	ContainerInspect(ctx context.Context, containerID string, options client.ContainerInspectOptions) (client.ContainerInspectResult, error)
	ContainerList(ctx context.Context, options client.ContainerListOptions) (client.ContainerListResult, error)
	Events(ctx context.Context, options client.EventsListOptions) client.EventsResult
}

// runEventLoop processes Docker events until an error occurs or context is canceled.
// This function is designed to be retryable - errors are returned to the caller for retry logic.
func runEventLoop(ectx *EventContext) error {
	// Get event stream - listen to both network and container events
	filters := client.Filters{}
	filters.Add("type", eventTypeNetwork)
	filters.Add("type", eventTypeContainer)
	result := ectx.Client.Events(ectx.Ctx, client.EventsListOptions{Filters: filters})

	log.Printf("monitoring Docker events, managing hosts file: %s", ectx.Config.HostsPath)

	// Event loop
	for {
		select {
		case msg := <-result.Messages:
			var err error

			// Handle different event types
			switch msg.Type {
			case eventTypeNetwork:
				// Network events: connect/disconnect
				networkName := msg.Actor.Attributes["name"]
				containerID := msg.Actor.Attributes["container"]

				switch msg.Action {
				case actionConnect:
					err = HandleNetworkConnect(ectx, containerID, networkName)
					if err != nil {
						log.Printf("ERROR: handling network connect: %v (will resync on reconnection)", err)
						// Continue processing events - don't kill daemon for single container failure
					}

				case actionDisconnect:
					err = HandleNetworkDisconnect(ectx, containerID, networkName)
					if err != nil {
						log.Printf("ERROR: handling network disconnect: %v", err)
						// Continue processing events - don't kill daemon for single container failure
					}
				}

			case eventTypeContainer:
				// Container events: die (for bulk cleanup)
				containerName := msg.Actor.Attributes["name"]

				switch msg.Action {
				case actionDie:
					err = HandleContainerDie(ectx, containerName)
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

		case <-ectx.Ctx.Done():
			// Clean shutdown requested
			return ectx.Ctx.Err()
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
	wm := write.NewManager(ctx, hf, cfg.UpdateCommand, cfg.MinimumUpdateInterval)

	// Create event context
	ectx := &EventContext{
		Ctx:          ctx,
		Client:       cli,
		Hostfile:     hf,
		WriteManager: wm,
		Config:       cfg,
	}

	// Ensure pending writes are flushed on exit
	defer func() {
		if err := wm.Flush(); err != nil {
			log.Printf("ERROR: failed to flush pending writes: %v", err)
		}
	}()

	// Initial sync with retry
	retryCfg := DefaultRetryConfig()
	err := RetryWithBackoff(ctx, retryCfg, "initial sync", func() error {
		return SyncRunningContainers(ectx)
	})
	if err != nil {
		return fmt.Errorf("failed to sync running containers: %w", err)
	}

	// Infinite retry loop for event stream
	return RetryWithBackoff(ctx, retryCfg, "event stream", func() error {
		// Resync on each reconnection to catch containers started/stopped during downtime
		if err := SyncRunningContainers(ectx); err != nil {
			return fmt.Errorf("failed to resync running containers: %w", err)
		}

		// Run event loop until error
		return runEventLoop(ectx)
	})
}
