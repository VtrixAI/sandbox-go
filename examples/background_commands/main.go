// background_commands demonstrates running long-lived processes, streaming
// output, sending stdin, and signals.
//
// Run:
//
//	SANDBOX_API_KEY=your-key SANDBOX_BASE_URL=https://api.sandbox.vtrix.ai go run ./examples/background_commands
package main

import (
	"errors"
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
	// 1. Run a background process, stream its output, then wait for it
	// ---------------------------------------------------------------------------
	handle, err := sb.Commands.RunBackground("for i in 1 2 3; do echo line$i; sleep 0.3; done", sandbox.RunOpts{
		OnStdout: func(line string) { fmt.Printf("[stdout] %s", line) },
		OnStderr: func(line string) { fmt.Printf("[stderr] %s", line) },
	})
	if err != nil {
		log.Fatalf("RunBackground: %v", err)
	}
	fmt.Printf("started pid=%d\n", handle.PID())

	result, err := handle.Wait()
	if err != nil {
		var exitErr *sandbox.CommandExitError
		if errors.As(err, &exitErr) {
			fmt.Printf("exited with code %d\n", exitErr.ExitCode)
		} else {
			log.Fatalf("Wait: %v", err)
		}
	} else {
		fmt.Printf("finished. exit_code=%d\n", result.ExitCode)
	}

	// ---------------------------------------------------------------------------
	// 2. Send stdin to a running process
	// ---------------------------------------------------------------------------
	cat, err := sb.Commands.RunBackground("cat")
	if err != nil {
		log.Fatalf("RunBackground cat: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	_ = sb.Commands.SendStdin(cat.PID(), "hello stdin\n")
	_ = sb.Commands.CloseStdin(cat.PID())

	catResult, _ := cat.Wait()
	if catResult != nil {
		fmt.Printf("cat echoed: %s", catResult.Stdout)
	}

	// ---------------------------------------------------------------------------
	// 3. Send a signal — demonstrate multiple signal constants
	// ---------------------------------------------------------------------------
	sleeper, err := sb.Commands.RunBackground("sleep 60")
	if err != nil {
		log.Fatalf("RunBackground sleep: %v", err)
	}

	time.Sleep(200 * time.Millisecond)
	// SignalTerm — graceful termination
	if err := sb.Commands.SendSignal(sleeper.PID(), sandbox.SignalTerm); err != nil {
		log.Fatalf("SendSignal SIGTERM: %v", err)
	}
	_, _ = sleeper.Wait()
	fmt.Println("sleep process terminated via SIGTERM")

	// SignalInt (SIGINT) — interrupt, e.g. Ctrl-C
	sleeper2, _ := sb.Commands.RunBackground("sleep 60")
	time.Sleep(100 * time.Millisecond)
	_ = sb.Commands.SendSignal(sleeper2.PID(), sandbox.SignalInt)
	_, _ = sleeper2.Wait()
	fmt.Println("sleep process interrupted via SIGINT")

	// SignalHup (SIGHUP) — hangup / config reload
	sleeper3, _ := sb.Commands.RunBackground("sleep 60")
	time.Sleep(100 * time.Millisecond)
	_ = sb.Commands.SendSignal(sleeper3.PID(), sandbox.SignalHup)
	_, _ = sleeper3.Wait()
	fmt.Println("sleep process received SIGHUP")

	// ---------------------------------------------------------------------------
	// 4. Disconnect from a process (it keeps running)
	// ---------------------------------------------------------------------------
	bg, _ := sb.Commands.RunBackground("sleep 30")
	pid := bg.PID()
	bg.Disconnect() // detach — process stays alive

	time.Sleep(200 * time.Millisecond)
	ok, _ := sb.Commands.Kill(pid)
	fmt.Printf("kill after disconnect: %v\n", ok)

	// ---------------------------------------------------------------------------
	// 5. RunBackground with a Tag (for by-tag operations on Linux)
	// ---------------------------------------------------------------------------
	tagged, err := sb.Commands.RunBackground("sleep 5", sandbox.RunOpts{Tag: "bg-example"})
	if err != nil {
		log.Fatalf("RunBackground with tag: %v", err)
	}
	fmt.Printf("tagged process pid=%d tag=bg-example\n", tagged.PID())
	tagged.Kill() //nolint:errcheck

	fmt.Println("done.")
}
