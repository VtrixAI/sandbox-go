package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
)

// ---- Commands ----

// Commands provides process management operations against a running sandbox.
type Commands struct {
	cfg    ConnectionConfig
	client *http.Client
}

// Pty provides PTY (pseudo-terminal) process operations against a running sandbox.
type Pty struct {
	cfg    ConnectionConfig
	client *http.Client
}

// ---- wire types ----

type processConfig struct {
	Cmd  string            `json:"cmd"`
	Args []string          `json:"args,omitempty"`
	Envs map[string]string `json:"envs,omitempty"`
	Cwd  string            `json:"cwd,omitempty"`
}

// startRequest is the wire format for process.Process/Start.
// process.cmd is required; all other fields are optional.
type startRequest struct {
	Process processConfig `json:"process"`
	Timeout *int          `json:"timeout,omitempty"`
	Tag     string        `json:"tag,omitempty"`
	Stdin   *bool         `json:"stdin,omitempty"`
	Pty     *ptyConfig    `json:"pty,omitempty"`
}

type ptySize struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

type ptyConfig struct {
	Size ptySize `json:"size"`
}

// SSE event envelope types.
type processSSEEvent struct {
	Event *processEventUnion `json:"event"`
}

type processEventUnion struct {
	Start     *startEvent `json:"start"`
	Data      *dataEvent  `json:"data"`
	End       *endEvent   `json:"end"`
	Keepalive *struct{}   `json:"keepalive"`
}

type startEvent struct {
	PID   int    `json:"pid"`
	CmdID string `json:"cmdId"`
}

type dataEvent struct {
	Stdout string `json:"stdout"` // base64 encoded
	Stderr string `json:"stderr"` // base64 encoded
	Pty    string `json:"pty"`    // base64 encoded (PTY mode — stdout+stderr merged)
}

type endEvent struct {
	Exited bool   `json:"exited"`
	Status string `json:"status"`
	Error  *string `json:"error"`
}

type processInfoWire struct {
	PID    int               `json:"pid"`
	Config processConfig     `json:"config"`
	Tag    string            `json:"tag"`
	CmdID  string            `json:"cmdId"`
}

type listProcessResponse struct {
	Processes []processInfoWire `json:"processes"`
}

// ---- internal helpers ----

func (c *Commands) envdURL(path string) string {
	return strings.TrimRight(c.cfg.EnvdURL, "/") + path
}

func (c *Commands) setAccessToken(req *http.Request) {
	if c.cfg.AccessToken != "" {
		req.Header.Set("X-Access-Token", c.cfg.AccessToken)
	} else if c.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", c.cfg.APIKey)
	}
}

func (c *Commands) doRPC(method string, reqBody interface{}) (*http.Response, error) {
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal rpc body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, c.envdURL(method), bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	c.setAccessToken(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc %s: %w", method, err)
	}
	return resp, nil
}

// startSSE opens a streaming process RPC and returns the response body reader.
// The caller is responsible for closing the response body.
func (c *Commands) startSSE(ctx context.Context, method string, reqBody interface{}) (*http.Response, error) {
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal sse body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.envdURL(method), bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create sse request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Accept", "text/event-stream")
	c.setAccessToken(req)

	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("sse %s: %w", method, err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, parseAPIError(resp.StatusCode, body)
	}
	return resp, nil
}

// decodeBase64 decodes a base64 string; returns empty string on failure.
func decodeBase64(s string) string {
	if s == "" {
		return ""
	}
	b, err := base64.StdEncoding.DecodeString(s)
	if err != nil {
		// Try URL-safe variant as fallback.
		b, err = base64.URLEncoding.DecodeString(s)
		if err != nil {
			return s
		}
	}
	return string(b)
}

// parseSSELine extracts the data payload from a single SSE line.
// Returns ("", false) for non-data lines.
func parseSSELine(line string) (string, bool) {
	if strings.HasPrefix(line, "data:") {
		return strings.TrimSpace(strings.TrimPrefix(line, "data:")), true
	}
	return "", false
}

// buildStartRequest constructs the process.Process/Start payload.
func buildStartRequest(cmd string, opts RunOpts) startRequest {
	// Run as a shell command so that shell features (pipes, redirects, etc.) work.
	sr := startRequest{
		Process: processConfig{
			Cmd:  "/bin/bash",
			Args: []string{"-c", cmd},
			Envs: opts.Envs,
			Cwd:  opts.Cwd,
		},
		Timeout: opts.Timeout,
		Tag:     opts.Tag,
	}
	if opts.Stdin {
		t := true
		sr.Stdin = &t
	}
	// When a timeout is explicitly set to 0, treat it as nil (no timeout) per spec.
	if sr.Timeout != nil && *sr.Timeout == 0 {
		sr.Timeout = nil
	}
	return sr
}

// ---- CommandHandle ----

// CommandHandle provides access to a running (or recently completed) process.
type CommandHandle struct {
	mu       sync.Mutex
	pid      int
	cmdID    string
	cancel   context.CancelFunc
	body     io.ReadCloser
	commands *Commands

	onStdout func(string) // called for each stdout chunk while draining
	onStderr func(string) // called for each stderr chunk while draining

	// resultCh is closed (and resultVal set) when the process ends.
	resultCh  chan struct{}
	resultVal CommandResult
}

// PID returns the system process ID.
func (h *CommandHandle) PID() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.pid
}

// Disconnect detaches from the SSE stream without killing the process.
func (h *CommandHandle) Disconnect() {
	h.cancel()
}

// Kill sends SIGKILL to the process.
func (h *CommandHandle) Kill() (bool, error) {
	h.mu.Lock()
	pid := h.pid
	h.mu.Unlock()
	if pid == 0 {
		return false, fmt.Errorf("process not started")
	}
	return h.commands.Kill(pid)
}

// SendStdin writes data to the process's stdin.
func (h *CommandHandle) SendStdin(data string) error {
	h.mu.Lock()
	pid := h.pid
	h.mu.Unlock()
	if pid == 0 {
		return fmt.Errorf("process not started")
	}
	return h.commands.SendStdin(pid, data)
}

// Wait blocks until the process exits and returns its result.
// Stdout and stderr callbacks set in RunOpts.OnStdout / RunOpts.OnStderr are
// invoked for each chunk as it arrives.
// Returns *CommandExitError if the process exits with a non-zero code.
func (h *CommandHandle) Wait() (*CommandResult, error) {
	// Block until the background drain goroutine closes resultCh.
	<-h.resultCh
	h.mu.Lock()
	r := h.resultVal
	h.mu.Unlock()
	if r.ExitCode != 0 {
		return &r, &CommandExitError{ExitCode: r.ExitCode, Stdout: r.Stdout, Stderr: r.Stderr}
	}
	return &r, nil
}

// drainSSE reads SSE lines from r until an "end" event arrives or the stream
// closes, collecting stdout/stderr and returning a CommandResult.
func drainSSE(r io.ReadCloser, onStdout, onStderr func(string)) (*CommandResult, error) {
	defer r.Close()
	scanner := bufio.NewScanner(r)

	var result CommandResult
	var dataBuf strings.Builder
	ended := false

	flush := func() {
		raw := strings.TrimSpace(dataBuf.String())
		dataBuf.Reset()
		if raw == "" {
			return
		}
		var evt processSSEEvent
		if err := json.Unmarshal([]byte(raw), &evt); err != nil || evt.Event == nil {
			return
		}
		e := evt.Event
		if e.Data != nil {
			if e.Data.Stdout != "" {
				decoded := decodeBase64(e.Data.Stdout)
				result.Stdout += decoded
				if onStdout != nil {
					onStdout(decoded)
				}
			}
			if e.Data.Stderr != "" {
				decoded := decodeBase64(e.Data.Stderr)
				result.Stderr += decoded
				if onStderr != nil {
					onStderr(decoded)
				}
			}
			if e.Data.Pty != "" {
				decoded := decodeBase64(e.Data.Pty)
				result.Stdout += decoded
				if onStdout != nil {
					onStdout(decoded)
				}
			}
		}
		if e.End != nil {
			result.ExitCode = parseExitCode(e.End.Status)
			if e.End.Error != nil {
				result.Error = *e.End.Error
			}
			ended = true
		}
	}

	for scanner.Scan() {
		line := scanner.Text()
		if data, ok := parseSSELine(line); ok {
			dataBuf.WriteString(data)
		} else if line == "" {
			flush()
			if ended {
				break
			}
		}
	}
	// Flush any remaining buffered data.
	flush()

	if !ended {
		if err := scanner.Err(); err != nil && err != io.EOF {
			return nil, fmt.Errorf("read SSE stream: %w", err)
		}
	}
	return &result, nil
}

// parseExitCode extracts the numeric exit code from a status string like "exit status 1".
func parseExitCode(status string) int {
	if status == "" || status == "exit status 0" {
		return 0
	}
	parts := strings.Fields(status)
	if len(parts) > 0 {
		n, err := strconv.Atoi(parts[len(parts)-1])
		if err == nil {
			return n
		}
	}
	return 1
}

// ---- Commands methods ----

// Run executes a shell command and waits for it to complete.
func (c *Commands) Run(cmd string, opts ...RunOpts) (*CommandResult, error) {
	var o RunOpts
	if len(opts) > 0 {
		o = opts[0]
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sr := buildStartRequest(cmd, o)
	resp, err := c.startSSE(ctx, "/process.Process/Start", sr)
	if err != nil {
		return nil, err
	}

	result, err := drainSSE(resp.Body, o.OnStdout, o.OnStderr)
	if err != nil {
		return nil, err
	}
	if result.ExitCode != 0 {
		return result, &CommandExitError{ExitCode: result.ExitCode, Stdout: result.Stdout, Stderr: result.Stderr}
	}
	return result, nil
}

// RunBackground starts a command and returns a handle after the start event arrives.
// The caller can call handle.Wait() to stream remaining output.
func (c *Commands) RunBackground(cmd string, opts ...RunOpts) (*CommandHandle, error) {
	var o RunOpts
	if len(opts) > 0 {
		o = opts[0]
	}

	ctx, cancel := context.WithCancel(context.Background())

	sr := buildStartRequest(cmd, o)
	resp, err := c.startSSE(ctx, "/process.Process/Start", sr)
	if err != nil {
		cancel()
		return nil, err
	}

	// Read lines until we have the start event.
	scanner := bufio.NewScanner(resp.Body)
	var pid int
	var cmdID string
	var dataBuf strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if data, ok := parseSSELine(line); ok {
			dataBuf.WriteString(data)
		} else if line == "" && dataBuf.Len() > 0 {
			raw := strings.TrimSpace(dataBuf.String())
			dataBuf.Reset()

			var evt processSSEEvent
			if err := json.Unmarshal([]byte(raw), &evt); err == nil && evt.Event != nil && evt.Event.Start != nil {
				pid = evt.Event.Start.PID
				cmdID = evt.Event.Start.CmdID
				break
			}
		}
	}

	if pid == 0 {
		cancel()
		resp.Body.Close()
		return nil, fmt.Errorf("no start event received from process")
	}

	resultCh := make(chan struct{})
	handle := &CommandHandle{
		pid:      pid,
		cmdID:    cmdID,
		cancel:   cancel,
		body:     resp.Body,
		commands: c,
		onStdout: o.OnStdout,
		onStderr: o.OnStderr,
		resultCh: resultCh,
	}

	// Background goroutine drains remaining events.
	go func() {
		result, _ := drainSSE(resp.Body, handle.onStdout, handle.onStderr)
		if result != nil {
			handle.mu.Lock()
			handle.resultVal = *result
			handle.mu.Unlock()
		}
		select {
		case <-resultCh:
		default:
			close(resultCh)
		}
	}()

	return handle, nil
}

// Connect attaches to an already-running process by PID.
func (c *Commands) Connect(pid int, opts ...RunOpts) (*CommandHandle, error) {
	ctx, cancel := context.WithCancel(context.Background())

	body := map[string]interface{}{"process": map[string]int{"pid": pid}}
	resp, err := c.startSSE(ctx, "/process.Process/Connect", body)
	if err != nil {
		cancel()
		return nil, err
	}

	resultCh := make(chan struct{})
	handle := &CommandHandle{
		pid:      pid,
		cancel:   cancel,
		body:     resp.Body,
		commands: c,
		resultCh: resultCh,
	}

	go func() {
		result, _ := drainSSE(resp.Body, nil, nil)
		if result != nil {
			handle.mu.Lock()
			handle.resultVal = *result
			handle.mu.Unlock()
		}
		select {
		case <-resultCh:
		default:
			close(resultCh)
		}
	}()

	return handle, nil
}

// List returns the currently running processes.
func (c *Commands) List(opts ...RequestOpts) ([]ProcessInfo, error) {
	resp, err := c.doRPC("/process.Process/List", map[string]interface{}{})
	if err != nil {
		return nil, err
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	var lr listProcessResponse
	if err := json.Unmarshal(body, &lr); err != nil {
		return nil, fmt.Errorf("decode List response: %w", err)
	}

	procs := make([]ProcessInfo, len(lr.Processes))
	for i, p := range lr.Processes {
		procs[i] = ProcessInfo{
			PID:  p.PID,
			Tag:  p.Tag,
			Cmd:  p.Config.Cmd,
			Args: p.Config.Args,
			Cwd:  p.Config.Cwd,
			Envs: p.Config.Envs,
		}
	}
	return procs, nil
}

// Kill sends SIGKILL to the process identified by pid.
func (c *Commands) Kill(pid int, opts ...RequestOpts) (bool, error) {
	body := map[string]interface{}{
		"process": map[string]int{"pid": pid},
		"signal":  "SIGKILL",
	}
	resp, err := c.doRPC("/process.Process/SendSignal", body)
	if err != nil {
		return false, err
	}
	raw, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, parseAPIError(resp.StatusCode, raw)
	}
	return true, nil
}

// SendStdin sends data to the standard input of the process identified by pid.
func (c *Commands) SendStdin(pid int, data string, opts ...RequestOpts) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	body := map[string]interface{}{
		"process": map[string]int{"pid": pid},
		"input":   map[string]string{"stdin": encoded},
	}
	resp, err := c.doRPC("/process.Process/SendInput", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// SendStdinByTag sends data to the standard input of the process identified by tag.
func (c *Commands) SendStdinByTag(tag string, data string, opts ...RequestOpts) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(data))
	body := map[string]interface{}{
		"process": map[string]string{"tag": tag},
		"input":   map[string]string{"stdin": encoded},
	}
	resp, err := c.doRPC("/process.Process/SendInput", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// CloseStdin closes the stdin of the process identified by pid, triggering EOF.
func (c *Commands) CloseStdin(pid int, opts ...RequestOpts) error {
	body := map[string]interface{}{"process": map[string]int{"pid": pid}}
	resp, err := c.doRPC("/process.Process/CloseStdin", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// SendSignal sends the given signal to the process identified by pid.
func (c *Commands) SendSignal(pid int, signal Signal, opts ...RequestOpts) error {
	body := map[string]interface{}{
		"process": map[string]int{"pid": pid},
		"signal":  string(signal),
	}
	resp, err := c.doRPC("/process.Process/SendSignal", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// KillByTag sends SIGKILL to the process identified by tag.
func (c *Commands) KillByTag(tag string, opts ...RequestOpts) (bool, error) {
	body := map[string]interface{}{
		"process": map[string]string{"tag": tag},
		"signal":  "SIGKILL",
	}
	resp, err := c.doRPC("/process.Process/SendSignal", body)
	if err != nil {
		return false, err
	}
	raw, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, parseAPIError(resp.StatusCode, raw)
	}
	return true, nil
}

// GetResult retrieves structured output (stdout/stderr/exitCode) for a completed process.
// cmdId is the UUID returned in the 'start' SSE event. Call this after receiving the 'end' event.
func (c *Commands) GetResult(cmdID string, opts ...RequestOpts) (*GetResultResponse, error) {
	resp, err := c.doRPC("/process.Process/GetResult", map[string]string{"cmdId": cmdID})
	if err != nil {
		return nil, err
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}
	var raw struct {
		ExitCode      int    `json:"exitCode"`
		Stdout        string `json:"stdout"`
		Stderr        string `json:"stderr"`
		StartedAtUnix int64  `json:"startedAtUnix"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode GetResult response: %w", err)
	}
	return &GetResultResponse{
		ExitCode:      raw.ExitCode,
		Stdout:        raw.Stdout,
		Stderr:        raw.Stderr,
		StartedAtUnix: raw.StartedAtUnix,
	}, nil
}

// ConnectByTag attaches to an already-running process identified by tag.
func (c *Commands) ConnectByTag(tag string, opts ...RunOpts) (*CommandHandle, error) {
	ctx, cancel := context.WithCancel(context.Background())

	body := map[string]interface{}{"process": map[string]string{"tag": tag}}
	resp, err := c.startSSE(ctx, "/process.Process/Connect", body)
	if err != nil {
		cancel()
		return nil, err
	}

	resultCh := make(chan struct{})
	handle := &CommandHandle{
		cancel:   cancel,
		body:     resp.Body,
		commands: c,
		resultCh: resultCh,
	}

	go func() {
		result, _ := drainSSE(resp.Body, nil, nil)
		if result != nil {
			handle.mu.Lock()
			handle.resultVal = *result
			handle.mu.Unlock()
		}
		select {
		case <-resultCh:
		default:
			close(resultCh)
		}
	}()

	return handle, nil
}

// ---- Pty methods ----

func (p *Pty) envdURL(path string) string {
	return strings.TrimRight(p.cfg.EnvdURL, "/") + path
}

func (p *Pty) setAccessToken(req *http.Request) {
	if p.cfg.AccessToken != "" {
		req.Header.Set("X-Access-Token", p.cfg.AccessToken)
	} else if p.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", p.cfg.APIKey)
	}
}

func (p *Pty) doRPC(method string, reqBody interface{}) (*http.Response, error) {
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal rpc body: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, p.envdURL(method), bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	p.setAccessToken(req)

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc %s: %w", method, err)
	}
	return resp, nil
}

// Create starts a new PTY session with the given terminal size.
func (p *Pty) Create(size PtySize, opts ...PtyCreateOpts) (*CommandHandle, error) {
	var o PtyCreateOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Cmd == "" {
		o.Cmd = "/bin/bash"
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if o.TimeoutMs > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), msToTimeout(o.TimeoutMs))
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}

	sr := startRequest{
		Process: processConfig{
			Cmd:  o.Cmd,
			Envs: o.Envs,
			Cwd:  o.Cwd,
		},
		Pty: &ptyConfig{Size: ptySize{Rows: size.Rows, Cols: size.Cols}},
	}

	b, err := json.Marshal(sr)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("marshal pty start body: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.envdURL("/process.Process/Start"), bytes.NewReader(b))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create pty start request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Accept", "text/event-stream")
	p.setAccessToken(req)

	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("pty start: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		cancel()
		return nil, parseAPIError(resp.StatusCode, body)
	}

	// Read until we have the start event.
	scanner := bufio.NewScanner(resp.Body)
	var pid int
	var cmdID string
	var dataBuf strings.Builder

	for scanner.Scan() {
		line := scanner.Text()
		if data, ok := parseSSELine(line); ok {
			dataBuf.WriteString(data)
		} else if line == "" && dataBuf.Len() > 0 {
			raw := strings.TrimSpace(dataBuf.String())
			dataBuf.Reset()

			var evt processSSEEvent
			if err := json.Unmarshal([]byte(raw), &evt); err == nil && evt.Event != nil && evt.Event.Start != nil {
				pid = evt.Event.Start.PID
				cmdID = evt.Event.Start.CmdID
				break
			}
		}
	}

	if pid == 0 {
		cancel()
		resp.Body.Close()
		return nil, fmt.Errorf("no start event received from pty")
	}

	// Build a Commands proxy so Kill works on CommandHandle.
	cmds := &Commands{cfg: p.cfg, client: p.client}

	resultCh := make(chan struct{})
	handle := &CommandHandle{
		pid:      pid,
		cmdID:    cmdID,
		cancel:   cancel,
		body:     resp.Body,
		commands: cmds,
		resultCh: resultCh,
	}

	go func() {
		result, _ := drainSSE(resp.Body, nil, nil)
		if result != nil {
			handle.mu.Lock()
			handle.resultVal = *result
			handle.mu.Unlock()
		}
		select {
		case <-resultCh:
		default:
			close(resultCh)
		}
	}()

	return handle, nil
}

// Kill sends SIGKILL to a PTY process.
func (p *Pty) Kill(pid int, opts ...RequestOpts) (bool, error) {
	body := map[string]interface{}{
		"process": map[string]int{"pid": pid},
		"signal":  "SIGKILL",
	}
	resp, err := p.doRPC("/process.Process/SendSignal", body)
	if err != nil {
		return false, err
	}
	raw, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, parseAPIError(resp.StatusCode, raw)
	}
	return true, nil
}

// Resize updates the terminal dimensions of a PTY process.
func (p *Pty) Resize(pid int, size PtySize, opts ...RequestOpts) error {
	body := map[string]interface{}{
		"process": map[string]int{"pid": pid},
		"pty": map[string]interface{}{
			"size": map[string]uint16{
				"rows": size.Rows,
				"cols": size.Cols,
			},
		},
	}
	resp, err := p.doRPC("/process.Process/Update", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// SendInput sends raw bytes (e.g. keystrokes) to a PTY process.
func (p *Pty) SendInput(pid int, data []byte, opts ...RequestOpts) error {
	encoded := base64.StdEncoding.EncodeToString(data)
	body := map[string]interface{}{
		"process": map[string]int{"pid": pid},
		"input":   map[string]string{"pty": encoded},
	}
	resp, err := p.doRPC("/process.Process/SendInput", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// ---- v2 Agent-Friendly API ----

// V2RunResult holds the response from RunV2.
type V2RunResult struct {
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	ExitCode   int    `json:"exit_code"`
	DurationMs int64  `json:"duration_ms"`
	Error      string `json:"error,omitempty"`
}

// RunV2 executes a shell command synchronously via the v2 agent-friendly API (POST /v2/run).
// No Connect header required. Returns when the command exits.
func (c *Commands) RunV2(cmd string, opts ...RunOpts) (*V2RunResult, error) {
	body := map[string]interface{}{"cmd": cmd}
	var o RunOpts
	if len(opts) > 0 {
		o = opts[0]
	}
	if o.Cwd != "" {
		body["cwd"] = o.Cwd
	}
	if len(o.Envs) > 0 {
		body["env"] = o.Envs
	}
	if o.Timeout != nil {
		body["timeout"] = *o.Timeout
	}

	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal v2/run body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, c.envdURL("/v2/run"), bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create v2/run request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	c.setAccessToken(req)

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /v2/run: %w", err)
	}
	raw, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, raw)
	}
	var result V2RunResult
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, fmt.Errorf("decode v2/run response: %w", err)
	}
	return &result, nil
}

// ReadFileV2 reads a file's raw bytes via the v2 agent-friendly API (GET /v2/file).
func (f *Filesystem) ReadFileV2(path string) ([]byte, error) {
	u := f.envdURL("/v2/file") + "?path=" + url.QueryEscape(path)
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create v2/file request: %w", err)
	}
	f.setAccessToken(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /v2/file: %w", err)
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}
	return body, nil
}

// WriteFileV2 writes data to a file via the v2 agent-friendly API (POST /v2/file).
// Parent directories are created automatically by the server.
func (f *Filesystem) WriteFileV2(path string, data []byte) error {
	u := f.envdURL("/v2/file") + "?path=" + url.QueryEscape(path)
	req, err := http.NewRequest(http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return fmt.Errorf("create v2/file write request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	f.setAccessToken(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return fmt.Errorf("POST /v2/file: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return parseAPIError(resp.StatusCode, body)
	}
	return nil
}
