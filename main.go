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

	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
)

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

func main() {
	// Create context with signal handling for graceful shutdown
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Create Docker client
	cli, err := client.New(client.FromEnv)
	if err != nil {
		log.Fatalf("Error creating Docker client: %v", err)
	}
	defer cli.Close()

	// Get event stream
	filters := client.Filters{}
	filters.Add("type", "container")
	result := cli.Events(ctx, client.EventsListOptions{Filters: filters})

	// Event loop
	for {
		select {
		case msg := <-result.Messages:
			name := msg.Actor.Attributes["name"]
			switch msg.Action {
			case "create":
				fmt.Printf("CREATE %s\n", name)
			case "die":
				fmt.Printf("DIE %s\n", name)
			}

		case err := <-result.Err:
			if err == io.EOF {
				fmt.Fprintln(os.Stderr, "Event stream closed.")
				return
			}
			log.Fatalf("Error receiving event: %v", err)

		case <-ctx.Done():
			fmt.Fprintln(os.Stderr, "\nShutting down...")
			return
		}
	}
}
