// Package tests contains end-to-end tests for the Go SDK.
//
// # Architecture
//
//	Test → SDK → http://localhost:8080/api/v1/sandboxes/<id>/exec/<rpc>
//	             ↓  (hermes strips /exec prefix, proxies to atlas.local_ip:9000)
//	             http://127.0.0.1:9000/<rpc>  (nano-executor)
//
// # How to run
//
//	go test ./tests/ -v -timeout 120s
//
// TestMain automatically starts nano-executor and hermes as subprocesses.
// Set SKIP_START=1 if they are already running.
//
// # Environment variables
//
//	NANO_EXECUTOR_BIN  – path to nano-executor binary
//	                     (default: <repo>/../nano-executor/target/release/nano-executor)
//	HERMES_DIR         – path to hermes repo root (default: auto-detected)
//	SKIP_START         – set to "1" to skip starting subprocesses
//	ATLAS_BASE_URL     – set to enable sandbox management tests (Create/Kill/List)
//	SANDBOX_API_KEY    – API key for management tests (default: "test-key")
package tests

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"

	sandbox "github.com/VtrixAI/sandbox-go"
)

// ---------------------------------------------------------------------------
// Constants
// ---------------------------------------------------------------------------

const (
	hermesAddr        = "http://localhost:8080"
	nanoAddr          = "http://localhost:9000"
	testSandboxID     = "e2e-test-sandbox"
	startupTimeout    = 45 * time.Second
	readyPollInterval = 300 * time.Millisecond
)

// ---------------------------------------------------------------------------
// Global state
// ---------------------------------------------------------------------------

var (
	// sb is the main test client: SDK → hermes → nano-executor
	sb *sandbox.Sandbox

	hermesCmd *exec.Cmd
	nanoCmd   *exec.Cmd
)

// ---------------------------------------------------------------------------
// TestMain
// ---------------------------------------------------------------------------

func TestMain(m *testing.M) {
	skipStart := os.Getenv("SKIP_START") == "1"

	if !skipStart {
		startNanoExecutor()
		startHermes()
	}

	waitReady(nanoAddr+"/health", "nano-executor")
	waitReady(hermesAddr+"/health", "hermes")

	// Build SDK client: EnvdURL routes RPC through hermes, which strips
	// the /api/v1/sandboxes/:id/exec prefix and forwards to 127.0.0.1:9000.
	envdURL := fmt.Sprintf("%s/api/v1/sandboxes/%s/exec", hermesAddr, testSandboxID)
	sb = sandbox.NewFromConfig(sandbox.ConnectionConfig{
		SandboxID:   testSandboxID,
		EnvdURL:     envdURL,
		AccessToken: "",
		APIKey:      "test-key",
		BaseURL:     hermesAddr,
		Timeout:     30 * time.Second,
	})

	code := m.Run()

	if !skipStart {
		stopAll()
	}

	os.Exit(code)
}

// ---------------------------------------------------------------------------
// Subprocess helpers
// ---------------------------------------------------------------------------

func repoRoot() string {
	if d := os.Getenv("HERMES_DIR"); d != "" {
		return d
	}
	// This file is at sdk/go/tests/; repo root is 3 levels up.
	abs, err := filepath.Abs("../../..")
	if err != nil {
		return "../../.."
	}
	return abs
}

func startNanoExecutor() {
	bin := os.Getenv("NANO_EXECUTOR_BIN")
	if bin == "" {
		bin = filepath.Join(repoRoot(), "..", "nano-executor", "target", "release", "nano-executor")
	}
	bin, _ = filepath.Abs(bin)

	if _, err := os.Stat(bin); err != nil {
		fmt.Fprintf(os.Stderr, "[e2e] SKIP: nano-executor binary not found at %s\n", bin)
		fmt.Fprintf(os.Stderr, "[e2e] Build it with: cd ../nano-executor && cargo build --release\n")
		os.Exit(1)
	}

	cmd := exec.Command(bin, "serve")
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[e2e] failed to start nano-executor: %v\n", err)
		os.Exit(1)
	}
	nanoCmd = cmd
	fmt.Printf("[e2e] started nano-executor pid=%d\n", cmd.Process.Pid)
}

func startHermes() {
	root := repoRoot()

	// Build the binary first so startup is fast and respects configs/ in root dir.
	bin := filepath.Join(os.TempDir(), "hermes-e2e-test")
	build := exec.Command("go", "build", "-o", bin, ".")
	build.Dir = root
	build.Stdout = os.Stderr
	build.Stderr = os.Stderr
	if err := build.Run(); err != nil {
		fmt.Fprintf(os.Stderr, "[e2e] failed to build hermes: %v\n", err)
		os.Exit(1)
	}

	cmd := exec.Command(bin)
	cmd.Dir = root // must match configs/ location so rate limits load from local.yaml
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = append(os.Environ(), "APP_ENV=local")
	if err := cmd.Start(); err != nil {
		fmt.Fprintf(os.Stderr, "[e2e] failed to start hermes: %v\n", err)
		os.Exit(1)
	}
	hermesCmd = cmd
	fmt.Printf("[e2e] started hermes pid=%d\n", cmd.Process.Pid)
}

func waitReady(url, name string) {
	deadline := time.Now().Add(startupTimeout)
	client := &http.Client{Timeout: 2 * time.Second}
	for time.Now().Before(deadline) {
		resp, err := client.Get(url) //nolint:noctx
		if err == nil {
			resp.Body.Close()
			if resp.StatusCode < 500 {
				fmt.Printf("[e2e] %s ready (HTTP %d)\n", name, resp.StatusCode)
				return
			}
		}
		time.Sleep(readyPollInterval)
	}
	fmt.Fprintf(os.Stderr, "[e2e] FATAL: %s not ready within %s\n", name, startupTimeout)
	stopAll()
	os.Exit(1)
}

func stopAll() {
	var wg sync.WaitGroup
	for _, cmd := range []*exec.Cmd{hermesCmd, nanoCmd} {
		if cmd != nil && cmd.Process != nil {
			wg.Add(1)
			go func(c *exec.Cmd) {
				defer wg.Done()
				_ = c.Process.Kill()
				_ = c.Wait()
			}(cmd)
		}
	}
	wg.Wait()
}

// ---------------------------------------------------------------------------
// Test helpers
// ---------------------------------------------------------------------------

func fatalf(t *testing.T, format string, args ...any) {
	t.Helper()
	t.Fatalf(format, args...)
}

func noErr(t *testing.T, err error, msg string) {
	t.Helper()
	if err != nil {
		fatalf(t, "%s: %v", msg, err)
	}
}
