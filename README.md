# sandbox-go

Go SDK for [Vtrix](https://github.com/VtrixAI) sandbox — JSON-RPC 2.0 over WebSocket.

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
        ServiceID: "your-service-id",
    })

    ctx := context.Background()

    sb, err := client.Create(ctx, sandbox.CreateOptions{
        UserID: "user-123",
        Spec:   &sandbox.Spec{CPU: "2", Memory: "4Gi"},
    })
    if err != nil {
        log.Fatal(err)
    }
    defer sb.Close()

    result, err := sb.Execute(ctx, "echo hello && uname -a", nil)
    if err != nil {
        log.Fatal(err)
    }
    fmt.Printf("exit_code=%d\n%s\n", result.ExitCode, result.Output)
}
```

## API

### Client

```go
client := sandbox.NewClient(sandbox.ClientOptions{
    BaseURL:    "http://host:8080", // Hermes gateway URL
    Token:      "...",              // Bearer token
    ServiceID:  "...",              // X-Service-ID header
    HTTPClient: nil,                // optional custom *http.Client
})

sb, err   := client.Create(ctx, opts)                   // create + poll + connect
sb, err    = client.Attach(ctx, id, token, serviceID)   // connect to existing sandbox
list, err  = client.List(ctx, opts)                     // list sandboxes
info, err  = client.Get(ctx, id)                        // get sandbox metadata
err        = client.Delete(ctx, id)                     // delete sandbox
```

### Execute

```go
// Blocking — waits for command to finish
result, err := sb.Execute(ctx, "command", &sandbox.ExecOptions{
    WorkingDir: "/tmp",
    TimeoutSec: 30,
    Env:        map[string]string{"FOO": "bar"},
    Sudo:       false,
    Stdin:      "",
})
// result.ExitCode, result.Output, result.CmdID

// Streaming — real-time stdout/stderr
eventCh, resultCh, errCh := sb.ExecuteStream(ctx, "command", nil)
for ev := range eventCh {
    // ev.Type: "start" | "stdout" | "stderr" | "done"
    // ev.Data
}
result := <-resultCh

// Detached — fire and forget, returns immediately
cmd, err := sb.ExecuteDetached(ctx, "long-running-command", nil)
// cmd.CmdID, cmd.PID

// Command methods
finished, err := cmd.Wait(ctx)                   // block until done
logCh, errCh  := cmd.Logs(ctx)                  // stream LogEvents
stdout, err    = cmd.Stdout(ctx)                 // collect stdout string
stderr, err    = cmd.Stderr(ctx)                 // collect stderr string
out, err       = cmd.CollectOutput(ctx, "both")  // "stdout"|"stderr"|"both"
err             = cmd.Kill(ctx, "SIGTERM")        // send signal

// Reconnect to a known command
cmd = sb.GetCommand(cmdID)
```

### File Operations

```go
// Read / Write / Edit
result, err := sb.Read(ctx, "/path/to/file")
result, err  = sb.Write(ctx, "/path/to/file", "content")
result, err  = sb.Edit(ctx, "/path/to/file", "old text", "new text")

// Binary files
err = sb.WriteFiles(ctx, []sandbox.WriteFileEntry{
    {Path: "/tmp/data.bin", Content: []byte{...}},
})
data, err := sb.ReadToBuffer(ctx, "/path/to/file") // []byte

// Directory
err = sb.MkDir(ctx, "/path/to/dir")

// List / Stat / Exists
entries, err := sb.ListFiles(ctx, "/path")
info, err     = sb.Stat(ctx, "/path/to/file")
exists, err   = sb.Exists(ctx, "/path/to/file")

// Upload / Download
err = sb.UploadFile(ctx, "local.txt", "/sandbox/path.txt", nil)
abs, err = sb.DownloadFile(ctx, "/sandbox/path.txt", "local.txt", nil)
downloaded, err = sb.DownloadFiles(ctx, []sandbox.DownloadEntry{
    {SandboxPath: "/a.txt", LocalPath: "a.txt"},
}, nil)

// Stream large files
chunkCh, resultCh, errCh := sb.ReadStream(ctx, "/large/file", 65536)
for chunk := range chunkCh {
    // chunk.Data is base64-encoded bytes
}

// URL for exposed ports
url := sb.Domain(8080) // "https://8080-<preview-host>"
```

### Lifecycle

```go
err = sb.Refresh(ctx, client)
err = sb.Stop(ctx, client, &sandbox.StopOptions{Blocking: true})
err = sb.Start(ctx, client)
err = sb.Restart(ctx, client)
err = sb.Extend(ctx, client, 12)          // extend TTL by 12h
err = sb.ExtendTimeout(ctx, client, 12)   // extend + refresh
err = sb.Update(ctx, client, sandbox.UpdateOptions{...})
err = sb.Configure(ctx, client)           // apply config to pod
err = sb.Delete(ctx, client)

status   := sb.Status()    // cached status string
expireAt := sb.ExpireAt()  // cached expiry (RFC3339)
```

## Examples

See the [`examples/`](examples/) directory:

| File | Description |
|------|-------------|
| `examples/basic/main.go` | Create sandbox, run commands |
| `examples/stream/main.go` | Real-time streaming output |
| `examples/files/main.go` | File read/write/edit/upload/download |
| `examples/lifecycle/main.go` | Stop/start/extend/update/delete |
