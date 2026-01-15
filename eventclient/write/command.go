package write

import (
	"context"
	"log"
	"os/exec"
)

// CommandExecutor executes shell commands.
type CommandExecutor interface {
	Execute(ctx context.Context, command string) error
}

// ShellCommandExecutor executes commands via /bin/sh.
type ShellCommandExecutor struct{}

// Execute runs the command using /bin/sh.
func (e *ShellCommandExecutor) Execute(ctx context.Context, command string) error {
	if command == "" {
		return nil
	}

	cmd := exec.CommandContext(ctx, "/bin/sh", "-c", command)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Printf("ERROR: update command failed: %v", err)
		if len(output) > 0 {
			log.Printf("command output: %s", string(output))
		}
		return err
	}

	log.Printf("update command executed successfully")
	if len(output) > 0 {
		log.Printf("command output: %s", string(output))
	}
	return nil
}
