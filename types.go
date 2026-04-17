package sandbox

import "time"

// SandboxOpts holds options for creating or connecting to a sandbox.
type SandboxOpts struct {
	APIKey      string
	BaseURL     string // e.g. "https://api.example.com"
	Timeout     int    // sandbox lifetime seconds, default 300
	Template    string // sandbox template ID, default "base"
	WorkspaceID string // optional; workspace ID for cloud-storage access
	Metadata    map[string]string
	Envs        map[string]string
}

// ConnectionConfig holds the resolved connection parameters for a live sandbox.
type ConnectionConfig struct {
	SandboxID   string
	EnvdURL     string        // direct URL to nano-executor e.g. "https://<id>.<domain>"
	AccessToken string        // envd access token (X-Access-Token)
	APIKey      string
	BaseURL     string
	Timeout     time.Duration // request timeout
}

// SandboxInfo describes the current state of a sandbox.
type SandboxInfo struct {
	SandboxID  string
	TemplateID string
	Alias      string
	StartedAt  time.Time
	EndAt      time.Time
	Metadata   map[string]string
	State      string // "running" | "paused"
}

// EntryInfo describes a filesystem entry (file or directory).
type EntryInfo struct {
	Name          string
	Type          string // "file" | "dir"
	Path          string
	Size          int64
	Mode          uint32
	Permissions   string
	Owner         string
	Group         string
	ModifiedTime  time.Time
	SymlinkTarget string
}

// WriteInfo is returned after a successful file write.
type WriteInfo struct {
	Name string
	Path string
	Type string
}

// WriteEntry is a single file to write in a batch operation.
type WriteEntry struct {
	Path    string
	Content []byte
}

// ProcessInfo describes a running process.
type ProcessInfo struct {
	PID  int
	Tag  string
	Cmd  string
	Args []string
	Cwd  string
	Envs map[string]string
}

// CommandResult holds the output of a completed command.
type CommandResult struct {
	Stdout   string
	Stderr   string
	ExitCode int
	Error    string
}

// GetResultResponse holds the structured output returned by GetResult.
type GetResultResponse struct {
	ExitCode       int
	Stdout         string
	Stderr         string
	StartedAtUnix  int64
}

// FilesystemEvent describes a change in a watched directory.
type FilesystemEvent struct {
	Name string
	Type string // EVENT_TYPE_CREATE, EVENT_TYPE_WRITE, etc.
}

// PtySize specifies the terminal dimensions for a PTY process.
type PtySize struct {
	Rows uint16
	Cols uint16
}

// SandboxPage is one page of sandbox results returned by ListPage.
type SandboxPage struct {
	Items   []SandboxInfo
	HasMore bool   // true if there are more pages
	Next    string // opaque cursor to pass as PageToken for the next page
}

// SandboxListOpts holds options for listing sandboxes.
type SandboxListOpts struct {
	APIKey    string
	BaseURL   string
	Limit     int    // max items per page; 0 = server default
	PageToken string // cursor from a previous SandboxPage.Next
}

// --- Option types ---

// ReadOpts holds options for read operations.
type ReadOpts struct {
	User              string // run as this OS user (optional)
	RequestTimeoutMs  int    // per-request timeout in milliseconds; 0 = use client default
}

// WriteOpts holds options for write operations.
type WriteOpts struct {
	User             string // run as this OS user (optional)
	RequestTimeoutMs int    // per-request timeout in milliseconds; 0 = use client default
}

// ListOpts holds options for directory listing.
type ListOpts struct {
	Depth            int    // max recursion depth; 0 = flat (default)
	User             string // run as this OS user (optional)
	RequestTimeoutMs int    // per-request timeout in milliseconds; 0 = use client default
}

// FSRequestOpts holds common options for filesystem RPC requests.
type FSRequestOpts struct {
	User             string // run as this OS user (optional)
	RequestTimeoutMs int    // per-request timeout in milliseconds; 0 = use client default
}

// WatchOpts holds options for WatchDir.
type WatchOpts struct {
	Recursive        bool            // watch sub-directories recursively
	OnExit           func(err error) // called when the watcher stream closes
	TimeoutMs        int             // watcher stream timeout in milliseconds; 0 = no timeout
	RequestTimeoutMs int             // initial connection timeout in milliseconds; 0 = use client default
}

// RunOpts holds options for running a process.
type RunOpts struct {
	Envs             map[string]string
	Cwd              string
	Timeout          *int         // process execution timeout in seconds; nil = no timeout
	Tag              string
	User             string       // run as this OS user (optional)
	Stdin            bool         // keep stdin open for SendStdin calls
	OnStdout         func(string) // called with each decoded stdout chunk (foreground Run only)
	OnStderr         func(string) // called with each decoded stderr chunk (foreground Run only)
	RequestTimeoutMs int          // per-request timeout in milliseconds; 0 = use client default
}

// PtyCreateOpts holds options for creating a PTY session.
type PtyCreateOpts struct {
	Envs             map[string]string
	Cwd              string
	Cmd              string // default: /bin/bash
	User             string // run as this OS user (optional)
	TimeoutMs        int    // timeout waiting for PTY start event, in milliseconds; 0 = no timeout
	RequestTimeoutMs int    // per-request timeout in milliseconds; 0 = use client default
}

// RequestOpts holds generic request options.
type RequestOpts struct {
	RequestTimeoutMs int // per-request timeout in milliseconds; 0 = use client default
}

// SandboxUrlOpts holds options for generating signed file download/upload URLs.
type SandboxUrlOpts struct {
	// User is the OS user that will own/access the file.
	User string
	// UseSignatureExpiration is the URL lifetime in seconds.
	// Values <= 86400 are relative (now + N seconds); larger values are absolute unix timestamps.
	// Default: 300 (5 minutes).
	UseSignatureExpiration int
}

// Signal is the name of a POSIX signal to send to a process.
type Signal string

const (
	SignalKill Signal = "SIGKILL"
	SignalTerm Signal = "SIGTERM"
	SignalInt  Signal = "SIGINT"
	SignalHup  Signal = "SIGHUP"
)
