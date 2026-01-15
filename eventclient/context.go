package eventclient

import (
	"context"

	"github.com/larsks/dnsthing/hostfile"
)

// EventContext encapsulates all dependencies needed by event handlers.
// This eliminates parameter bloat by bundling related dependencies.
type EventContext struct {
	Ctx          context.Context
	Client       DockerClient
	Hostfile     *hostfile.Hostfile
	WriteManager WriteManager
	Config       Config
}

// WriteManager interface abstracts write operations.
// This enables easier testing via interface injection.
type WriteManager interface {
	RequestWrite() error
	Flush() error
}
