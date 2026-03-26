# sandbox-go

Go SDK for [Vtrix](https://github.com/VtrixAI) sandbox — run commands and manage files in isolated Linux environments over a persistent WebSocket connection.

**Requires Go 1.21+**

## Installation

```bash
go get github.com/VtrixAI/sandbox-go
```

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
        ProjectID: "your-project-id",
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

## Core types

| Type | What it does |
|---|---|
| [`Client`](#client) | Creates and manages sandbox instances |
| [`Sandbox`](#sandbox) | Runs commands and manages files in an isolated environment |
| [`Command`](#command) | Handles a running or completed process |
| [`CommandFinished`](#command) | Result after a command completes — embeds `Command` and adds `ExitCode` and `Output` |

---

## Client

### `NewClient(opts ClientOptions) *Client`

Creates a new client. The client is reusable and safe for concurrent use across multiple sandbox sessions.

| Field | Type | Required | Description |
|---|---|---|---|
| `BaseURL` | `string` | Yes | Hermes gateway URL (e.g. `http://host:8080`). |
| `Token` | `string` | No | Bearer token for authentication. |
| `ProjectID` | `string` | No | Value sent as `X-Project-ID` header. |
| `HTTPClient` | `*http.Client` | No | Optional custom HTTP client for proxy or TLS configuration. |

```go
client := sandbox.NewClient(sandbox.ClientOptions{
    BaseURL:   "http://your-hermes-host:8080",
    Token:     "your-token",
    ProjectID: "your-project-id",
})
```

### `client.Create(ctx, opts) (*Sandbox, error)`

Use `client.Create()` to launch a new sandbox, poll until it is active, and open a WebSocket connection. This is the primary entry point for starting a sandbox session. Pass `Env` to set default environment variables that all commands in this sandbox will inherit.

**Returns:** `(*Sandbox, error)`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `opts.UserID` | `string` | Yes | Owner of the sandbox. |
| `opts.Spec` | `*Spec` | No | Resource spec (`CPU`, `Memory`, `Image`). |
| `opts.Labels` | `map[string]string` | No | Arbitrary key-value metadata attached to the sandbox. |
| `opts.Payloads` | `[]Payload` | No | Initialisation calls sent to the pod after creation. |
| `opts.TTLHours` | `int` | No | Sandbox lifetime in hours. Uses the server default when `0`. |
| `opts.Env` | `map[string]string` | No | Default environment variables inherited by all commands. Per-command `RunOptions.Env` values override these. |

```go
sb, err := client.Create(ctx, sandbox.CreateOptions{
    UserID:   "user-123",
    Spec:     &sandbox.Spec{CPU: "2", Memory: "4Gi"},
    TTLHours: 2,
    Env:      map[string]string{"NODE_ENV": "production"},
})
```

### `client.Attach(ctx, sandboxID) (*Sandbox, error)`

Use `client.Attach()` to connect to an existing sandbox without creating a new one. Use this to resume a session after a restart or to connect from a different goroutine. Auth uses the client-level token and project ID.

**Returns:** `(*Sandbox, error)`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `sandboxID` | `string` | Yes | ID of the sandbox to connect to. |

```go
sb, err := client.Attach(ctx, "sandbox-id-abc")
```

### `client.List(ctx, opts) (*ListResult, error)`

Use `client.List()` to enumerate sandboxes visible to the current credentials. Filter by `UserID` or `Status` to scope results.

**Returns:** `(*ListResult, error)` — `ListResult.Items` is `[]Info`, `ListResult.Pagination` has `Total`, `Limit`, `Offset`, `HasMore`.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `opts.UserID` | `string` | No | Return only sandboxes owned by this user. |
| `opts.Status` | `string` | No | Filter by status: `"active"`, `"stopped"`, etc. |
| `opts.Limit` | `int` | No | Maximum number of results. |
| `opts.Offset` | `int` | No | Pagination offset. |

```go
result, err := client.List(ctx, sandbox.ListOptions{UserID: "user-123", Status: "active"})
fmt.Printf("Found %d sandboxes\n", result.Pagination.Total)
```

### `client.Get(ctx, sandboxID) (*Info, error)`

Use `client.Get()` to fetch metadata for a sandbox by ID without opening a WebSocket connection.

**Returns:** `(*Info, error)`

```go
info, err := client.Get(ctx, "sandbox-id-abc")
fmt.Println(info.Status)
```

### `client.Delete(ctx, sandboxID) error`

Call `client.Delete()` to permanently delete a sandbox. This cannot be undone.

**Returns:** `error`

```go
err := client.Delete(ctx, "sandbox-id-abc")
```

---

## Sandbox

A `*Sandbox` gives you full control over an isolated environment. You receive one from `client.Create()` or `client.Attach()`.

### Methods

#### `sandbox.CreatedAt() time.Time`

`CreatedAt()` returns the sandbox creation time parsed from `sb.Info.CreatedAt`. Returns `time.Time{}` (zero value) if the field is empty or unparsable.

**Returns:** `time.Time`

```go
fmt.Println(sb.CreatedAt().Format(time.RFC3339))
```

#### `sandbox.Status() string`

The `Status()` method returns the cached lifecycle state of the sandbox. Call `sandbox.Refresh(ctx)` first if you need a live value.

**Returns:** `string` — `"active"`, `"stopped"`, `"destroying"`, etc.

```go
fmt.Println(sb.Status())
```

#### `sandbox.ExpireAt() string`

`ExpireAt()` returns the cached expiry timestamp. Call `sandbox.Refresh(ctx)` first for an accurate value.

**Returns:** `string` — RFC 3339 timestamp.

```go
fmt.Println(sb.ExpireAt())
```

#### `sandbox.Timeout() int64`

`Timeout()` returns the remaining sandbox lifetime in milliseconds based on the cached `ExpireAt`. Returns `0` if the sandbox has already expired. Compare against upcoming commands and call `sandbox.ExtendTimeout()` if the window is too short.

**Returns:** `int64` — milliseconds remaining; `0` if expired.

```go
if sb.Timeout() < 60_000 {
    sb.ExtendTimeout(ctx, 1) // extend by 1 hour
}
```

---

## Running Commands

### `sandbox.RunCommand(ctx, cmd, args, opts) (*CommandFinished, error)`

`sandbox.RunCommand()` executes a command and blocks until it finishes. Set `opts.Stdout` or `opts.Stderr` to receive output in real time while still blocking — useful for progress logging.

**Returns:** `(*CommandFinished, error)` — `ExitCode`, `Output`, `CmdID`.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `cmd` | `string` | Yes | Shell command to run. |
| `args` | `[]string` | No | Arguments shell-quoted and appended to `cmd`. Prevents injection. |
| `opts.WorkingDir` | `string` | No | Working directory inside the sandbox. |
| `opts.TimeoutSec` | `uint64` | No | Kill the command after this many seconds. |
| `opts.Env` | `map[string]string` | No | Per-command environment variables. Merges with sandbox defaults. |
| `opts.Sudo` | `bool` | No | Prepend `sudo -E` to the command. |
| `opts.Stdin` | `string` | No | Data written to the command's stdin before reading output. |
| `opts.Stdout` | `io.Writer` | No | Receives stdout chunks as they arrive. |
| `opts.Stderr` | `io.Writer` | No | Receives stderr chunks as they arrive. |

```go
result, err := sb.RunCommand(ctx, "npm install", nil, &sandbox.RunOptions{
    WorkingDir: "/app",
    Stdout:     os.Stdout,
    Stderr:     os.Stderr,
})
if err != nil {
    log.Fatal(err)
}
fmt.Printf("exit_code=%d\n", result.ExitCode)
```

### `sandbox.RunCommandStream(ctx, cmd, args, opts) (<-chan ExecEvent, <-chan *CommandFinished, <-chan error)`

Use `sandbox.RunCommandStream()` to run a command and stream `ExecEvent` values in real time. Use this instead of `RunCommand` when you need to process stdout and stderr as separate, typed events — for example, to display them with different colours or route them to different log streams.

`eventCh` is closed when the command finishes. Read the final result from `resultCh` or check `errCh` for errors.

**Returns:** `(<-chan ExecEvent, <-chan *CommandFinished, <-chan error)`

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

### `sandbox.RunCommandDetached(ctx, cmd, args, opts) (*Command, error)`

Use `sandbox.RunCommandDetached()` to start a command in the background and return immediately. Use this for long-running processes such as servers where you want to do other work while the command runs, then call `cmd.Wait()` when you need the result.

**Returns:** `(*Command, error)` — `CmdID`, `PID`, `StartedAt`, `WorkingDir`.

```go
cmd, err := sb.RunCommandDetached(ctx, "python server.py", nil, &sandbox.RunOptions{
    WorkingDir: "/app",
    Env:        map[string]string{"PORT": "8080"},
})
if err != nil {
    log.Fatal(err)
}
// ... do other work ...
finished, err := cmd.Wait(ctx)
```

### `sandbox.ExecLogs(ctx, cmdID) (<-chan ExecEvent, <-chan *ExecResult, <-chan error)`

Use `sandbox.ExecLogs()` to attach to a running or completed command and stream its output. It replays buffered output first (up to 512 KB), then streams live events for commands still running. Use this to replay logs from a detached command or to attach a second observer.

**Returns:** `(<-chan ExecEvent, <-chan *ExecResult, <-chan error)`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `cmdID` | `string` | Yes | ID of the command to attach to. |

```go
eventCh, resultCh, errCh := sb.ExecLogs(ctx, cmd.CmdID)
for ev := range eventCh {
    fmt.Printf("[%s] %s", ev.Type, ev.Data)
}
if err := <-errCh; err != nil {
    log.Fatal(err)
}
_ = <-resultCh
```

### `sandbox.GetCommand(cmdID) *Command`

Use `sandbox.GetCommand()` to reconstruct a `*Command` handle from a known `cmdID`. Use this to reconnect to a command started in a previous call or a different goroutine without going through `RunCommandDetached` again.

**Returns:** `*Command`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `cmdID` | `string` | Yes | ID of the command to retrieve. |

```go
cmd := sb.GetCommand("cmd-id-abc")
result, err := cmd.Wait(ctx)
```

### `sandbox.Kill(ctx, cmdID, signal) error`

Call `sandbox.Kill()` to send a signal to a running command by ID. The signal is sent to the entire process group, so child processes are also terminated. Send `SIGTERM` for graceful shutdown or `SIGKILL` for immediate termination.

**Returns:** `error`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `cmdID` | `string` | Yes | ID of the command to signal. |
| `signal` | `string` | No | Signal name: `"SIGTERM"` (default), `"SIGKILL"`, `"SIGINT"`, `"SIGHUP"`. |

```go
err := sb.Kill(ctx, cmd.CmdID, "SIGTERM")
```

---

## Command

A `*Command` represents a running or completed process. You receive one from `RunCommandDetached` or `GetCommand`. `CommandFinished` embeds `Command` and adds `ExitCode` and `Output`.

**Fields:** `CmdID string`, `PID int`, `WorkingDir string`, `StartedAt time.Time`.

### `command.Wait(ctx) (*CommandFinished, error)`

Use `command.Wait()` to block until a detached command finishes and get the resulting `*CommandFinished` object. This method is essential after `RunCommandDetached` when you need the exit code or output.

**Returns:** `(*CommandFinished, error)` — `ExitCode`, `Output`, `CmdID`.

```go
cmd, _ := sb.RunCommandDetached(ctx, "python server.py", nil, nil)
// ... do other work ...
result, err := cmd.Wait(ctx)
if err != nil {
    log.Fatal(err)
}
if result.ExitCode != 0 {
    fmt.Println("Command failed:", result.Output)
}
```

### `command.Logs(ctx) (<-chan LogEvent, <-chan error)`

Call `command.Logs()` to stream structured log entries as they arrive. Each `LogEvent` has `Stream` (`"stdout"` or `"stderr"`) and `Data`. Use this instead of `sandbox.ExecLogs()` when you already have a `*Command` handle.

**Returns:** `(<-chan LogEvent, <-chan error)`

```go
logCh, errCh := cmd.Logs(ctx)
for ev := range logCh {
    fmt.Printf("[%s] %s\n", ev.Stream, ev.Data)
}
if err := <-errCh; err != nil {
    log.Fatal(err)
}
```

### `command.Stdout(ctx) (string, error)`

Use `command.Stdout()` to collect the full standard output as a string. Call this after `Wait()` when you need to parse the complete output rather than process it line by line.

**Returns:** `(string, error)`

```go
out, err := cmd.Stdout(ctx)
if err != nil {
    log.Fatal(err)
}
var data map[string]any
json.Unmarshal([]byte(out), &data)
```

### `command.Stderr(ctx) (string, error)`

Use `command.Stderr()` to collect the full standard error output as a string. Combine with `ExitCode` to build user-friendly error messages.

**Returns:** `(string, error)`

```go
errOut, err := cmd.Stderr(ctx)
if errOut != "" {
    fmt.Fprintln(os.Stderr, "Command errors:", errOut)
}
```

### `command.CollectOutput(ctx, stream) (string, error)`

Use `command.CollectOutput()` to collect stdout, stderr, or both as a single string. Choose `"both"` for combined output, or specify the stream you need to process separately.

**Returns:** `(string, error)`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `stream` | `string` | Yes | `"stdout"`, `"stderr"`, or `"both"`. |

```go
combined, err := cmd.CollectOutput(ctx, "both")
```

### `command.Kill(ctx, signal) error`

Call `command.Kill()` to send a signal to this command. See `sandbox.Kill()` for valid signal names.

**Returns:** `error`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `signal` | `string` | No | Signal name: `"SIGTERM"` (default), `"SIGKILL"`, `"SIGINT"`, `"SIGHUP"`. |

```go
err := cmd.Kill(ctx, "SIGKILL")
```

---

## File Operations

### `sandbox.Read(ctx, path) (*ReadResult, error)`

Use `sandbox.Read()` to read a file from the sandbox. Text files up to 200 KB are returned in full; larger files are truncated (`Truncated: true`). Image files are detected automatically and returned as base64-encoded data with a MIME type. Returns an error if the file does not exist.

**Returns:** `(*ReadResult, error)`

| Field | Type | Description |
|---|---|---|
| `Type` | `string` | `"text"` or `"image"`. |
| `Content` | `string` | File content (text files). |
| `Truncated` | `bool` | `true` if the file was larger than 200 KB. Use `ReadStream` for the full content. |
| `Data` | `string` | Base64-encoded bytes (image files). |
| `MimeType` | `string` | MIME type (image files, e.g. `"image/png"`). |

```go
result, err := sb.Read(ctx, "/app/config.json")
if err != nil {
    log.Fatal(err)
}
if result.Truncated {
    // use ReadStream for the full file
}
fmt.Println(result.Content)
```

### `sandbox.Write(ctx, path, content) (*WriteResult, error)`

Use `sandbox.Write()` to write a text string to a file. Creates parent directories automatically. Returns the number of bytes written.

**Returns:** `(*WriteResult, error)` — `BytesWritten`.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `path` | `string` | Yes | Destination path inside the sandbox. |
| `content` | `string` | Yes | Text content to write. |

```go
result, err := sb.Write(ctx, "/app/config.json", string(configJSON))
fmt.Printf("Wrote %d bytes\n", result.BytesWritten)
```

### `sandbox.Edit(ctx, path, oldText, newText) (*EditResult, error)`

Use `sandbox.Edit()` to replace an exact occurrence of `oldText` with `newText` inside a file. Returns an error if `oldText` appears zero times or more than once — ensuring the edit is unambiguous.

**Returns:** `(*EditResult, error)` — `Message`.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `path` | `string` | Yes | Path to the file inside the sandbox. |
| `oldText` | `string` | Yes | The exact text to find and replace. |
| `newText` | `string` | Yes | The text to substitute in its place. |

```go
_, err := sb.Edit(ctx, "/app/config.json", `"port": 3000`, `"port": 8080`)
```

### `sandbox.WriteFiles(ctx, files) error`

Use `sandbox.WriteFiles()` to upload one or more binary files in a single round trip. Creates parent directories automatically. Use this for uploading compiled binaries, images, or executable scripts.

**Returns:** `error`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `files[].Path` | `string` | Yes | Destination path inside the sandbox. |
| `files[].Content` | `[]byte` | Yes | Raw file bytes. |
| `files[].Mode` | `uint32` | No | Unix permission bits (e.g. `0o755` for executable). Uses server default when `0`. |

```go
err := sb.WriteFiles(ctx, []sandbox.WriteFileEntry{
    {Path: "/app/run.sh", Content: scriptBytes, Mode: 0o755},
    {Path: "/app/data.bin", Content: dataBytes},
})
```

### `sandbox.ReadToBuffer(ctx, path) ([]byte, error)`

Use `sandbox.ReadToBuffer()` to read a file into memory as raw bytes. Returns `nil` (not an error) when the file does not exist, making it easy to check for optional files without error branching.

**Returns:** `([]byte, error)` — `nil` if the file does not exist.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `path` | `string` | Yes | File path inside the sandbox. |

```go
buf, err := sb.ReadToBuffer(ctx, "/app/output.bin")
if err != nil {
    log.Fatal(err)
}
if buf != nil {
    process(buf)
}
```

### `sandbox.ReadStream(ctx, path, chunkSize) (<-chan ReadStreamChunk, <-chan ReadStreamResult, <-chan error)`

Use `sandbox.ReadStream()` to read a large file in chunks. Use this instead of `Read` when the file exceeds 200 KB or you need complete binary content without truncation. Each chunk's `Data` field is base64-encoded.

**Returns:** `(<-chan ReadStreamChunk, <-chan ReadStreamResult, <-chan error)`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `path` | `string` | Yes | File path inside the sandbox. |
| `chunkSize` | `int` | No | Bytes per chunk. Pass `0` for the server default (65536). |

```go
chunkCh, resultCh, errCh := sb.ReadStream(ctx, "/data/large.csv", 0)
f, _ := os.Create("large.csv")
for chunk := range chunkCh {
    decoded, _ := base64.StdEncoding.DecodeString(chunk.Data)
    f.Write(decoded)
}
if err := <-errCh; err != nil {
    log.Fatal(err)
}
```

### `sandbox.MkDir(ctx, path) error`

Use `sandbox.MkDir()` to create a directory and all parent directories. Safe to call on paths that already exist.

**Returns:** `error`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `path` | `string` | Yes | Directory to create. |

```go
err := sb.MkDir(ctx, "/app/logs")
```

### `sandbox.ListFiles(ctx, path) ([]FileEntry, error)`

Use `sandbox.ListFiles()` to list the contents of a directory. Returns an error if the path does not exist or is not a directory.

**Returns:** `([]FileEntry, error)` — each entry has `Name`, `Path`, `Size`, `IsDir`, `ModifiedAt` (RFC 3339 or `nil`).

| Parameter | Type | Required | Description |
|---|---|---|---|
| `path` | `string` | Yes | Directory path inside the sandbox. |

```go
entries, err := sb.ListFiles(ctx, "/app")
for _, entry := range entries {
    prefix := "f"
    if entry.IsDir {
        prefix = "d"
    }
    fmt.Printf("%s %s\n", prefix, entry.Name)
}
```

### `sandbox.Stat(ctx, path) (*FileInfo, error)`

Use `sandbox.Stat()` to get metadata for a path. Unlike most operations, this does **not** return an error when the path does not exist — check `FileInfo.Exists` instead.

**Returns:** `(*FileInfo, error)`

| Field | Type | Description |
|---|---|---|
| `Exists` | `bool` | `false` when the path does not exist. |
| `IsFile` | `bool` | `true` for regular files. |
| `IsDir` | `bool` | `true` for directories. |
| `Size` | `int64` | File size in bytes. |
| `ModifiedAt` | `*string` | RFC 3339 timestamp, or `nil`. |

```go
info, err := sb.Stat(ctx, "/app/config.json")
if err != nil {
    log.Fatal(err)
}
if !info.Exists {
    sb.Write(ctx, "/app/config.json", "{}")
}
```

### `sandbox.Exists(ctx, path) (bool, error)`

Use `sandbox.Exists()` to check whether a path exists. A convenient shorthand for `Stat` when you only need the existence check.

**Returns:** `(bool, error)`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `path` | `string` | Yes | Path to check. |

```go
exists, err := sb.Exists(ctx, "/app/config.json")
if exists {
    // ...
}
```

### `sandbox.UploadFile(ctx, localPath, sandboxPath, opts) error`

Use `sandbox.UploadFile()` to upload a file from the local filesystem into the sandbox.

**Returns:** `error`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `localPath` | `string` | Yes | Absolute path on the local machine. |
| `sandboxPath` | `string` | Yes | Destination path inside the sandbox. |
| `opts.MkdirRecursive` | `bool` | No | Create parent directories on the sandbox side if they do not exist. |

```go
err := sb.UploadFile(ctx, "/local/model.bin", "/app/model.bin", &sandbox.FileOptions{MkdirRecursive: true})
```

### `sandbox.DownloadFile(ctx, sandboxPath, localPath, opts) (string, error)`

Use `sandbox.DownloadFile()` to download a file from the sandbox to the local filesystem. Returns the absolute local path on success, or `""` when the sandbox file does not exist.

**Returns:** `(string, error)` — empty string if the file does not exist.

| Parameter | Type | Required | Description |
|---|---|---|---|
| `sandboxPath` | `string` | Yes | Path to the file inside the sandbox. |
| `localPath` | `string` | Yes | Destination path on the local machine. |
| `opts.MkdirRecursive` | `bool` | No | Create local parent directories if they do not exist. |

```go
dst, err := sb.DownloadFile(ctx, "/app/output.json", "/tmp/output.json", nil)
if dst != "" {
    fmt.Printf("Saved to %s\n", dst)
}
```

### `sandbox.DownloadFiles(ctx, entries, opts) (map[string]string, error)`

Use `sandbox.DownloadFiles()` to download multiple files in parallel (up to 8 concurrent). Returns a map of sandbox path → local path for each file that was successfully downloaded.

**Returns:** `(map[string]string, error)`

```go
results, err := sb.DownloadFiles(ctx, []sandbox.DownloadEntry{
    {SandboxPath: "/app/out.json", LocalPath: "/tmp/out.json"},
    {SandboxPath: "/app/log.txt", LocalPath: "/tmp/log.txt"},
}, nil)
```

### `sandbox.Domain(port) string`

Use `sandbox.Domain()` to get the publicly accessible URL for an exposed port. The sandbox must be created with this port declared.

**Returns:** `string`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `port` | `int` | Yes | Port number to resolve. |

```go
url := sb.Domain(3000)
fmt.Printf("App running at %s\n", url)
```

---

## Lifecycle

### `sandbox.Refresh(ctx) error`

Call `sandbox.Refresh()` to re-fetch sandbox metadata from the server and update `sb.Info`. Call this before reading `Status()` or `ExpireAt()` if you need current values.

**Returns:** `error`

```go
if err := sb.Refresh(ctx); err != nil {
    log.Fatal(err)
}
fmt.Println(sb.Status())
```

### `sandbox.Stop(ctx, opts) error`

Call `sandbox.Stop()` to pause the sandbox without deleting it. Set `opts.Blocking` to wait until the sandbox reaches `"stopped"` or `"failed"` status before returning.

**Returns:** `error`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `opts.Blocking` | `bool` | No | Poll until the sandbox has stopped. |
| `opts.PollInterval` | `time.Duration` | No | How often to poll. Defaults to `2s`. |
| `opts.Timeout` | `time.Duration` | No | Maximum time to wait. Defaults to `5 minutes`. |

```go
err := sb.Stop(ctx, &sandbox.StopOptions{Blocking: true})
```

### `sandbox.Start(ctx) error`

Use `sandbox.Start()` to resume a stopped sandbox.

**Returns:** `error`

```go
err := sb.Start(ctx)
```

### `sandbox.Restart(ctx) error`

Use `sandbox.Restart()` to stop and restart the sandbox.

**Returns:** `error`

```go
err := sb.Restart(ctx)
```

### `sandbox.Extend(ctx, hours) error`

Use `sandbox.Extend()` to extend the sandbox TTL by `hours`. Pass `0` to use the server default (12 hours).

**Returns:** `error`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `hours` | `int` | No | Number of hours to add. Pass `0` for the server default (12 hours). |

```go
err := sb.Extend(ctx, 2) // extend by 2 hours
```

### `sandbox.ExtendTimeout(ctx, hours) error`

Use `sandbox.ExtendTimeout()` to extend the TTL and immediately refresh `sb.Info` in one call.

**Returns:** `error`

```go
err := sb.ExtendTimeout(ctx, 1) // +1 hour, then refresh
```

### `sandbox.Update(ctx, opts) error`

Use `sandbox.Update()` to change the sandbox spec, image, or payloads. Changing payloads triggers a sandbox restart.

**Returns:** `error`

| Parameter | Type | Required | Description |
|---|---|---|---|
| `opts.Spec` | `*Spec` | No | New resource spec. |
| `opts.Image` | `string` | No | New container image tag. |
| `opts.Payloads` | `[]Payload` | No | Replaces all stored payloads and triggers a restart. |

```go
err := sb.Update(ctx, sandbox.UpdateOptions{
    Spec: &sandbox.Spec{CPU: "4", Memory: "8Gi"},
})
```

### `sandbox.Configure(ctx, payloads...) error`

Call `sandbox.Configure()` to immediately apply the current configuration to the running pod. Optionally override the stored payloads for this apply only.

**Returns:** `error`

```go
err := sb.Configure(ctx)
```

### `sandbox.Delete(ctx) error`

Call `sandbox.Delete()` to permanently delete the sandbox. This cannot be undone.

**Returns:** `error`

```go
err := sb.Delete(ctx)
```

### `sandbox.Close()`

Call `sandbox.Close()` to close the WebSocket connection. Use `defer sb.Close()` to ensure the connection is always freed.

```go
sb, err := client.Create(ctx, sandbox.CreateOptions{UserID: "user-123"})
if err != nil {
    log.Fatal(err)
}
defer sb.Close()
```

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
