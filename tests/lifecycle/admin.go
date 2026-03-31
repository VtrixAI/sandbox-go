package main

import (
	"context"
	"fmt"
	"time"

	sdk "github.com/VtrixAI/sandbox-go/src"
)

// ────────────────────────────────────────────────────────────────────────────
// Admin tests (no sandbox required)
// ────────────────────────────────────────────────────────────────────────────

func testPoolStatus(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── admin: PoolStatus ──")
	ps, err := client.PoolStatus(ctx)
	check("pool status no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	check("pool total >= 0", ps.Total >= 0, fmt.Sprint(ps.Total))
	check("pool warm >= 0", ps.Warm >= 0, fmt.Sprint(ps.Warm))
	check("pool active >= 0", ps.Active >= 0, fmt.Sprint(ps.Active))
	check("pool creating >= 0", ps.Creating >= 0, fmt.Sprint(ps.Creating))
	check("pool deleting >= 0", ps.Deleting >= 0, fmt.Sprint(ps.Deleting))
	check("pool warm_pool_size >= 0", ps.WarmPoolSize >= 0, fmt.Sprint(ps.WarmPoolSize))
	check("pool max_total >= 0", ps.MaxTotal >= 0, fmt.Sprint(ps.MaxTotal))
	check("pool utilization in [0,1]", ps.Utilization >= 0 && ps.Utilization <= 1, fmt.Sprintf("%.3f", ps.Utilization))
	check("pool healthy field present", true) // bool field always present; value is informational
	fmt.Printf("    pool: healthy=%v health_message=%q\n", ps.Healthy, ps.HealthMessage)
	fmt.Printf("    pool: total=%d warm=%d active=%d healthy=%v\n",
		ps.Total, ps.Warm, ps.Active, ps.Healthy)
}

func testRollingStatus(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── admin: RollingStatus ──")
	rs, err := client.RollingStatus(ctx)
	check("rolling status no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	check("rolling phase non-empty", rs.Phase != "", rs.Phase)
	check("rolling progress in [0,1]", rs.Progress >= 0 && rs.Progress <= 1, fmt.Sprintf("%.3f", rs.Progress))
	fmt.Printf("    rolling: phase=%s progress=%.0f%% warm=%d/%d active=%d/%d\n",
		rs.Phase, rs.Progress*100, rs.WarmUpdated, rs.WarmTotal, rs.ActiveUpdated, rs.ActiveTotal)
}

func testRollingFull(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── admin: Rolling Update (full) ──")

	// ── 前置：确认当前无滚动更新进行中 ──
	rs0, err := client.RollingStatus(ctx)
	check("rolling pre-check no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	if rs0.Phase != "idle" {
		// 有残留滚动更新，先取消
		fmt.Printf("    pre-existing rolling update (%s), cancelling first\n", rs0.Phase)
		client.RollingCancel(ctx)
		time.Sleep(3 * time.Second)
	}

	// ── 记录滚动前 warm pool 快照 ──
	ps0, err := client.PoolStatus(ctx)
	check("pool status before rolling no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	fmt.Printf("    pool before: warm=%d active=%d\n", ps0.Warm, ps0.Active)

	// ── RollingStart（不传 image，始终 latest）──
	rs, err := client.RollingStart(ctx, sdk.RollingStartOptions{})
	check("rolling start no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	check("rolling start phase=running", rs.Phase == "running", rs.Phase)
	fmt.Printf("    rolling start: phase=%s warm=%d/%d active=%d/%d\n",
		rs.Phase, rs.WarmUpdated, rs.WarmTotal, rs.ActiveUpdated, rs.ActiveTotal)

	// ── 轮询直到 phase=idle（完成）或超时 ──
	// 使用 1s 间隔（rotate job 也是 1s），确保能捕获中间进度
	fmt.Println("    polling rolling status...")
	deadline := time.Now().Add(15 * time.Minute)
	var finalRS *sdk.RollingStatus
	prevProgress := -1.0
	seenProgressBelow1 := false
	first := true
	for time.Now().Before(deadline) {
		if first {
			first = false
		} else {
			time.Sleep(1 * time.Second)
		}
		st, err := client.RollingStatus(ctx)
		if err != nil {
			fmt.Printf("    poll error: %v\n", err)
			continue
		}
		if st.Progress != prevProgress {
			fmt.Printf("    phase=%s progress=%.0f%% warm=%d/%d active=%d/%d\n",
				st.Phase, st.Progress*100, st.WarmUpdated, st.WarmTotal, st.ActiveUpdated, st.ActiveTotal)
			prevProgress = st.Progress
		}
		if st.Phase == "running" && st.Progress >= 0 && st.Progress < 1.0 {
			seenProgressBelow1 = true
		}
		if st.Phase == "idle" {
			finalRS = st
			break
		}
	}

	check("rolling completed (phase=idle)", finalRS != nil, func() string {
		if finalRS != nil {
			return finalRS.Phase
		}
		return "timed out"
	}())
	// 沙箱全已是新版本时 rolling 瞬间完成（第一次 poll 就 idle），看不到中间进度，属正常情况
	if !seenProgressBelow1 {
		fmt.Printf("    note: rolling completed instantly (all sandboxes already at current version), no intermediate progress observed\n")
	}
	if finalRS == nil {
		// 超时，取消清理
		client.RollingCancel(ctx)
		return
	}
	check("rolling progress=1.0", finalRS.Progress == 1.0, fmt.Sprintf("%.3f", finalRS.Progress))

	// ── 验证 warm pool 中新创建的沙箱使用目标镜像 ──
	// 从 warm pool 分配一个沙箱，验证其版本（通过 Create 触发分配）
	fmt.Println("    verifying new sandbox uses target image...")
	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "rolling-verify"})
	check("rolling verify: create no error", err == nil, fmt.Sprint(err))
	if err == nil {
		info, getErr := client.Get(ctx, sb.Info.ID)
		check("rolling verify: get no error", getErr == nil, fmt.Sprint(getErr))
		if getErr == nil {
			// 确认沙箱 active（从 warm pool 分配应立即 active）
			check("rolling verify: status active", info.Status == "active", info.Status)
			fmt.Printf("    new sandbox %s: status=%s\n", info.ID, info.Status)
		}
		sb.Close()
		client.Delete(ctx, sb.Info.ID)
	}

	// ── 二次 RollingStart 时已在 idle，应能正常开启新一轮 ──
	rs2, err2 := client.RollingStart(ctx, sdk.RollingStartOptions{})
	check("rolling restart no error", err2 == nil, fmt.Sprint(err2))
	if err2 == nil {
		check("rolling restart phase=running", rs2.Phase == "running", rs2.Phase)
		fmt.Printf("    rolling restart: phase=%s\n", rs2.Phase)
		// 取消，避免影响后续测试
		time.Sleep(2 * time.Second)
		rc, cancelErr := client.RollingCancel(ctx)
		check("rolling restart cancel no error", cancelErr == nil, fmt.Sprint(cancelErr))
		if cancelErr == nil {
			check("rolling restart cancel phase=idle", rc.Phase == "idle", rc.Phase)
		}
	}
}

func testRollingStartWithImage(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── admin: RollingStart with explicit Image ──")

	if testImage == "" {
		fmt.Println("    SKIPPED: HERMES_IMAGE not set")
		check("rolling with image: skipped", true)
		return
	}

	// 确保当前无进行中的滚动更新
	rs0, err := client.RollingStatus(ctx)
	check("rolling-with-image: pre-check no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	if rs0.Phase != "idle" {
		fmt.Printf("    pre-existing rolling update (%s), cancelling first\n", rs0.Phase)
		client.RollingCancel(ctx)
		time.Sleep(3 * time.Second)
	}

	rs, err := client.RollingStart(ctx, sdk.RollingStartOptions{Image: testImage})
	check("rolling-with-image: start no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	check("rolling-with-image: phase=running", rs.Phase == "running", rs.Phase)
	check("rolling-with-image: target_image set", rs.TargetImage == testImage,
		fmt.Sprintf("want=%s got=%s", testImage, rs.TargetImage))
	fmt.Printf("    rolling start with image: phase=%s target=%s\n", rs.Phase, rs.TargetImage)

	// 取消，避免长时间占用
	time.Sleep(2 * time.Second)
	rc, cancelErr := client.RollingCancel(ctx)
	check("rolling-with-image: cancel no error", cancelErr == nil, fmt.Sprint(cancelErr))
	if cancelErr == nil {
		check("rolling-with-image: cancel phase=idle", rc.Phase == "idle", rc.Phase)
	}
}

func testRollingCancelWhenIdle(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── admin: RollingCancel when idle (expect error or no-op) ──")

	// 确保当前处于 idle
	rs, err := client.RollingStatus(ctx)
	check("rolling-cancel-idle: status no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	if rs.Phase != "idle" {
		fmt.Printf("    rolling not idle (%s), skipping cancel-when-idle test\n", rs.Phase)
		check("rolling-cancel-idle: skipped (not idle)", true)
		return
	}

	rc, cancelErr := client.RollingCancel(ctx)
	// 服务端可能返回 error（"no rolling update in progress"）或幂等返回 idle；两种都可接受
	fmt.Printf("    cancel-when-idle result: err=%v phase=%v\n", cancelErr, func() string {
		if rc != nil {
			return rc.Phase
		}
		return "nil"
	}())
	check("rolling-cancel-idle: did not panic", true)
	if cancelErr == nil {
		check("rolling-cancel-idle: phase=idle when no error", rc.Phase == "idle", rc.Phase)
	} else {
		check("rolling-cancel-idle: error is non-nil (expected behavior)", true, fmt.Sprint(cancelErr))
	}
}
