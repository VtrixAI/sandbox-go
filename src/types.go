package sandbox

import (
	"encoding/json"
	"io"
	"time"
)

// MaxExtendSeconds is the maximum value accepted by Extend / ExtendTimeout (matches Atlas).
const MaxExtendSeconds = 86400

// ── Public types ──────────────────────────────────────────

// Info holds the metadata returned by Atlas for a sandbox.
type Info struct {
	ID           string            `json:"id"`
	UserID       string            `json:"user_id"`
	Namespace    string            `json:"namespace"`
	Status       string            `json:"status"`
	IP           string            `json:"ip"`
	PreviewURL   string            `json:"preview_url"`
	PreviewHost  string            `json:"preview_host"`
	Port         int               `json:"port"`
	ImageTag     string            `json:"image_tag"`
	Spec         *Spec             `json:"spec,omitempty"`
	Labels       map[string]string `json:"labels,omitempty"`
	CreatedAt    string            `json:"created_at"`
	AllocatedAt  string            `json:"allocated_at"`
	ExpireAt     string            `json:"expire_at"`
	LastActiveAt string            `json:"last_active_at"`
}

// Payload represents a single initialisation call sent to the sandbox pod.
// API is the endpoint path (e.g. "/api/v1/env") and Body is the JSON body.
// Envs are optional per-item environment overrides (Atlas PayloadItem.envs).
type Payload struct {
	API  string            `json:"api"`
	Body any               `json:"body,omitempty"`
	Envs map[string]string `json:"envs,omitempty"`
}

// CreateOptions configures sandbox creation.
type CreateOptions struct {
	UserID    string            `json:"user_id"`
	Spec      *Spec             `json:"spec,omitempty"`
	Labels    map[string]string `json:"labels,omitempty"`
	Payloads  []Payload         `json:"payloads,omitempty"`
	TTLHours  int               `json:"ttl_hours,omitempty"`
	Token     string            `json:"-"` // Bearer token
	ProjectID string            `json:"-"` // X-Project-ID header
	Env       map[string]string `json:"-"` // default env inherited by all commands
}

// Spec defines resource requirements for a sandbox (Atlas ResourceSpec).
type Spec struct {
	CPU             string `json:"cpu,omitempty"`
	Memory          string `json:"memory,omitempty"`
	Image           string `json:"image,omitempty"`
	RequestsCPU     string `json:"requests_cpu,omitempty"`
	RequestsMemory  string `json:"requests_memory,omitempty"`
}

// RunOptions configures a command execution.
type RunOptions struct {
	WorkingDir string
	TimeoutSec uint64
	Env        map[string]string
	Sudo       bool     // prepend "sudo -E" to the command
	Stdin      string   // data written to the command's stdin
	// Stdout, when non-nil, receives each stdout chunk as it arrives.
	// RunCommand will use streaming internally and write to this writer.
	Stdout     io.Writer
	// Stderr, when non-nil, receives each stderr chunk as it arrives.
	Stderr     io.Writer
}

// FileOptions configures file transfer operations (DownloadFile / UploadFile).
type FileOptions struct {
	// MkdirRecursive creates parent directories if needed (local for downloads, sandbox for uploads).
	MkdirRecursive bool
}

// DownloadEntry specifies a single file to download in DownloadFiles.
type DownloadEntry struct {
	SandboxPath string
	LocalPath   string
}

// StopOptions configures a Stop call.
type StopOptions struct {
	// Blocking polls until the sandbox status is "stopped" or "failed".
	Blocking     bool
	PollInterval time.Duration // 0 → 2s
	Timeout      time.Duration // 0 → 5min
}

// ExecResult is the final result of an exec call.
type ExecResult struct {
	CmdID     string `json:"cmd_id"`
	Output    string `json:"output"`
	ExitCode  int    `json:"exit_code"`
	StartedAt string `json:"started_at"` // RFC3339 UTC; empty if not available
}

// ExecEvent is a streaming event emitted during exec.
// Type is one of "stdout", "stderr", "start", or "done".
type ExecEvent struct {
	Type string // "stdout" | "stderr" | "start" | "done"
	Data string
}

// detachedResult is the internal wire result of a detached exec call.
type detachedResult struct {
	CmdID     string `json:"cmd_id"`
	PID       uint32 `json:"pid"`
	StartedAt string `json:"started_at"` // RFC3339 UTC
}

// WriteFileEntry is a single file to write in WriteFiles.
type WriteFileEntry struct {
	Path    string
	Content []byte
	// Mode is the optional Unix file permission bits (e.g. 0o755 for executable).
	// When zero, the server default is used.
	Mode uint32
}

// ReadResult is the result of a read call.
type ReadResult struct {
	Type      string `json:"type"`      // "text" | "image"
	Content   string `json:"content"`   // text content
	Truncated bool   `json:"truncated"` // whether text was truncated
	MimeType  string `json:"mime_type"` // image mime type
	Data      string `json:"data"`      // base64 image data
}

// ReadStreamResult is the final result of a ReadStream call.
type ReadStreamResult struct {
	Type       string `json:"type"`                 // "text" | "image"
	TotalBytes uint64 `json:"total_bytes"`
	MimeType   string `json:"mime_type,omitempty"`
}

// ReadStreamChunk is a single chunk notification from a ReadStream call.
// Data is base64-encoded bytes.
type ReadStreamChunk struct {
	Data string
}

// WriteResult is the result of a write call.
type WriteResult struct {
	BytesWritten int `json:"bytes_written"`
}

// EditResult is the result of an edit call.
type EditResult struct {
	Message string `json:"message"`
}

// FileEntry is a single file or directory entry returned by ListFiles.
type FileEntry struct {
	Name       string  `json:"name"`
	Path       string  `json:"path"`
	Size       int64   `json:"size"`
	IsDir      bool    `json:"is_dir"`
	ModifiedAt *string `json:"modified_at"` // RFC 3339 or nil
}

// FileInfo is the metadata returned by Stat.
type FileInfo struct {
	Exists     bool    `json:"exists"`
	Size       int64   `json:"size"`
	IsDir      bool    `json:"is_dir"`
	IsFile     bool    `json:"is_file"`
	ModifiedAt *string `json:"modified_at"` // RFC 3339 or nil
}

// ── Lifecycle types ───────────────────────────────────────

// ListOptions filters the sandbox list query.
type ListOptions struct {
	UserID string `json:"user_id,omitempty"`
	Status string `json:"status,omitempty"`
	Limit  int    `json:"limit,omitempty"`
	Offset int    `json:"offset,omitempty"`
}

// Pagination holds paging metadata returned by the list endpoint.
type Pagination struct {
	Total   int  `json:"total"`
	Limit   int  `json:"limit"`
	Offset  int  `json:"offset"`
	HasMore bool `json:"has_more"`
}

// ListResult is the response from Client.List.
type ListResult struct {
	Items      []Info     `json:"items"`
	Pagination Pagination `json:"pagination"`
}

// UpdateOptions specifies what to change when calling Sandbox.Update (Atlas PATCH /api/v1/sandbox/:id).
// Payload changes are not supported on Update; use Configure to apply or replace stored payloads.
type UpdateOptions struct {
	Spec  *Spec  `json:"spec,omitempty"`
	Image string `json:"image,omitempty"`
}

// ── Internal wire types ───────────────────────────────────

type atlasEnvelope struct {
	Code      int             `json:"code"`
	Message   string          `json:"message"`
	Data      json.RawMessage `json:"data"`
	RequestID string          `json:"request_id"`
}

type createData struct {
	Sandbox Info `json:"sandbox"`
}

type rpcRequest struct {
	Jsonrpc string `json:"jsonrpc"`
	Method  string `json:"method"`
	Params  any    `json:"params,omitempty"`
	ID      *int64 `json:"id,omitempty"`
}

type rpcResponse struct {
	Jsonrpc string          `json:"jsonrpc"`
	Method  string          `json:"method,omitempty"` // for notifications
	Params  json.RawMessage `json:"params,omitempty"` // for notifications
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *rpcError       `json:"error,omitempty"`
	ID      *int64          `json:"id,omitempty"`
}

type rpcError struct {
	Code    int             `json:"code"`
	Message string          `json:"message"`
	Data    json.RawMessage `json:"data,omitempty"`
}

func (e *rpcError) Error() string { return e.Message }

// execNotifParams mirrors the params sent by the executor for exec.* notifications.
type execNotifParams struct {
	ID       any    `json:"id"`
	Data     string `json:"data"`
	ExitCode int    `json:"exit_code"`
	Output   string `json:"output"`
}

// readStreamNotifParams mirrors the params sent for read_stream.* notifications.
type readStreamNotifParams struct {
	ID         any    `json:"id"`
	Data       string `json:"data"`       // chunk data (base64)
	TotalBytes uint64 `json:"total_bytes"` // done event
}
