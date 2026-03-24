// attach: 复用已有沙箱（不重新创建）
// Usage: go run main.go <sandbox-id>
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	sandbox "github.com/VtrixAI/sandbox-go/src"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintln(os.Stderr, "Usage: go run main.go <sandbox-id>")
		os.Exit(1)
	}
	sandboxID := os.Args[1]

	client := sandbox.NewClient(sandbox.ClientOptions{
		BaseURL:   "http://localhost:8080",
		Token:     "your-token",
		ServiceID: "seaclaw",
	})

	ctx := context.Background()

	fmt.Printf("Attaching to existing sandbox: %s\n", sandboxID)
	sb, err := client.Attach(ctx, sandboxID, "", "")
	if err != nil {
		log.Fatal(err)
	}
	defer sb.Close()

	fmt.Printf("Attached: %s (status=%s)\n", sb.Info.ID, sb.Info.Status)

	result, err := sb.RunCommand(ctx, "hostname && date", nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Print(result.Output)
}
