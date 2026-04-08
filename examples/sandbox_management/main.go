// sandbox_management demonstrates sandbox lifecycle management:
// Connect, List, SetTimeout, GetInfo, IsRunning, GetHost, GetMetrics.
//
// Run:
//
//	SANDBOX_API_KEY=your-key SANDBOX_BASE_URL=https://api.sandbox.vtrix.ai go run ./examples/sandbox_management
package main

import (
	"fmt"
	"log"
	"os"

	sandbox "github.com/VtrixAI/sandbox-go"
)

func main() {
	opts := sandbox.SandboxOpts{
		APIKey:  os.Getenv("SANDBOX_API_KEY"),
		BaseURL: os.Getenv("SANDBOX_BASE_URL"),
		Timeout: 120,
	}

	// ---------------------------------------------------------------------------
	// 1. List existing sandboxes
	// ---------------------------------------------------------------------------
	infos, err := sandbox.List(opts)
	if err != nil {
		log.Fatalf("List: %v", err)
	}
	fmt.Printf("existing sandboxes: %d\n", len(infos))

	// ---------------------------------------------------------------------------
	// 2. Create a sandbox
	// ---------------------------------------------------------------------------
	sb, err := sandbox.Create(opts)
	if err != nil {
		log.Fatalf("Create: %v", err)
	}
	fmt.Printf("created: %s\n", sb.SandboxID)
	defer sb.Kill() //nolint:errcheck

	// ---------------------------------------------------------------------------
	// 3. GetInfo / IsRunning
	// ---------------------------------------------------------------------------
	info, err := sb.GetInfo()
	if err != nil {
		log.Fatalf("GetInfo: %v", err)
	}
	fmt.Printf("state: %s  running: %v\n", info.State, sb.IsRunning())

	// ---------------------------------------------------------------------------
	// 4. SetTimeout — extend sandbox lifetime
	// ---------------------------------------------------------------------------
	if err := sb.SetTimeout(300); err != nil {
		log.Printf("SetTimeout: %v (may not be supported)", err)
	} else {
		fmt.Println("timeout extended to 300s")
	}

	// ---------------------------------------------------------------------------
	// 5. GetHost — proxy URL for a port inside the sandbox
	// ---------------------------------------------------------------------------
	host := sb.GetHost(8080)
	fmt.Printf("proxy host for port 8080: %s\n", host)

	// ---------------------------------------------------------------------------
	// 6. GetMetrics — current CPU / memory usage
	// ---------------------------------------------------------------------------
	metrics, err := sb.GetMetrics()
	if err != nil {
		log.Printf("GetMetrics: %v (may not be supported)", err)
	} else {
		fmt.Printf("cpu=%.2f%%  mem=%.2fMiB\n", metrics.CPUUsedPct, metrics.MemUsedMiB)
	}

	// ---------------------------------------------------------------------------
	// 7. Connect to an existing sandbox by ID
	// ---------------------------------------------------------------------------
	sb2, err := sandbox.Connect(sb.SandboxID, opts)
	if err != nil {
		log.Fatalf("Connect: %v", err)
	}
	result, err := sb2.Commands.Run("echo 'reconnected'")
	if err != nil {
		log.Fatalf("Run after Connect: %v", err)
	}
	fmt.Printf("reconnected stdout: %s", result.Stdout)

	fmt.Println("done.")
}
