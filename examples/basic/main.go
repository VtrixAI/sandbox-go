// basic: 创建沙箱 → 执行命令 → detached 命令 → 关闭
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

	// 一次性执行（阻塞直到命令结束）
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

	// detached 命令：立即返回 Command，稍后 Wait()
	cmd, err := sb.RunCommandDetached(ctx, "for i in $(seq 1 3); do echo bg_$i; sleep 0.3; done", nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Detached cmdId=%s  pid=%d\n", cmd.CmdID, cmd.PID)

	// Wait() 等待结束，获取 CommandFinished
	finished, err := cmd.Wait(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Detached done: exit_code=%d\n", finished.ExitCode)
	fmt.Printf("Detached output:\n%s\n", finished.Output)

	// GetCommand：通过 cmdId 重新拿到 Command 对象
	cmd2 := sb.GetCommand(cmd.CmdID)
	stdoutText, err := cmd2.Stdout(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Re-fetched stdout: %s\n", stdoutText)

	// Kill 示例（先启动一个 sleep，再 kill 它）
	sleeper, err := sb.RunCommandDetached(ctx, "sleep 60", nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	if err := sb.Kill(ctx, sleeper.CmdID, "SIGKILL"); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Killed sleep, cmdId=%s\n", sleeper.CmdID)
}
