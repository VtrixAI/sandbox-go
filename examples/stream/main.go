// stream: 流式执行长命令，实时打印 stdout/stderr
package main

import (
	"context"
	"fmt"
	"log"

	sandbox "github.com/seaagent/hermes-sdk/src"
)

func main() {
	client := sandbox.NewClient(sandbox.ClientOptions{
		BaseURL:   "http://localhost:8080",
		Token:     "your-token",
		ServiceID: "seaclaw",
	})

	ctx := context.Background()

	sb, err := client.Create(ctx, sandbox.CreateOptions{UserID: "user-123"})
	if err != nil {
		log.Fatal(err)
	}
	defer sb.Close()

	fmt.Println("Streaming output:")

	events, resultCh, errCh := sb.ExecuteStream(ctx, `
		for i in $(seq 1 5); do
			echo "stdout line $i"
			echo "stderr line $i" >&2
			sleep 0.2
		done
	`, nil)

	// 实时消费事件
	for ev := range events {
		switch ev.Type {
		case "start":
			fmt.Println("[start]")
		case "stdout":
			fmt.Printf("[stdout] %s\n", ev.Data)
		case "stderr":
			fmt.Printf("[stderr] %s\n", ev.Data)
		case "done":
			fmt.Println("[done]")
		}
	}

	// 等待最终结果
	select {
	case result := <-resultCh:
		fmt.Printf("exit_code=%d, total output len=%d\n", result.ExitCode, len(result.Output))
	case err := <-errCh:
		log.Fatal(err)
	}
}
