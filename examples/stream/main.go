// stream: 流式执行 + ExecLogs 日志回放
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	sandbox "github.com/VtrixAI/sandbox-go/src"
)

func main() {
	client := sandbox.NewClient(sandbox.ClientOptions{
		BaseURL:   "http://localhost:8080",
		Token:     "your-token",
		ProjectID: "seaclaw",
	})

	ctx := context.Background()

	sb, err := client.Create(ctx, sandbox.CreateOptions{UserID: "user-123"})
	if err != nil {
		log.Fatal(err)
	}
	defer sb.Close()

	fmt.Printf("Sandbox: %s\n", sb.Info.ID)

	script := `
		for i in $(seq 1 5); do
			echo "stdout line $i"
			echo "stderr line $i" >&2
			sleep 0.2
		done
	`

	// ── 实时流式输出 ────────────────────────────────────────
	fmt.Println("Streaming:")
	events, resultCh, errCh := sb.RunCommandStream(ctx, script, nil, nil)
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
	select {
	case result := <-resultCh:
		fmt.Printf("exit_code=%d\n", result.ExitCode)
	case err := <-errCh:
		log.Fatal(err)
	}

	// ── detached 命令 + stdout/stderr writer ────────────────
	detached, err := sb.RunCommandDetached(ctx, script, nil, &sandbox.RunOptions{
		Stdout: os.Stdout,
		Stderr: os.Stderr,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nDetached cmdId=%s\n", detached.CmdID)
	finished, err := detached.Wait(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Wait done: exit_code=%d\n", finished.ExitCode)

	// ── ExecLogs 日志回放（已完成命令）────────────────────
	fmt.Println("\nReplaying logs via ExecLogs:")
	logCh, _, replayErrCh := sb.ExecLogs(ctx, detached.CmdID)
	for ev := range logCh {
		switch ev.Type {
		case "stdout":
			fmt.Printf("  [replay stdout] %s\n", ev.Data)
		case "stderr":
			fmt.Printf("  [replay stderr] %s\n", ev.Data)
		}
	}
	if err := <-replayErrCh; err != nil {
		log.Fatal(err)
	}

	// ── Command.Logs() / Stdout() / Stderr() ────────────────
	cmd2, err := sb.RunCommandDetached(ctx, `echo "out_line" && echo "err_line" >&2`, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("\nCommand.Logs():")
	logsCh, logsErrCh := cmd2.Logs(ctx)
	for logEv := range logsCh {
		fmt.Printf("  [%s] %s\n", logEv.Stream, logEv.Data)
	}
	if err := <-logsErrCh; err != nil {
		log.Fatal(err)
	}

	cmd3, err := sb.RunCommandDetached(ctx, `printf "line1\nline2\n"`, nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	out, err := cmd3.Stdout(ctx)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nCommand.Stdout(): %q\n", out)
}
