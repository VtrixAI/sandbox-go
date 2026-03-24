package sandbox

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
)

// Execute runs a bash command and returns the result as a CommandFinished.
func (s *Sandbox) Execute(ctx context.Context, command string, opts *ExecOptions) (*CommandFinished, error) {
	resp, err := s.call(ctx, "exec", s.buildExecParams(command, opts), nil)
	if err != nil {
		return nil, err
	}
	var result ExecResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("exec result parse: %w", err)
	}
	return &CommandFinished{
		Command:  Command{CmdID: result.CmdID, sandbox: s, StartedAt: time.Now()},
		ExitCode: result.ExitCode,
		Output:   result.Output,
	}, nil
}

// ExecuteStream runs a bash command and streams ExecEvents in real time.
// eventCh is closed when the command finishes.
// The final CommandFinished is sent on resultCh, or an error on errCh.
func (s *Sandbox) ExecuteStream(ctx context.Context, command string, opts *ExecOptions) (<-chan ExecEvent, <-chan *CommandFinished, <-chan error) {
	eventCh := make(chan ExecEvent, 64)
	resultCh := make(chan *CommandFinished, 1)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(resultCh)
		defer close(errCh)

		stream := make(chan ExecEvent, 64)
		type callResult struct {
			resp *rpcResponse
			err  error
		}
		callDone := make(chan callResult, 1)
		go func() {
			resp, err := s.call(ctx, "exec", s.buildExecParams(command, opts), stream)
			callDone <- callResult{resp, err}
		}()

		for ev := range stream {
			eventCh <- ev
		}

		cr := <-callDone
		if cr.err != nil {
			errCh <- cr.err
			return
		}
		var result ExecResult
		if err := json.Unmarshal(cr.resp.Result, &result); err != nil {
			errCh <- fmt.Errorf("exec result parse: %w", err)
			return
		}
		resultCh <- &CommandFinished{
			Command:  Command{CmdID: result.CmdID, sandbox: s, StartedAt: time.Now()},
			ExitCode: result.ExitCode,
			Output:   result.Output,
		}
	}()

	return eventCh, resultCh, errCh
}

// ExecuteDetached starts a command in background (detached) mode.
// Returns immediately with a Command object for tracking the running process.
func (s *Sandbox) ExecuteDetached(ctx context.Context, command string, opts *ExecOptions) (*Command, error) {
	params := s.buildExecParams(command, opts)
	params["detached"] = true

	resp, err := s.call(ctx, "exec", params, nil)
	if err != nil {
		return nil, err
	}
	var result detachedResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("exec detached result parse: %w", err)
	}
	workingDir := ""
	if opts != nil {
		workingDir = opts.WorkingDir
	}
	return &Command{
		CmdID:      result.CmdID,
		PID:        result.PID,
		StartedAt:  time.Now(),
		WorkingDir: workingDir,
		sandbox:    s,
	}, nil
}

// GetCommand reconstructs a Command object from a known cmd_id.
// Useful for reconnecting to a command started in a previous call.
func (s *Sandbox) GetCommand(cmdID string) *Command {
	return &Command{CmdID: cmdID, sandbox: s}
}

// Kill sends a signal to a running command by ID.
// signal is one of "SIGTERM" (default), "SIGKILL", "SIGINT", "SIGHUP".
func (s *Sandbox) Kill(ctx context.Context, cmdID string, signal string) error {
	if signal == "" {
		signal = "SIGTERM"
	}
	_, err := s.call(ctx, "kill", map[string]any{"cmd_id": cmdID, "signal": signal}, nil)
	return err
}

// ExecLogs attaches to a running or completed command and streams its output.
// Replays the ring buffer first, then streams live output.
// eventCh is closed when the command finishes.
// The final ExecResult is sent on resultCh, or an error on errCh.
func (s *Sandbox) ExecLogs(ctx context.Context, cmdID string) (<-chan ExecEvent, <-chan *ExecResult, <-chan error) {
	eventCh := make(chan ExecEvent, 64)
	resultCh := make(chan *ExecResult, 1)
	errCh := make(chan error, 1)

	go func() {
		defer close(eventCh)
		defer close(resultCh)
		defer close(errCh)

		stream := make(chan ExecEvent, 64)
		type callResult struct {
			resp *rpcResponse
			err  error
		}
		callDone := make(chan callResult, 1)
		go func() {
			resp, err := s.call(ctx, "exec_logs", map[string]any{"cmd_id": cmdID}, stream)
			callDone <- callResult{resp, err}
		}()

		for ev := range stream {
			eventCh <- ev
		}

		cr := <-callDone
		if cr.err != nil {
			errCh <- cr.err
			return
		}
		var result ExecResult
		if err := json.Unmarshal(cr.resp.Result, &result); err != nil {
			errCh <- fmt.Errorf("exec_logs result parse: %w", err)
			return
		}
		resultCh <- &result
	}()

	return eventCh, resultCh, errCh
}

func (s *Sandbox) buildExecParams(command string, opts *ExecOptions) map[string]any {
	// Append shell-quoted args to the command string if provided.
	if opts != nil && len(opts.Args) > 0 {
		parts := make([]string, 0, len(opts.Args)+1)
		parts = append(parts, command)
		for _, arg := range opts.Args {
			parts = append(parts, shellQuote(arg))
		}
		command = strings.Join(parts, " ")
	}
	params := map[string]any{"command": command}

	// start with sandbox-level default env
	merged := make(map[string]string, len(s.defaultEnv))
	for k, v := range s.defaultEnv {
		merged[k] = v
	}
	// per-request env overrides defaults
	if opts != nil {
		for k, v := range opts.Env {
			merged[k] = v
		}
	}
	if len(merged) > 0 {
		params["env"] = merged
	}

	if opts != nil {
		if opts.WorkingDir != "" {
			params["working_dir"] = opts.WorkingDir
		}
		if opts.TimeoutSec > 0 {
			params["timeout"] = opts.TimeoutSec
		}
		if opts.Sudo {
			params["sudo"] = true
		}
		if opts.Stdin != "" {
			params["stdin"] = opts.Stdin
		}
	}
	return params
}

// shellQuote returns a single-quoted shell-safe version of s.
// Single quotes are escaped by ending the quote, inserting a literal quote, and reopening.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'"
}
