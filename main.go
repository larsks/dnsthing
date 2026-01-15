package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/larsks/dnsthing/eventclient"
	"github.com/larsks/dnsthing/hostfile"
	"github.com/moby/moby/client"
	"github.com/spf13/pflag"
)

func main() {
	// Parse command line arguments
	domain := pflag.StringP("domain", "d", "", "domain to append to container names")
	multiNet := pflag.BoolP("multiple-networks", "m", false, "create entries for all networks")
	updateCommand := pflag.StringP("update-command", "u", "", "command to run after hostfile updates")
	minInterval := pflag.DurationP("minimum-update-interval", "i", 0, "minimum time between hostfile updates")
	replace := pflag.BoolP("replace", "r", false, "replace hosts file (ignore existing entries)")
	pflag.Parse()

	if len(pflag.Args()) != 1 {
		fmt.Fprintf(os.Stderr, "Usage: %s [options] <hostfile-path>\n", os.Args[0])
		pflag.PrintDefaults()
		os.Exit(1)
	}

	cfg := eventclient.Config{
		HostsPath:             pflag.Args()[0],
		Domain:                *domain,
		MultiNet:              *multiNet,
		UpdateCommand:         *updateCommand,
		MinimumUpdateInterval: *minInterval,
	}

	// Create context with signal handling
	ctx, stop := signal.NotifyContext(context.Background(),
		os.Interrupt, syscall.SIGTERM)
	defer stop()

	cli, err := client.New(client.FromEnv)
	if err != nil {
		log.Fatalf("error creating Docker client: %v", err)
	}
	defer cli.Close() //nolint:errcheck

	// Initialize hostfile
	hf := hostfile.NewHostfile(cfg.HostsPath)
	if !*replace {
		if err := hf.Read(); err != nil {
			log.Printf("WARNING: could not read hostfile: %v (starting with empty state)", err)
		}
	} else {
		log.Printf("replace mode enabled, ignoring existing hostfile entries")
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
