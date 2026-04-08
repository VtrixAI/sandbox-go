// quickstart demonstrates the most common usage patterns of the sandbox-go SDK.
//
// Run:
//
//	SANDBOX_API_KEY=your-key SANDBOX_BASE_URL=https://api.sandbox.vtrix.ai go run ./examples/quickstart
package main

import (
	"fmt"
	"log"
	"os"

	sandbox "github.com/VtrixAI/sandbox-go"
)

func main() {
	// ---------------------------------------------------------------------------
	// 1. Create a sandbox with metadata and custom env vars
	// ---------------------------------------------------------------------------
	sb, err := sandbox.Create(sandbox.SandboxOpts{
		APIKey:   os.Getenv("SANDBOX_API_KEY"),
		BaseURL:  os.Getenv("SANDBOX_BASE_URL"), // e.g. https://api.sandbox.vtrix.ai
		Template: "base",
		Timeout:  300, // seconds until the sandbox is automatically destroyed
		Metadata: map[string]string{"owner": "quickstart-example"},
		Envs:     map[string]string{"APP_ENV": "demo"},
	})
	if err != nil {
		log.Fatalf("Create: %v", err)
	}
	defer sb.Kill() //nolint:errcheck

	fmt.Printf("Sandbox created: %s\n", sb.SandboxID)

	// ---------------------------------------------------------------------------
	// 2. Run a command and get its output
	// ---------------------------------------------------------------------------
	result, err := sb.Commands.Run("echo 'hello from sandbox'")
	if err != nil {
		log.Fatalf("Run: %v", err)
	}
	fmt.Printf("stdout: %s", result.Stdout)

	// ---------------------------------------------------------------------------
	// 3. Run a command with env vars and working directory
	// ---------------------------------------------------------------------------
	timeout := 10
	result, err = sb.Commands.Run("echo $MY_VAR && pwd", sandbox.RunOpts{
		Envs:    map[string]string{"MY_VAR": "hello"},
		Cwd:     "/tmp",
		Timeout: &timeout,
	})
	if err != nil {
		log.Fatalf("Run with opts: %v", err)
	}
	fmt.Printf("env+cwd: %s", result.Stdout)

	// ---------------------------------------------------------------------------
	// 4. Write and read a file
	// ---------------------------------------------------------------------------
	_, err = sb.Files.WriteText("/tmp/hello.txt", "hello, world!")
	if err != nil {
		log.Fatalf("WriteText: %v", err)
	}

	// ReadText — UTF-8 string
	content, err := sb.Files.ReadText("/tmp/hello.txt")
	if err != nil {
		log.Fatalf("ReadText: %v", err)
	}
	fmt.Printf("file content: %s\n", content)

	// Read — raw bytes
	raw, err := sb.Files.Read("/tmp/hello.txt")
	if err != nil {
		log.Fatalf("Read bytes: %v", err)
	}
	fmt.Printf("raw bytes length: %d\n", len(raw))

	// ---------------------------------------------------------------------------
	// 5. NewFromConfig — construct a Sandbox directly from connection parameters
	//    (bypasses the management API; useful for testing and local development)
	// ---------------------------------------------------------------------------
	sb2 := sandbox.NewFromConfig(sandbox.ConnectionConfig{
		SandboxID: sb.SandboxID,
		EnvdURL:   os.Getenv("SANDBOX_BASE_URL") + "/api/v1/sandboxes/" + sb.SandboxID + "/exec",
		APIKey:    os.Getenv("SANDBOX_API_KEY"),
		BaseURL:   os.Getenv("SANDBOX_BASE_URL"),
	})
	r2, err := sb2.Commands.Run("echo 'from NewFromConfig'")
	if err != nil {
		log.Printf("NewFromConfig run: %v", err)
	} else {
		fmt.Printf("NewFromConfig: %s", r2.Stdout)
	}

	// ---------------------------------------------------------------------------
	// 6. Handle non-zero exit codes
	// ---------------------------------------------------------------------------
	_, err = sb.Commands.Run("exit 1")
	if err != nil {
		if exitErr, ok := err.(*sandbox.CommandExitError); ok {
			fmt.Printf("command failed with exit code %d\n", exitErr.ExitCode)
		} else {
			log.Fatalf("unexpected error: %v", err)
		}
	}

	fmt.Println("done.")
}
