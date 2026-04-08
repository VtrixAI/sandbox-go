// pty demonstrates PTY (pseudo-terminal) creation, resize, input, and wait.
//
// Run:
//
//	SANDBOX_API_KEY=your-key SANDBOX_BASE_URL=https://api.sandbox.vtrix.ai go run ./examples/pty
package main

import (
	"fmt"
	"log"
	"os"
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
	// 1. Create a PTY (bash shell in a pseudo-terminal)
	// ---------------------------------------------------------------------------
	handle, err := sb.Pty.Create(sandbox.PtySize{Rows: 24, Cols: 80})
	if err != nil {
		log.Fatalf("Pty.Create: %v", err)
	}
	fmt.Printf("PTY pid=%d\n", handle.PID())

	// ---------------------------------------------------------------------------
	// 1b. Create a PTY with custom shell, env vars, and working directory
	// ---------------------------------------------------------------------------
	handle2, err := sb.Pty.Create(
		sandbox.PtySize{Rows: 24, Cols: 80},
		sandbox.PtyCreateOpts{
			Cmd:  "/bin/sh",
			Envs: map[string]string{"TERM": "xterm-256color"},
			Cwd:  "/tmp",
		},
	)
	if err != nil {
		log.Fatalf("Pty.Create with opts: %v", err)
	}
	fmt.Printf("custom PTY pid=%d\n", handle2.PID())
	time.Sleep(100 * time.Millisecond)
	sb.Pty.Kill(handle2.PID()) //nolint:errcheck

	// ---------------------------------------------------------------------------
	// 2. Resize the terminal
	// ---------------------------------------------------------------------------
	if err := sb.Pty.Resize(handle.PID(), sandbox.PtySize{Rows: 40, Cols: 200}); err != nil {
		log.Fatalf("Pty.Resize: %v", err)
	}
	fmt.Println("resized to 40x200")

	// ---------------------------------------------------------------------------
	// 3. Send input (like typing in a terminal)
	// ---------------------------------------------------------------------------
	time.Sleep(300 * time.Millisecond)

	if err := sb.Pty.SendInput(handle.PID(), []byte("echo 'hello from pty'\n")); err != nil {
		log.Fatalf("Pty.SendInput: %v", err)
	}
	time.Sleep(300 * time.Millisecond)

	// ---------------------------------------------------------------------------
	// 4. Exit the shell and wait for the PTY to close
	// ---------------------------------------------------------------------------
	if err := sb.Pty.SendInput(handle.PID(), []byte("exit\n")); err != nil {
		log.Fatalf("Pty.SendInput exit: %v", err)
	}

	done := make(chan struct{})
	go func() {
		defer close(done)
		result, _ := handle.Wait()
		if result != nil {
			fmt.Printf("\nPTY exited with code %d\n", result.ExitCode)
		}
	}()

	select {
	case <-done:
	case <-time.After(10 * time.Second):
		// Timed out waiting — kill the PTY manually
		sb.Pty.Kill(handle.PID()) //nolint:errcheck
		fmt.Println("PTY killed after timeout")
	}

	fmt.Println("done.")
}
