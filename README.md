# sandbox-go

Go SDK for [Vtrix](https://github.com/VtrixAI) sandbox — run commands and manage files in isolated Linux environments over a persistent WebSocket connection.

## Installation

```bash
go get github.com/VtrixAI/sandbox-go
```

**Requires Go 1.21+**

## Quick Start

```go
package main

import (
    "context"
    "fmt"
    "log"

    sandbox "github.com/VtrixAI/sandbox-go/src"
)

func main() {
    client := sandbox.NewClient(sandbox.ClientOptions{
        BaseURL:   "http://your-hermes-host:8080",
        Token:     "your-token",
        ServiceID: "your-service-id",
    })

    ctx := context.Background()

    // Create a sandbox and wait for it to become active
    sb, err := client.Create(ctx, sandbox.CreateOptions{UserID: "user-123"})
    if err != nil {
        log.Fatal(err)
    }
    defer sb.Close()

    // Run a command and get the result
    result, err := sb.RunCommand(ctx, "echo hello && uname -a", nil, nil)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("exit_code=%d\n%s\n", result.ExitCode, result.Output)
}
```

## API Reference

### Client

#### `NewClient(opts ClientOptions) *Client`

Creates a new client. The client is reusable and safe for concurrent use.

| Field | Type | Description |
|---|---|---|
| `BaseURL` | `string` | Hermes gateway URL (e.g. `http://host:8080`). |
| `Token` | `string` | Bearer token for authentication. |
| `ServiceID` | `string` | Value sent as `X-Service-ID` header. |
| `HTTPClient` | `*http.Client` | Optional custom HTTP client for proxy or TLS configuration. |

#### `client.Create(ctx, opts) (*Sandbox, error)`

Creates a new sandbox, polls until it becomes active, and opens a WebSocket connection. This is the primary entry point for starting a sandbox session.

| Parameter | Type | Description |
|---|---|---|
| `opts.UserID` | `string` | Owner of the sandbox. |
| `opts.Spec` | `*Spec` | Optional resource spec (`CPU`, `Memory`, `Image`). |
| `opts.Labels` | `map[string]string` | Arbitrary key-value metadata attached to the sandbox. |
| `opts.Payloads` | `[]Payload` | Initialisation calls sent to the pod after creation. |
| `opts.TTLHours` | `int` | Sandbox lifetime in hours. Uses the server default when 0. |
| `opts.Env` | `map[string]string` | Default environment variables inherited by all commands. Per-command `RunOptions.Env` values override these. |

**Returns:** `(*Sandbox, error)`

```go
sb, err := client.Create(ctx, sandbox.CreateOptions{
    UserID:   "user-123",
    Spec:     &sandbox.Spec{CPU: "2", Memory: "4Gi"},
    TTLHours: 2,
    Env:      map[string]string{"NODE_ENV": "production"},
})
```

#### `client.Attach(ctx, sandboxID, token, serviceID) (*Sandbox, error)`

Connects to an existing sandbox without creating a new one. Use this to resume a session after a restart or to connect from a different process. Pass empty strings for `token` and `serviceID` to fall back to the client-level values.

**Returns:** `(*Sandbox, error)`

```go
sb, err := client.Attach(ctx, "sandbox-id-abc", "", "")
```

#### `client.List(ctx, opts) (*ListResult, error)`

Lists sandboxes visible to the current credentials. Filter by `UserID` or `Status` to scope results.

| Parameter | Type | Description |
|---|---|---|
| `opts.UserID` | `string` | Return only sandboxes owned by this user. |
| `opts.Status` | `string` | Filter by status: `"active"`, `"stopped"`, etc. |
| `opts.Limit` | `int` | Maximum number of results. |
| `opts.Offset` | `int` | Pagination offset. |

**Returns:** `(*ListResult, error)` — `ListResult.Items` is `[]Info`, `ListResult.Pagination` has `Total`, `Limit`, `Offset`, `HasMore`.

#### `client.Get(ctx, sandboxID) (*Info, error)`

Fetches metadata for a sandbox by ID without opening a WebSocket connection.

**Returns:** `(*Info, error)`

#### `client.Delete(ctx, sandboxID) error`

Permanently deletes a sandbox. This cannot be undone.

---

### Running Commands

#### `sandbox.RunCommand(ctx, cmd, args, opts) (*CommandFinished, error)`

Runs a command and blocks until it finishes. Set `opts.Stdout` or `opts.Stderr` to receive output in real time while still blocking for the final result — useful for progress logging.

| Parameter | Type | Description |
|---|---|---|
| `cmd` | `string` | Shell command to run. |
| `args` | `[]string` | Arguments shell-quoted and appended to `cmd`. Prevents injection. |
| `opts.WorkingDir` | `string` | Working directory inside the sandbox. |
| `opts.TimeoutSec` | `uint64` | Kill the command after this many seconds. |
| `opts.Env` | `map[string]string` | Per-command environment variables. Merges with sandbox defaults. |
| `opts.Sudo` | `bool` | Prepend `sudo -E` to the command. |
| `opts.Stdin` | `string` | Data written to the command's stdin before reading output. |
| `opts.Stdout` | `io.Writer` | Receives stdout chunks as they arrive. |
| `opts.Stderr` | `io.Writer` | Receives stderr chunks as they arrive. |

**Returns:** `(*CommandFinished, error)` — `ExitCode`, `Output`, `CmdID`.

```go
result, err := sb.RunCommand(ctx, "npm install", nil, &sandbox.RunOptions{
    WorkingDir: "/app",
    Stdout:     os.Stdout,
    Stderr:     os.Stderr,
})
```

#### `sandbox.RunCommandStream(ctx, cmd, args, opts) (<-chan ExecEvent, <-chan *CommandFinished, <-chan error)`

Runs a command and streams `ExecEvent` values in real time. Use this instead of `RunCommand` when you need to process stdout and stderr as separate, typed events (e.g. to display them with different colours).

`eventCh` is closed when the command finishes. Read the final result from `resultCh` or check `errCh` for errors.

| `ExecEvent.Type` | Meaning |
|---|---|
| `"start"` | Command has started executing. |
| `"stdout"` | A chunk of standard output. Read from `ev.Data`. |
| `"stderr"` | A chunk of standard error. Read from `ev.Data`. |
| `"done"` | Command has finished. |

```go
eventCh, resultCh, errCh := sb.RunCommandStream(ctx, "make build", nil, nil)
for ev := range eventCh {
    switch ev.Type {
    case "stdout":
        fmt.Print(ev.Data)
    case "stderr":
        fmt.Fprint(os.Stderr, ev.Data)
    }
}
select {
case result := <-resultCh:
    fmt.Printf("exit_code=%d\n", result.ExitCode)
case err := <-errCh:
    log.Fatal(err)
}
```

#### `sandbox.RunCommandDetached(ctx, cmd, args, opts) (*Command, error)`

Starts a command in the background and returns immediately. Use this for long-running processes such as servers or build jobs where you want to do other work while the command runs, then call `cmd.Wait()` when you need the result.

**Returns:** `(*Command, error)` — `CmdID`, `PID`, `StartedAt`, `WorkingDir`.

```go
cmd, err := sb.RunCommandDetached(ctx, "python server.py", nil, &sandbox.RunOptions{
    WorkingDir: "/app",
    Env:        map[string]string{"PORT": "8080"},
})
// ... do other work ...
finished, err := cmd.Wait(ctx)
```

#### `sandbox.ExecLogs(ctx, cmdID) (<-chan ExecEvent, <-chan *ExecResult, <-chan error)`

Attaches to a running or completed command and streams its output. Replays buffered output first (up to 512 KB), then streams live events for commands still running. Use this to replay logs from a detached command or to attach a second observer.

**Returns:** Three channels: events, final result, error.

```go
eventCh, resultCh, errCh := sb.ExecLogs(ctx, cmd.CmdID)
for ev := range eventCh {
    fmt.Printf("[%s] %s", ev.Type, ev.Data)
}
```

#### `sandbox.GetCommand(cmdID) *Command`

Reconstructs a `Command` handle from a known `cmdID`. Use this to reconnect to a command started in a previous call or a different goroutine without going through `RunCommandDetached` again.

**Returns:** `*Command`

#### `sandbox.Kill(ctx, cmdID, signal) error`

Sends a signal to a running command by ID. The signal is sent to the entire process group, so child processes are also terminated.

| Parameter | Type | Description |
|---|---|---|
| `cmdID` | `string` | ID of the command to signal. |
| `signal` | `string` | Signal name: `"SIGTERM"` (default), `"SIGKILL"`, `"SIGINT"`, `"SIGHUP"`. |

---

### Command

A `Command` represents a running or completed process. You receive one from `RunCommandDetached` or `GetCommand`. `CommandFinished` embeds `Command` and adds `ExitCode` and `Output`.

#### `command.Wait(ctx) (*CommandFinished, error)`

Blocks until the command finishes and returns the final result. Essential after `RunCommandDetached` when you need the exit code or output.

**Returns:** `(*CommandFinished, error)` — `ExitCode`, `Output`, `CmdID`.

#### `command.Logs(ctx) (<-chan LogEvent, <-chan error)`

Streams structured log entries as they arrive. Each `LogEvent` has `Stream` (`"stdout"` or `"stderr"`) and `Data`. Use this instead of `ExecLogs` when you already have a `Command` handle.

```go
logCh, errCh := cmd.Logs(ctx)
for ev := range logCh {
    fmt.Printf("[%s] %s\n", ev.Stream, ev.Data)
}
if err := <-errCh; err != nil {
    log.Fatal(err)
}
```

#### `command.Stdout(ctx) (string, error)`

Collects the full standard output as a string. Call this after `Wait()` when you need to parse the complete output rather than process it line by line.

**Returns:** `(string, error)`

#### `command.Stderr(ctx) (string, error)`

Collects the full standard error output as a string.

**Returns:** `(string, error)`

#### `command.CollectOutput(ctx, stream) (string, error)`

Collects stdout, stderr, or both as a single string.

| Parameter | Type | Description |
|---|---|---|
| `stream` | `string` | `"stdout"`, `"stderr"`, or `"both"`. |

**Returns:** `(string, error)`

#### `command.Kill(ctx, signal) error`

Sends a signal to this command. See `sandbox.Kill` for valid signal names.

---

### File Operations

#### `sandbox.Read(ctx, path) (*ReadResult, error)`

Reads a file from the sandbox. Text files up to 200 KB are returned in full; larger files are truncated (`Truncated: true`). Image files are detected automatically and returned as base64-encoded data with a MIME type. Returns an error if the file does not exist.

| Field | Type | Description |
|---|---|---|
| `Type` | `string` | `"text"` or `"image"`. |
| `Content` | `string` | File content (text files). |
| `Truncated` | `bool` | `true` if the file was larger than 200 KB and content was cut. Use `ReadStream` for full content. |
| `Data` | `string` | Base64-encoded bytes (image files). |
| `MimeType` | `string` | MIME type (image files, e.g. `"image/png"`). |

```go
result, err := sb.Read(ctx, "/app/config.json")
if result.Truncated {
    // use ReadStream for the full file
}
```

#### `sandbox.Write(ctx, path, content) (*WriteResult, error)`

Writes a text string to a file. Creates parent directories automatically. Returns the number of bytes written.

**Returns:** `(*WriteResult, error)` — `BytesWritten`.

#### `sandbox.Edit(ctx, path, oldText, newText) (*EditResult, error)`

Replaces an exact occurrence of `oldText` with `newText` inside a file. Returns an error if `oldText` appears zero times or more than once — ensuring the edit is unambiguous.

**Returns:** `(*EditResult, error)` — `Message`.

#### `sandbox.WriteFiles(ctx, files) error`

Writes one or more binary files in a single round trip. Creates parent directories automatically. Use this for uploading compiled binaries, images, or executable scripts.

| Parameter | Type | Description |
|---|---|---|
| `files[].Path` | `string` | Destination path inside the sandbox. |
| `files[].Content` | `[]byte` | Raw file bytes. |
| `files[].Mode` | `uint32` | Unix permission bits (e.g. `0o755` for executable). Uses server default when 0. |

```go
err := sb.WriteFiles(ctx, []sandbox.WriteFileEntry{
    {Path: "/app/run.sh", Content: scriptBytes, Mode: 0o755},
    {Path: "/app/data.bin", Content: dataBytes},
})
```

#### `sandbox.ReadToBuffer(ctx, path) ([]byte, error)`

Reads a file into memory as raw bytes. Returns `nil` (not an error) when the file does not exist, making it easy to check for optional files without error handling.

**Returns:** `([]byte, error)` — `nil` if the file does not exist.

#### `sandbox.ReadStream(ctx, path, chunkSize) (<-chan ReadStreamChunk, <-chan ReadStreamResult, <-chan error)`

Reads a large file in chunks. Use this instead of `Read` when the file exceeds 200 KB or you need the complete binary content without truncation. Each chunk's `Data` field is base64-encoded.

| Parameter | Type | Description |
|---|---|---|
| `path` | `string` | File path inside the sandbox. |
| `chunkSize` | `int` | Bytes per chunk. Pass `0` for the server default (65536). |

```go
chunkCh, resultCh, errCh := sb.ReadStream(ctx, "/data/large.csv", 0)
f, _ := os.Create("large.csv")
for chunk := range chunkCh {
    decoded, _ := base64.StdEncoding.DecodeString(chunk.Data)
    f.Write(decoded)
}
```

#### `sandbox.MkDir(ctx, path) error`

Creates a directory and all parent directories. Safe to call on paths that already exist.

#### `sandbox.ListFiles(ctx, path) ([]FileEntry, error)`

Lists the contents of a directory. Returns an error if the path does not exist or is not a directory.

Each `FileEntry` has: `Name`, `Path`, `Size`, `IsDir`, `ModifiedAt` (RFC 3339 or `nil`).

#### `sandbox.Stat(ctx, path) (*FileInfo, error)`

Returns metadata for a path. Unlike most operations, this does **not** error when the path does not exist — check `FileInfo.Exists` instead.

| Field | Type | Description |
|---|---|---|
| `Exists` | `bool` | `false` when the path does not exist. |
| `IsFile` | `bool` | `true` for regular files. |
| `IsDir` | `bool` | `true` for directories. |
| `Size` | `int64` | File size in bytes. |
| `ModifiedAt` | `*string` | RFC 3339 timestamp, or `nil`. |

#### `sandbox.Exists(ctx, path) (bool, error)`

Returns `true` if the path exists (file or directory). A convenient shorthand for `Stat` when you only need the existence check.

#### `sandbox.UploadFile(ctx, localPath, sandboxPath, opts) error`

Uploads a file from the local filesystem into the sandbox.

| Parameter | Type | Description |
|---|---|---|
| `opts.MkdirRecursive` | `bool` | Create parent directories on the sandbox side if they do not exist. |

#### `sandbox.DownloadFile(ctx, sandboxPath, localPath, opts) (string, error)`

Downloads a file from the sandbox to the local filesystem. Returns the absolute local path on success, or `""` when the sandbox file does not exist.

| Parameter | Type | Description |
|---|---|---|
| `opts.MkdirRecursive` | `bool` | Create local parent directories if they do not exist. |

**Returns:** `(string, error)` — empty string if the file does not exist.

#### `sandbox.DownloadFiles(ctx, entries, opts) (map[string]string, error)`

Downloads multiple files in parallel. Returns a map of sandbox path → local path for each file that was successfully downloaded.

#### `sandbox.Domain(port) string`

Returns the publicly accessible URL for an exposed port. The sandbox must have been created with this port declared.

```go
url := sb.Domain(3000) // "https://3000-preview.example.com"
```

---

### Lifecycle

#### `sandbox.Refresh(ctx, client) error`

Re-fetches the sandbox metadata from the server and updates `sb.Info`. Call this before reading `Status()` or `ExpireAt()` if you need current values.

#### `sandbox.Stop(ctx, client, opts) error`

Pauses the sandbox without deleting it. Set `opts.Blocking` to wait until the sandbox reaches `"stopped"` or `"failed"` status before returning.

| Parameter | Type | Description |
|---|---|---|
| `opts.Blocking` | `bool` | Poll until the sandbox has stopped. |
| `opts.PollInterval` | `time.Duration` | How often to poll. Defaults to 2s. |
| `opts.Timeout` | `time.Duration` | Maximum time to wait. Defaults to 5 minutes. |

#### `sandbox.Start(ctx, client) error`

Resumes a stopped sandbox.

#### `sandbox.Restart(ctx, client) error`

Stops and restarts the sandbox.

#### `sandbox.Extend(ctx, client, durationMs) error`

Extends the sandbox TTL by `durationMs` milliseconds. Pass `0` to use the server default (12 hours).

```go
// Extend by 30 minutes
sb.Extend(ctx, client, 30*60*1000)
```

#### `sandbox.ExtendTimeout(ctx, client, durationMs) error`

Extends the TTL and immediately refreshes `sb.Info`.

#### `sandbox.Status() string`

Returns the cached status string (`"active"`, `"stopped"`, etc.). Call `Refresh` first for a live value.

#### `sandbox.ExpireAt() string`

Returns the cached expiry timestamp in RFC 3339 format.

#### `sandbox.Timeout() int64`

Returns the remaining sandbox lifetime in milliseconds based on the cached `ExpireAt`. Returns `0` if the sandbox has already expired. Call `Refresh` first for an accurate value.

#### `sandbox.Update(ctx, client, opts) error`

Updates the sandbox spec, image, or payloads. Changing payloads triggers a sandbox restart.

| Parameter | Type | Description |
|---|---|---|
| `opts.Spec` | `*Spec` | New resource spec. |
| `opts.Image` | `string` | New container image tag. |
| `opts.Payloads` | `[]Payload` | Replaces all stored payloads and triggers a restart. |

#### `sandbox.Configure(ctx, client, payloads...) error`

Immediately applies the current configuration to the running pod. Optionally override the stored payloads for this apply only.

#### `sandbox.Delete(ctx, client) error`

Permanently deletes the sandbox. This cannot be undone.

#### `sandbox.Close()`

Closes the WebSocket connection. Call this (or use `defer sb.Close()`) when you are done with the sandbox to free the connection.

---

## Examples

| File | Description |
|---|---|
| [`examples/basic/main.go`](examples/basic/main.go) | Create a sandbox, run commands, use detached execution |
| [`examples/stream/main.go`](examples/stream/main.go) | Real-time streaming, exec_logs replay, `Command.Logs`/`Stdout` |
| [`examples/files/main.go`](examples/files/main.go) | Read, write, edit, upload, download, and stream files |
| [`examples/lifecycle/main.go`](examples/lifecycle/main.go) | Stop, start, extend, update, and delete sandboxes |
| [`examples/attach/main.go`](examples/attach/main.go) | Reconnect to an existing sandbox by ID |

```bash
cd examples/basic && go run main.go
```

## License

MIT — see [LICENSE](LICENSE).
