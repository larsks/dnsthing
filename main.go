package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/larsks/dnsthing/eventclient"
	"github.com/larsks/dnsthing/hostfile"
	"github.com/moby/moby/api/types/events"
	"github.com/moby/moby/client"
	"github.com/spf13/pflag"
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
	// Parse command line arguments
	domain := pflag.StringP("domain", "d", "", "domain to append to container names")
	multiNet := pflag.BoolP("multiple-networks", "m", false, "create entries for all networks")
	pflag.Parse()

	if len(pflag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <hostfile-path>\n", os.Args[0])
		pflag.PrintDefaults()
		os.Exit(1)
	}

	cfg := eventclient.Config{
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
	if err := eventclient.Run(ctx, cli, hf, cfg); err != nil {
		if err.Error() == "event stream closed" {
			fmt.Fprintln(os.Stderr, "Event stream closed.")
			return
		}
		log.Fatalf("error: %v", err)
	}

	fmt.Fprintln(os.Stderr, "\nShutting down...")
}
