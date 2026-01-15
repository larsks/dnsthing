package eventclient

import "time"

// Config holds configuration for the event client
type Config struct {
	HostsPath             string
	Domain                string
	MultiNet              bool
	UpdateCommand         string
	MinimumUpdateInterval time.Duration
}
