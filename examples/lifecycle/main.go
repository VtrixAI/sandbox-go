// lifecycle: 停止 / 启动 / 延期 / 更新配置 / 列表 / 删除
package main

import (
	"context"
	"fmt"
	"log"
	"time"

	sandbox "github.com/VtrixAI/sandbox-go/src"
)

func main() {
	client := sandbox.NewClient(sandbox.ClientOptions{
		BaseURL:   "http://localhost:8080",
		Token:     "your-token",
		ProjectID: "seaclaw",
	})

	ctx := context.Background()

	// ── 列表查询 ─────────────────────────────────────────
	result, err := client.List(ctx, sandbox.ListOptions{
		Status: "active",
		Limit:  10,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Active sandboxes: %d (total=%d)\n", len(result.Items), result.Pagination.Total)
	for _, info := range result.Items {
		fmt.Printf("  %s  status=%-10s  expires=%s\n", info.ID, info.Status, info.ExpireAt)
	}

	// ── 创建沙箱 ─────────────────────────────────────────
	sb, err := client.Create(ctx, sandbox.CreateOptions{
		UserID:   "user-123",
		TTLHours: 1,
		Payloads: []sandbox.Payload{
			{API: "/api/v1/env", Body: map[string]string{"LOG_LEVEL": "info"}},
		},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("\nCreated: %s\n", sb.Info.ID)

	// ── 查询单个 ─────────────────────────────────────────
	info, err := client.Get(ctx, sb.Info.ID)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Get: status=%s, ip=%s\n", info.Status, info.IP)

	// ── 延期 12h ──────────────────────────────────────────
	if err := sb.Extend(ctx, 12); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Extended TTL by 12h")

	// ── 更新配置（不重启 WS，只改 spec/env）────────────
	if err := sb.Update(ctx, sandbox.UpdateOptions{
		Payloads: []sandbox.Payload{
			{API: "/api/v1/env", Body: map[string]string{"LOG_LEVEL": "debug"}},
		},
	}); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Updated env")

	// ── 立即应用配置 ──────────────────────────────────────
	if err := sb.Configure(ctx); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Configured")

	// ── 刷新本地 Info ─────────────────────────────────────
	if err := sb.Refresh(ctx); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Refreshed: status=%s\n", sb.Info.Status)

	// ── 停止 / 等待 / 启动 ───────────────────────────────
	if err := sb.Stop(ctx, nil); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Stopped")
	time.Sleep(2 * time.Second)

	if err := sb.Start(ctx); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Started")

	// ── 重启 ─────────────────────────────────────────────
	if err := sb.Restart(ctx); err != nil {
		log.Fatal(err)
	}
	fmt.Println("Restarted")

	// ── 删除 ─────────────────────────────────────────────
	sb.Close()
	if err := sb.Delete(ctx); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Deleted %s\n", sb.Info.ID)
}
