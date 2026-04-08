# sandbox-go

Go SDK for [Vtrix](https://github.com/VtrixAI) sandbox — run commands, manage files, and operate sandbox instances in isolated Linux environments via the hermes gateway.

## Installation

```bash
go get github.com/VtrixAI/sandbox-go
```

**Requires Go 1.21+**

## Quick Start

```go
package main

import (
    "fmt"
    sandbox "github.com/VtrixAI/sandbox-go"
)

func main() {
    sb, err := sandbox.Create(sandbox.SandboxOpts{
        APIKey:  "your-api-key",
        BaseURL: "http://your-hermes-host:8080",
    })
    if err != nil {
        panic(err)
    }
    defer sb.Kill()

    result, err := sb.Commands.Run("echo hello")
    if err != nil {
        panic(err)
    }
    fmt.Println(result.Stdout)
}
```

## Sandbox lifecycle

### `sandbox.Create(opts SandboxOpts) (*Sandbox, error)`

Create a new sandbox and wait until it is running.

```go
sb, err := sandbox.Create(sandbox.SandboxOpts{
    APIKey:   "your-api-key",
    BaseURL:  "http://your-hermes-host:8080",
    Template: "base",
    Timeout:  300, // seconds
    Metadata: map[string]string{"env": "dev"},
})
```

| Field | Type | Description |
|---|---|---|
| `APIKey` | `string` | API key sent as `X-API-Key`. |
| `BaseURL` | `string` | Hermes gateway URL. |
| `Template` | `string` | Template ID. Defaults to `"base"`. |
| `Timeout` | `int` | Sandbox lifetime in seconds. Defaults to `300`. |
| `Metadata` | `map[string]string` | Arbitrary key-value labels. |
| `Envs` | `map[string]string` | Environment variables injected into every command. |

### `sandbox.Connect(sandboxID string, opts SandboxOpts) (*Sandbox, error)`

Connect to an existing sandbox by ID, resuming it if paused.

```go
sb, err := sandbox.Connect("sandbox-id", sandbox.SandboxOpts{
    APIKey:  "your-api-key",
    BaseURL: "http://your-hermes-host:8080",
})
```

### `sandbox.NewFromConfig(cfg ConnectionConfig) *Sandbox`

Construct a `Sandbox` directly from a `ConnectionConfig`, bypassing the management API. Useful for local development and testing.

```go
sb := sandbox.NewFromConfig(sandbox.ConnectionConfig{
    SandboxID:   "local-sandbox",
    EnvdURL:     "http://localhost:8080/api/v1/sandboxes/local-sandbox/exec",
    AccessToken: "",
    APIKey:      "test-key",
    BaseURL:     "http://localhost:8080",
})
```

### `(*Sandbox) Kill() error`

Terminate the sandbox immediately.

### `(*Sandbox) SetTimeout(seconds int) error`

Update the sandbox lifetime.

### `(*Sandbox) GetInfo() (*SandboxInfo, error)`

Fetch current metadata for the sandbox.

```go
info, err := sb.GetInfo()
fmt.Println(info.State) // "running", "paused", ...
```

### `(*Sandbox) IsRunning() bool`

Return `true` if the sandbox state is `"running"` or `"active"`.

### `(*Sandbox) GetHost(port int) string`

Return the proxy hostname for a port inside the sandbox.

```go
host := sb.GetHost(3000) // "3000-<sandboxID>.<domain>"
```

### `(*Sandbox) GetMetrics() (*SandboxMetrics, error)`

Fetch current CPU and memory utilization.

```go
m, err := sb.GetMetrics()
fmt.Printf("CPU %.1f%%  Mem %.0f MiB\n", m.CPUUsedPct, m.MemUsedMiB)
```

### `(*Sandbox) ResizeDisk(sizeMB int) error`

Expand the sandbox disk. Atlas performs an in-place PVC resize — the sandbox does not restart.

```go
err := sb.ResizeDisk(20 * 1024) // 20 GiB
```

### `(*Sandbox) DownloadURL(path string, opts ...SandboxUrlOpts) (string, error)`

Return a short-lived signed URL for downloading a file directly from the sandbox.

### `(*Sandbox) UploadURL(path string, opts ...SandboxUrlOpts) (string, error)`

Return a short-lived signed URL for uploading a file directly into the sandbox.

---

## Static helpers

These functions operate without a `Sandbox` instance.

| Function | Description |
|---|---|
| `sandbox.List(opts)` | Return all sandboxes as `[]SandboxInfo`. |
| `sandbox.ListPage(opts)` | Return one page of sandboxes (`*SandboxPage`). |
| `sandbox.KillSandbox(id, opts)` | Kill a sandbox by ID. |
| `sandbox.GetSandboxInfo(id, opts)` | Fetch metadata by ID. |
| `sandbox.SetSandboxTimeout(id, secs, opts)` | Update timeout by ID. |
| `sandbox.GetSandboxMetrics(id, opts)` | Fetch metrics by ID. |
| `sandbox.ResizeSandboxDisk(id, sizeMB, opts)` | Resize disk by ID. |

---

## Commands

`sb.Commands` exposes all process-management operations.

### `(*Commands) Run(cmd string, opts ...RunOpts) (*CommandResult, error)`

Run a command and block until it finishes.

```go
result, err := sb.Commands.Run("npm install", sandbox.RunOpts{
    WorkingDir: "/app",
    TimeoutMs:  60_000,
    Envs:       map[string]string{"NODE_ENV": "production"},
})
fmt.Printf("exit=%d stdout=%s\n", result.ExitCode, result.Stdout)
```

`RunOpts` fields:

| Field | Type | Description |
|---|---|---|
| `WorkingDir` | `string` | Working directory inside the sandbox. |
| `TimeoutMs` | `uint64` | Kill the process after this many milliseconds. `0` = no timeout. |
| `Envs` | `map[string]string` | Additional environment variables. |
| `User` | `string` | Run as this Unix user (not yet enforced by nano-executor). |
| `OnStdout` | `func(string)` | Called for each stdout chunk. |
| `OnStderr` | `func(string)` | Called for each stderr chunk. |

### `(*Commands) RunBackground(cmd string, opts ...RunOpts) (*CommandHandle, error)`

Start a command in the background and return a handle immediately.

```go
handle, err := sb.Commands.RunBackground("node server.js", sandbox.RunOpts{
    WorkingDir: "/app",
})
// ... do other work ...
result, err := handle.Wait()
```

### `(*Commands) Connect(pid int, opts ...RunOpts) (*CommandHandle, error)`

Attach to a running process by PID and stream its output.

### `(*Commands) ConnectByTag(tag string, opts ...RunOpts) (*CommandHandle, error)`

Attach to a running process by its tag.

### `(*Commands) List(opts ...RequestOpts) ([]ProcessInfo, error)`

List all running processes in the sandbox.

### `(*Commands) Kill(pid int, opts ...RequestOpts) (bool, error)`

Send SIGKILL to a process by PID.

### `(*Commands) KillByTag(tag string, opts ...RequestOpts) (bool, error)`

Send SIGKILL to a process by tag.

### `(*Commands) SendSignal(pid int, signal Signal, opts ...RequestOpts) error`

Send an arbitrary signal (`SIGTERM`, `SIGINT`, etc.) to a process.

### `(*Commands) SendStdin(pid int, data string, opts ...RequestOpts) error`

Write data to a process's stdin.

### `(*Commands) CloseStdin(pid int, opts ...RequestOpts) error`

Close a process's stdin (EOF).

---

## CommandHandle

Returned by `RunBackground` and `Connect`.

| Method | Description |
|---|---|
| `PID() int` | Process ID inside the sandbox. |
| `Wait() (*CommandResult, error)` | Block until the process finishes. Returns a non-nil error if exit code ≠ 0. |
| `Kill() (bool, error)` | Send SIGKILL. |
| `SendStdin(data string) error` | Write to the process stdin. |
| `Disconnect()` | Detach from the output stream without killing the process. |

`CommandResult` fields:

| Field | Type | Description |
|---|---|---|
| `Stdout` | `string` | Full standard output. |
| `Stderr` | `string` | Full standard error. |
| `ExitCode` | `int` | Process exit code. |

---

## Filesystem

`sb.Files` exposes all filesystem operations.

### Read

```go
data, err := sb.Files.Read("/app/config.json")
text, err := sb.Files.ReadText("/app/config.json")
rc, err  := sb.Files.ReadStream("/app/large.csv") // io.ReadCloser
```

### Write

```go
info, err := sb.Files.Write("/app/out.bin", data)
info, err := sb.Files.WriteText("/app/config.json", `{"port":8080}`)
infos, err := sb.Files.WriteFiles([]sandbox.WriteEntry{
    {Path: "/app/run.sh", Content: scriptBytes, Mode: 0o755},
})
```

### Directory operations

```go
entries, err := sb.Files.List("/app")          // list directory
ok, err      := sb.Files.MakeDir("/app/logs")  // create directory
exists, err  := sb.Files.Exists("/app/config.json")
info, err    := sb.Files.GetInfo("/app/config.json")
```

`EntryInfo` fields: `Name`, `Path`, `Type` (`"file"` / `"dir"` / `"symlink"`), `Size`, `ModifiedAt`, `SymlinkTarget`.

### Mutation

```go
err := sb.Files.Edit("/app/config.json", `"port": 3000`, `"port": 8080`)
err  = sb.Files.Remove("/app/old.log")
info, err := sb.Files.Rename("/app/old.txt", "/app/new.txt")
```

### Watch

```go
handle, err := sb.Files.WatchDir("/app", func(ev sandbox.FilesystemEvent) {
    fmt.Printf("%s %s\n", ev.Operation, ev.Path)
})
defer handle.Stop()
```

---

## PTY

`sb.Pty` provides interactive terminal sessions.

```go
handle, err := sb.Pty.Create(sandbox.PtySize{Rows: 24, Cols: 80})
defer sb.Pty.Kill(handle.PID())

// Resize terminal
err = sb.Pty.Resize(handle.PID(), sandbox.PtySize{Rows: 40, Cols: 200})

// Send input
err = sb.Pty.SendInput(handle.PID(), "ls -la\n")

// Read output
result, err := handle.Wait()
fmt.Println(result.Stdout)
```

---

## Examples

| File | Description |
|---|---|
| [`examples/quickstart/main.go`](examples/quickstart/main.go) | Create sandbox, run commands |
| [`examples/background_commands/main.go`](examples/background_commands/main.go) | Background processes, wait, kill |
| [`examples/filesystem/main.go`](examples/filesystem/main.go) | Read, write, list, watch files |
| [`examples/pty/main.go`](examples/pty/main.go) | PTY create, resize, input |
| [`examples/sandbox_management/main.go`](examples/sandbox_management/main.go) | Lifecycle, metrics, disk resize |

```bash
go run examples/quickstart/main.go
```

## License

MIT — see [LICENSE](LICENSE).
