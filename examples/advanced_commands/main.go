// advanced_commands demonstrates process listing, Connect, and by-tag operations.
//
// Run:
//
//	SANDBOX_API_KEY=your-key SANDBOX_BASE_URL=https://api.sandbox.vtrix.ai go run ./examples/advanced_commands
//
// Note: by-tag operations and process listing rely on /proc and only work on Linux.
package main

import (
	"errors"
	"fmt"
	"log"
	"os"
	"runtime"
	"time"

	sandbox "github.com/VtrixAI/sandbox-go"
)

func main() {
	sb, err := sandbox.Create(sandbox.SandboxOpts{
		APIKey:  os.Getenv("SANDBOX_API_KEY"),
		BaseURL: os.Getenv("SANDBOX_BASE_URL"),
	})
	if err != nil {
		log.Fatalf("Create: %v", err)
	}
	defer sb.Kill() //nolint:errcheck

	// ---------------------------------------------------------------------------
	// 1. List running processes
	// ---------------------------------------------------------------------------
	handle, err := sb.Commands.RunBackground("sleep 30")
	if err != nil {
		log.Fatalf("RunBackground: %v", err)
	}
	defer handle.Kill() //nolint:errcheck

	time.Sleep(200 * time.Millisecond)

	procs, err := sb.Commands.List()
	if err != nil {
		log.Fatalf("List: %v", err)
	}
	fmt.Printf("running processes: %d\n", len(procs))
	for _, p := range procs {
		fmt.Printf("  pid=%d cmd=%s\n", p.PID, p.Cmd)
	}

	// ---------------------------------------------------------------------------
	// 2. Connect to a running process by PID
	// ---------------------------------------------------------------------------
	connected, err := sb.Commands.Connect(handle.PID())
	if err != nil {
		log.Fatalf("Connect: %v", err)
	}
	fmt.Printf("connected to pid=%d\n", connected.PID())
	connected.Disconnect() // detach without killing

	// ---------------------------------------------------------------------------
	// 3. By-tag operations (Linux only — /proc required)
	// ---------------------------------------------------------------------------
	if runtime.GOOS != "linux" {
		fmt.Println("skipping by-tag operations (non-Linux)")
		fmt.Println("done.")
		return
	}

	const tag = "example-tag"
	tagged, err := sb.Commands.RunBackground("sleep 60", sandbox.RunOpts{Tag: tag})
	if err != nil {
		log.Fatalf("RunBackground with tag: %v", err)
	}

	time.Sleep(300 * time.Millisecond)

	// ConnectByTag
	conn, err := sb.Commands.ConnectByTag(tag)
	if err != nil {
		log.Fatalf("ConnectByTag: %v", err)
	}
	fmt.Printf("ConnectByTag pid=%d\n", conn.PID())
	conn.Disconnect()

	// SendStdinByTag
	if err := sb.Commands.SendStdinByTag(tag, "data\n"); err != nil {
		log.Fatalf("SendStdinByTag: %v", err)
	}
	fmt.Println("SendStdinByTag sent")

	// KillByTag
	ok, err := sb.Commands.KillByTag(tag)
	if err != nil {
		log.Fatalf("KillByTag: %v", err)
	}
	fmt.Printf("KillByTag: %v\n", ok)

	// Wait confirms the process is gone
	_, err = tagged.Wait()
	if err != nil {
		var exitErr *sandbox.CommandExitError
		if errors.As(err, &exitErr) {
			fmt.Printf("tagged process exited with code %d\n", exitErr.ExitCode)
		}
	}

	// ---------------------------------------------------------------------------
	// 4. WatchDir — watch a directory for filesystem events
	// ---------------------------------------------------------------------------
	watchPath := "/tmp/watch_example"
	if _, err := sb.Files.MakeDir(watchPath); err != nil {
		log.Fatalf("MakeDir: %v", err)
	}

	var events []sandbox.FilesystemEvent
	watcher, err := sb.Files.WatchDir(watchPath, func(e sandbox.FilesystemEvent) {
		events = append(events, e)
	})
	if err != nil {
		fmt.Printf("WatchDir not supported in this environment: %v\n", err)
	} else {
		time.Sleep(200 * time.Millisecond)
		sb.Files.WriteText(watchPath+"/trigger.txt", "hello") //nolint:errcheck
		time.Sleep(500 * time.Millisecond)

		watcher.Stop()
		fmt.Printf("WatchDir events received: %d\n", len(events))
		for _, e := range events {
			fmt.Printf("  %s %s\n", e.Type, e.Name)
		}
	}

	fmt.Println("done.")
}
