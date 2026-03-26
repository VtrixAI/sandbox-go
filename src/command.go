package sandbox

import (
	"context"
	"strings"
	"sync"
	"time"
)

// LogEvent is a single line of output from a running command.
// Stream is "stdout" or "stderr".
type LogEvent struct {
	Stream string
	Data   string
}

// Command represents a background command that may still be running.
// Obtain via Sandbox.ExecuteDetached or Sandbox.GetCommand.
type Command struct {
	CmdID      string
	PID        uint32
	StartedAt  time.Time
	WorkingDir string    // working directory the command was started in (empty if not known)
	sandbox    *Sandbox
	mu         sync.Mutex
	exitCode   *int // nil while running; populated after Wait completes
}

// ExitCode returns the exit code if the command has finished, or nil if it is
// still running. It is safe to call from multiple goroutines.
func (c *Command) ExitCode() *int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.exitCode
}

// CommandFinished is the result of a completed command.
// It embeds Command for access to command metadata.
// For commands started with ExecuteDetached, CmdID is set and Wait/Logs/Kill work.
// For blocking Execute results, CmdID is empty (command already finished).
type CommandFinished struct {
	Command
	ExitCode int
	Output   string // combined stdout+stderr output
}

// Wait waits for the command to finish and returns its result.
// Use after ExecuteDetached to block until completion.
func (c *Command) Wait(ctx context.Context) (*CommandFinished, error) {
	eventCh, resultCh, errCh := c.sandbox.ExecLogs(ctx, c.CmdID)
	for range eventCh {} // drain; we use ExecResult.Output for the combined output
	if err := <-errCh; err != nil {
		return nil, err
	}
	result := <-resultCh
	finished := &CommandFinished{Command: *c}
	if result != nil {
		finished.ExitCode = result.ExitCode
		finished.Output = result.Output
		// populate the live exitCode field so ExitCode() returns non-nil after Wait
		c.mu.Lock()
		ec := result.ExitCode
		c.exitCode = &ec
		c.mu.Unlock()
	}
	return finished, nil
}

// Logs streams output events (stdout/stderr) from the command.
// logCh is closed when the command finishes. errCh receives at most one error.
func (c *Command) Logs(ctx context.Context) (<-chan LogEvent, <-chan error) {
	logCh := make(chan LogEvent, 64)
	errCh := make(chan error, 1)
	go func() {
		defer close(logCh)
		defer close(errCh)
		eventCh, _, execErrCh := c.sandbox.ExecLogs(ctx, c.CmdID)
		for ev := range eventCh {
			if ev.Type == "stdout" || ev.Type == "stderr" {
				logCh <- LogEvent{Stream: ev.Type, Data: ev.Data}
			}
		}
		if err := <-execErrCh; err != nil {
			errCh <- err
		}
	}()
	return logCh, errCh
}

// Stdout collects and returns all stdout output as a string.
func (c *Command) Stdout(ctx context.Context) (string, error) {
	return c.collectOutput(ctx, "stdout")
}

// Stderr collects and returns all stderr output as a string.
func (c *Command) Stderr(ctx context.Context) (string, error) {
	return c.collectOutput(ctx, "stderr")
}

// CollectOutput collects output from the specified stream.
// stream is one of "stdout", "stderr", or "both".
func (c *Command) CollectOutput(ctx context.Context, stream string) (string, error) {
	return c.collectOutput(ctx, stream)
}

// Kill sends a signal to the command.
// signal is one of "SIGTERM" (default), "SIGKILL", "SIGINT", "SIGHUP".
func (c *Command) Kill(ctx context.Context, signal string) error {
	return c.sandbox.Kill(ctx, c.CmdID, signal)
}

func (c *Command) collectOutput(ctx context.Context, stream string) (string, error) {
	eventCh, _, errCh := c.sandbox.ExecLogs(ctx, c.CmdID)
	var sb strings.Builder
	for ev := range eventCh {
		if stream == "both" || ev.Type == stream {
			sb.WriteString(ev.Data)
		}
	}
	if err := <-errCh; err != nil {
		return "", err
	}
	return sb.String(), nil
}
