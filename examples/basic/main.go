// basic: 创建沙箱 → 执行命令 → 关闭
package main

import (
	"context"
	"fmt"
	"log"

	sandbox "github.com/VtrixAI/sandbox-go/src"
)

func main() {
	client := sandbox.NewClient(sandbox.ClientOptions{
		BaseURL:   "http://localhost:8080",
		Token:     "your-token",
		ServiceID: "seaclaw",
	})

	ctx := context.Background()

	fmt.Println("Creating sandbox...")
	sb, err := client.Create(ctx, sandbox.CreateOptions{
		UserID: "user-123",
		Spec:   &sandbox.Spec{CPU: "2", Memory: "4Gi"},
	})
	if err != nil {
		log.Fatal(err)
	}
	defer sb.Close()

	fmt.Printf("Sandbox ready: %s (status=%s)\n", sb.Info.ID, sb.Info.Status)

	// 执行命令
	result, err := sb.RunCommand(ctx, "echo hello && uname -a", nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("exit_code=%d\noutput:\n%s\n", result.ExitCode, result.Output)

	// 带 args 和选项
	result2, err := sb.RunCommand(ctx, "ls", []string{"-la", "/tmp"}, &sandbox.RunOptions{
		WorkingDir: "/tmp",
		TimeoutSec: 10,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("ls -la /tmp:\n%s\n", result2.Output)
}
