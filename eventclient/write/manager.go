package write

import (
	"context"
	"log"
	"sync"
	"time"

	"github.com/larsks/dnsthing/hostfile"
)

// Manager handles throttled writes to the hostfile and update command execution.
type Manager struct {
	hf                    *hostfile.Hostfile
	commandExecutor       CommandExecutor
	updateCommand         string
	minimumUpdateInterval time.Duration
	mu                    sync.Mutex
	lastWrite             time.Time
	pendingWrite          bool
	timer                 *time.Timer
	ctx                   context.Context
}

// NewManager creates a new write manager.
func NewManager(ctx context.Context, hf *hostfile.Hostfile, updateCommand string, minInterval time.Duration) *Manager {
	return &Manager{
		hf:                    hf,
		commandExecutor:       &ShellCommandExecutor{},
		updateCommand:         updateCommand,
		minimumUpdateInterval: minInterval,
		ctx:                   ctx,
	}
}

// RequestWrite requests a write to the hostfile, respecting the minimum update interval.
func (m *Manager) RequestWrite() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If no minimum interval, write immediately
	if m.minimumUpdateInterval == 0 {
		return m.writeNow()
	}

	// Check if enough time has passed since last write
	timeSinceLastWrite := time.Since(m.lastWrite)
	if timeSinceLastWrite >= m.minimumUpdateInterval {
		// Enough time has passed, write immediately
		return m.writeNow()
	}

	// Not enough time has passed, schedule a write
	m.pendingWrite = true

	// Cancel existing timer if any
	if m.timer != nil {
		m.timer.Stop()
	}

	// Calculate when the next write should happen
	timeUntilNextWrite := m.minimumUpdateInterval - timeSinceLastWrite

	// Schedule the write
	m.timer = time.AfterFunc(timeUntilNextWrite, func() {
		m.mu.Lock()
		defer m.mu.Unlock()

		if m.pendingWrite {
			if err := m.writeNow(); err != nil {
				log.Printf("ERROR: scheduled write failed: %v", err)
			}
		}
	})

	log.Printf("write scheduled in %v", timeUntilNextWrite)
	return nil
}

// Flush ensures any pending writes are completed.
func (m *Manager) Flush() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Cancel any pending timer
	if m.timer != nil {
		m.timer.Stop()
		m.timer = nil
	}

	// If there's a pending write, do it now
	if m.pendingWrite {
		return m.writeNow()
	}

	return nil
}

// writeNow performs the actual write (must be called with mutex held).
func (m *Manager) writeNow() error {
	if err := m.hf.Write(); err != nil {
		return err
	}

	m.lastWrite = time.Now()
	m.pendingWrite = false

	// Execute update command in background
	go m.commandExecutor.Execute(m.ctx, m.updateCommand) //nolint:errcheck

	return nil
}
