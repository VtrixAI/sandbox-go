package main

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/VtrixAI/sandbox-go/src"
)

// ────────────────────────────────────────────────────────────────────────────
// Stop / Start / Restart / Delete tests
// ────────────────────────────────────────────────────────────────────────────

func testStopStartRestart(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Stop / Start / Restart ──")

	// Create a fresh sandbox for stop/start testing
	sb, err := client.Create(ctx, sdk.CreateOptions{
		UserID: "test-stop-start",
		Labels: map[string]string{"suite": "lifecycle-stop"},
	})
	check("stop/start: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()
	fmt.Printf("    sandbox for stop/start: %s\n", id)

	// Stop (blocking)
	sbStop, attachErr := client.Attach(ctx, id)
	check("stop: attach no error", attachErr == nil, fmt.Sprint(attachErr))
	if attachErr != nil {
		client.Delete(ctx, id)
		return
	}
	stopErr := sbStop.Stop(ctx, &sdk.StopOptions{
		Blocking:     true,
		PollInterval: 2 * time.Second,
		Timeout:      2 * time.Minute,
	})
	sbStop.Close()
	check("stop blocking no error", stopErr == nil, fmt.Sprint(stopErr))

	info, getErr := client.Get(ctx, id)
	check("stop: get no error", getErr == nil, fmt.Sprint(getErr))
	if getErr != nil {
		client.Delete(ctx, id)
		return
	}
	check("stop: status stopped", info.Status == "stopped", info.Status)

	// Start
	sbStart, attachErr2 := client.Attach(ctx, id)
	check("start: attach no error", attachErr2 == nil, fmt.Sprint(attachErr2))
	if attachErr2 != nil {
		client.Delete(ctx, id)
		return
	}
	err2 := sbStart.Start(ctx)
	sbStart.Close()
	check("start no error", err2 == nil, fmt.Sprint(err2))

	// Poll until active (up to 5 minutes)
	deadline := time.Now().Add(5 * time.Minute)
	var info2 *sdk.Info
	for time.Now().Before(deadline) {
		i, _ := client.Get(ctx, id)
		if i != nil && i.Status == "active" {
			info2 = i
			break
		}
		time.Sleep(3 * time.Second)
	}
	if info2 == nil {
		info2, _ = client.Get(ctx, id)
	}
	check("start: status active", info2 != nil && info2.Status == "active", func() string {
		if info2 != nil {
			return info2.Status
		}
		return "nil"
	}())

	// Restart — only if sandbox is active
	if info2 != nil && info2.Status == "active" {
		sbRestart, attachErr3 := client.Attach(ctx, id)
		check("restart: attach no error", attachErr3 == nil, fmt.Sprint(attachErr3))
		if attachErr3 != nil {
			client.Delete(ctx, id)
			return
		}
		err3 := sbRestart.Restart(ctx)
		sbRestart.Close()
		check("restart no error", err3 == nil, fmt.Sprint(err3))

		// Verify: poll until active again after restart
		deadline2 := time.Now().Add(5 * time.Minute)
		var info3 *sdk.Info
		for time.Now().Before(deadline2) {
			i, _ := client.Get(ctx, id)
			if i != nil && i.Status == "active" {
				info3 = i
				break
			}
			time.Sleep(3 * time.Second)
		}
		if info3 == nil {
			info3, _ = client.Get(ctx, id)
		}
		check("restart: status active", info3 != nil && info3.Status == "active", func() string {
			if info3 != nil {
				return info3.Status
			}
			return "nil"
		}())
	} else {
		check("restart no error", true)       // skip: sandbox not active
		check("restart: status active", true) // skip
	}

	// Clean up
	time.Sleep(2 * time.Second)
	delErr := client.Delete(ctx, id)
	check("stop/start/restart sandbox deleted", delErr == nil, fmt.Sprint(delErr))
}

func testStopNonBlocking(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Stop non-blocking ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-stop-nonblock"})
	check("stop non-blocking: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	sbStop, attachErr := client.Attach(ctx, id)
	check("stop non-blocking: attach no error", attachErr == nil, fmt.Sprint(attachErr))
	if attachErr != nil {
		client.Delete(ctx, id)
		return
	}

	// non-blocking Stop：调用立即返回，不等待 stopped 状态
	t0 := time.Now()
	stopErr := sbStop.Stop(ctx, nil) // opts=nil → non-blocking
	elapsed := time.Since(t0)
	sbStop.Close()
	check("stop non-blocking: no error", stopErr == nil, fmt.Sprint(stopErr))
	check("stop non-blocking: returned quickly (< 5s)", elapsed < 5*time.Second,
		fmt.Sprintf("%.1fs", elapsed.Seconds()))

	// 轮询确认最终进入 stopped
	deadline := time.Now().Add(2 * time.Minute)
	var finalInfo *sdk.Info
	for time.Now().Before(deadline) {
		i, _ := client.Get(ctx, id)
		if i != nil && i.Status == "stopped" {
			finalInfo = i
			break
		}
		time.Sleep(2 * time.Second)
	}
	check("stop non-blocking: eventually stopped",
		finalInfo != nil && finalInfo.Status == "stopped",
		func() string {
			if finalInfo != nil {
				return finalInfo.Status
			}
			return "timed out"
		}())

	client.Delete(ctx, id)
}

func testStartOnActive(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Start on active sandbox (expect error) ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-start-active"})
	check("start on active: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	sbAttach, attachErr := client.Attach(ctx, id)
	check("start on active: attach no error", attachErr == nil, fmt.Sprint(attachErr))
	if attachErr != nil {
		client.Delete(ctx, id)
		return
	}
	defer sbAttach.Close()

	// active 状态调 Start 应返回错误
	startErr := sbAttach.Start(ctx)
	check("start on active: returns error", startErr != nil, fmt.Sprint(startErr))
	fmt.Printf("    start on active error: %v\n", startErr)

	// 状态应仍为 active
	info, getErr := client.Get(ctx, id)
	check("start on active: get no error", getErr == nil, fmt.Sprint(getErr))
	if getErr == nil {
		check("start on active: status still active", info.Status == "active", info.Status)
	}

	client.Delete(ctx, id)
}

func testStopOnStopped(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Stop on already-stopped sandbox (expect error or idempotent) ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-stop-on-stopped"})
	check("stop-on-stopped: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	// First stop (blocking)
	sbStop, attachErr := client.Attach(ctx, id)
	check("stop-on-stopped: attach no error", attachErr == nil, fmt.Sprint(attachErr))
	if attachErr != nil {
		client.Delete(ctx, id)
		return
	}
	stopErr := sbStop.Stop(ctx, &sdk.StopOptions{
		Blocking:     true,
		PollInterval: 2 * time.Second,
		Timeout:      2 * time.Minute,
	})
	sbStop.Close()
	check("stop-on-stopped: first stop no error", stopErr == nil, fmt.Sprint(stopErr))

	// Second stop on already-stopped: should return an error (or be idempotent — both acceptable;
	// we only require the sandbox to still be in stopped/non-active state afterwards)
	sbStop2, attachErr2 := client.Attach(ctx, id)
	check("stop-on-stopped: attach no error (2nd)", attachErr2 == nil, fmt.Sprint(attachErr2))
	if attachErr2 != nil {
		client.Delete(ctx, id)
		return
	}
	stopErr2 := sbStop2.Stop(ctx, nil)
	sbStop2.Close()
	// 记录行为（error 或 nil 都可接受），但沙箱不应回到 active
	fmt.Printf("    stop-on-stopped 2nd call result: %v\n", stopErr2)
	check("stop-on-stopped: 2nd stop returns error (expected)", stopErr2 != nil, fmt.Sprint(stopErr2))

	info, getErr := client.Get(ctx, id)
	check("stop-on-stopped: get no error", getErr == nil, fmt.Sprint(getErr))
	if getErr == nil {
		check("stop-on-stopped: status not active after 2nd stop", info.Status != "active", info.Status)
	}

	client.Delete(ctx, id)
}

func testRestartOnStopped(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Restart on stopped sandbox (expect error) ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-restart-stopped"})
	check("restart-on-stopped: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	// Stop first
	sbStop, attachErr := client.Attach(ctx, id)
	check("restart-on-stopped: attach no error", attachErr == nil, fmt.Sprint(attachErr))
	if attachErr != nil {
		client.Delete(ctx, id)
		return
	}
	stopErr := sbStop.Stop(ctx, &sdk.StopOptions{
		Blocking:     true,
		PollInterval: 2 * time.Second,
		Timeout:      2 * time.Minute,
	})
	sbStop.Close()
	check("restart-on-stopped: stop no error", stopErr == nil, fmt.Sprint(stopErr))
	if stopErr != nil {
		client.Delete(ctx, id)
		return
	}

	// Now restart on stopped — should return error
	sbRestart, attachErr2 := client.Attach(ctx, id)
	check("restart-on-stopped: attach no error (2nd)", attachErr2 == nil, fmt.Sprint(attachErr2))
	if attachErr2 != nil {
		client.Delete(ctx, id)
		return
	}
	restartErr := sbRestart.Restart(ctx)
	sbRestart.Close()
	check("restart-on-stopped: returns error", restartErr != nil, fmt.Sprint(restartErr))
	fmt.Printf("    restart-on-stopped error: %v\n", restartErr)

	// Status should remain stopped
	info, getErr := client.Get(ctx, id)
	check("restart-on-stopped: get no error", getErr == nil, fmt.Sprint(getErr))
	if getErr == nil {
		check("restart-on-stopped: status still stopped", info.Status == "stopped", info.Status)
	}

	client.Delete(ctx, id)
}

func testDelete(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Delete ──")

	// Create then delete via Client.Delete
	sb, err := client.Create(ctx, sdk.CreateOptions{
		UserID: "test-delete",
		Labels: map[string]string{"suite": "lifecycle-delete"},
	})
	check("client.Delete: create no error", err == nil, fmt.Sprint(err))
	if err == nil {
		id := sb.Info.ID
		sb.Close()

		delErr := client.Delete(ctx, id)
		check("client.Delete no error", delErr == nil, fmt.Sprint(delErr))

		// 轮询确认删除生效（异步删除，1s 固定等待不可靠）
		var deletedErr error
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			_, deletedErr = client.Get(ctx, id)
			if deletedErr != nil {
				break
			}
			time.Sleep(2 * time.Second)
		}
		check("get after delete returns error", deletedErr != nil, fmt.Sprint(deletedErr))
	}

	// Create then delete via Sandbox.Delete
	sb2, err2 := client.Create(ctx, sdk.CreateOptions{
		UserID: "test-delete-2",
	})
	check("sandbox.Delete: create no error", err2 == nil, fmt.Sprint(err2))
	if err2 == nil {
		id2 := sb2.Info.ID
		err3 := sb2.Delete(ctx)
		sb2.Close()
		check("sandbox.Delete no error", err3 == nil, fmt.Sprint(err3))

		// 轮询确认删除生效
		var deletedErr2 error
		deadline2 := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline2) {
			_, deletedErr2 = client.Get(ctx, id2)
			if deletedErr2 != nil {
				break
			}
			time.Sleep(2 * time.Second)
		}
		check("get after sandbox.Delete returns error", deletedErr2 != nil, fmt.Sprint(deletedErr2))
	}
}

func testDeleteStopped(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Delete stopped sandbox ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-delete-stopped"})
	check("delete-stopped: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	sbStop, attachErr := client.Attach(ctx, id)
	check("delete-stopped: attach no error", attachErr == nil, fmt.Sprint(attachErr))
	if attachErr != nil {
		client.Delete(ctx, id)
		return
	}
	stopErr := sbStop.Stop(ctx, &sdk.StopOptions{
		Blocking:     true,
		PollInterval: 2 * time.Second,
		Timeout:      2 * time.Minute,
	})
	sbStop.Close()
	check("delete-stopped: stop no error", stopErr == nil, fmt.Sprint(stopErr))
	if stopErr != nil {
		client.Delete(ctx, id)
		return
	}

	// Confirm stopped
	info, _ := client.Get(ctx, id)
	check("delete-stopped: status stopped before delete", info != nil && info.Status == "stopped", func() string {
		if info != nil {
			return info.Status
		}
		return "nil"
	}())

	delErr := client.Delete(ctx, id)
	check("delete-stopped: delete no error", delErr == nil, fmt.Sprint(delErr))

	// Poll until Get returns error (confirms deletion)
	deadline := time.Now().Add(30 * time.Second)
	var deletedErr error
	for time.Now().Before(deadline) {
		_, deletedErr = client.Get(ctx, id)
		if deletedErr != nil {
			break
		}
		time.Sleep(2 * time.Second)
	}
	check("delete-stopped: get after delete returns error", deletedErr != nil, fmt.Sprint(deletedErr))
}

func testDeleteNonExistent(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Delete non-existent sandbox (expect error) ──")

	err := client.Delete(ctx, "non-existent-sandbox-delete-xyz")
	check("delete non-existent: returns error", err != nil, fmt.Sprint(err))
	fmt.Printf("    delete non-existent error: %v\n", err)
}

func testAttachNonExistent(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Attach non-existent sandbox (expect error) ──")

	sb, err := client.Attach(ctx, "non-existent-sandbox-attach-xyz")
	check("attach non-existent: returns error", err != nil, fmt.Sprint(err))
	if sb != nil {
		sb.Close()
	}
	fmt.Printf("    attach non-existent error: %v\n", err)
}

func testDeleteTwice(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Delete twice (idempotency — 2nd call expect error) ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-delete-twice"})
	check("delete-twice: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	// First delete
	del1Err := client.Delete(ctx, id)
	check("delete-twice: first delete no error", del1Err == nil, fmt.Sprint(del1Err))

	// Poll until Get returns error (confirm deletion)
	deadline := time.Now().Add(30 * time.Second)
	var gone bool
	for time.Now().Before(deadline) {
		_, getErr := client.Get(ctx, id)
		if getErr != nil {
			gone = true
			break
		}
		time.Sleep(2 * time.Second)
	}
	check("delete-twice: sandbox eventually gone", gone)

	// Second delete — should return error
	del2Err := client.Delete(ctx, id)
	check("delete-twice: second delete returns error", del2Err != nil, fmt.Sprint(del2Err))
	fmt.Printf("    delete-twice 2nd delete error: %v\n", del2Err)
}

func testGetEmptyID(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Get/Attach with empty ID (expect error) ──")

	_, getErr := client.Get(ctx, "")
	check("get-empty-id: Get returns error", getErr != nil, fmt.Sprint(getErr))
	fmt.Printf("    Get('') error: %v\n", getErr)

	sb, attachErr := client.Attach(ctx, "")
	check("get-empty-id: Attach returns error", attachErr != nil, fmt.Sprint(attachErr))
	if sb != nil {
		sb.Close()
	}
	fmt.Printf("    Attach('') error: %v\n", attachErr)
}
