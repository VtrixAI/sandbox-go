// basic: 创建沙箱 → 执行命令 → 关闭
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
	result, err := sb.Execute(ctx, "echo hello && uname -a", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("exit_code=%d\noutput:\n%s\n", result.ExitCode, result.Output)

	// 带选项执行
	result2, err := sb.Execute(ctx, "pwd", &sandbox.ExecOptions{
		WorkingDir: "/tmp",
		TimeoutSec: 10,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("pwd: %s\n", result2.Output)
}
