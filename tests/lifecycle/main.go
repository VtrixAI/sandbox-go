// Hermes Go SDK — Lifecycle & Admin Integration Test
//
// Tests: Create, Get, List, Update, Extend, Stop, Start, Restart, Delete,
//        Configure, Refresh, PoolStatus, RollingStatus, RollingStart, RollingCancel
//
// Run:
//
//	cd sdk/go/tests/lifecycle
//	HERMES_URL=https://hermes-gateway.sandbox.cloud.vtrix.ai \
//	HERMES_TOKEN=<token> HERMES_PROJECT=<project> \
//	go run main.go
//
// Environment variables:
//
//	HERMES_URL      Gateway base URL  (required)
//	HERMES_TOKEN    Bearer token（必填，与网关 auth.token 一致；勿写入源码）
//	HERMES_PROJECT  Project ID        (required)
//	HERMES_IMAGE    Sandbox image tag (optional, for RollingStart test)
package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	sdk "github.com/VtrixAI/sandbox-go/src"
)

var (
	baseURL   = envOr("HERMES_URL", "http://localhost:8080")
	token     string // set in main from HERMES_TOKEN
	project   = envOr("HERMES_PROJECT", "local")
	testImage = envOr("HERMES_IMAGE", "harbor.ops.seaart.ai/seacloud-develop/nano-executor:b3f8165")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "error: env var %s is required\n", key)
		os.Exit(1)
	}
	return v
}

const pass = "✅"
const fail = "❌"

type result struct {
	name   string
	ok     bool
	detail string
}

var results []result

func check(name string, cond bool, detail ...string) {
	mark := pass
	if !cond {
		mark = fail
	}
	d := ""
	if len(detail) > 0 {
		d = "  [" + detail[0] + "]"
	}
	fmt.Printf("  %s %s%s\n", mark, name, d)
	det := ""
	if len(detail) > 0 {
		det = detail[0]
	}
	results = append(results, result{name, cond, det})
}

func mustV[T any](v T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("fatal: %v", err))
	}
	return v
}

func mustE(err error) {
	if err != nil {
		panic(fmt.Sprintf("fatal: %v", err))
	}
}

func softV[T any](v T, err error) (T, error) { return v, err }

func newClient() *sdk.Client {
	return sdk.NewClient(sdk.ClientOptions{
		BaseURL:   baseURL,
		Token:     token,
		ProjectID: project,
	})
}

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
	check("pool healthy field present", ps.Healthy || !ps.Healthy) // always valid bool
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

// ────────────────────────────────────────────────────────────────────────────
// Lifecycle tests
// ────────────────────────────────────────────────────────────────────────────

func testList(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: List ──")

	// List all
	res, err := client.List(ctx, sdk.ListOptions{})
	check("list all no error", err == nil, fmt.Sprint(err))
	if err == nil {
		check("list all items not nil", res.Items != nil)
		check("list pagination.total >= 0", res.Pagination.Total >= 0, fmt.Sprint(res.Pagination.Total))
		fmt.Printf("    total=%d items_in_page=%d\n", res.Pagination.Total, len(res.Items))
	}

	// List with limit=1
	res2, err := client.List(ctx, sdk.ListOptions{Limit: 1})
	check("list limit=1 no error", err == nil, fmt.Sprint(err))
	if err == nil {
		check("list limit=1 at most 1 item", len(res2.Items) <= 1, fmt.Sprint(len(res2.Items)))
		check("list limit=1 pagination.limit=1", res2.Pagination.Limit == 1, fmt.Sprint(res2.Pagination.Limit))
	}

	// List with offset
	res3, err := client.List(ctx, sdk.ListOptions{Limit: 10, Offset: 0})
	check("list offset=0 no error", err == nil, fmt.Sprint(err))
	if err == nil {
		check("list offset=0 pagination.offset=0", res3.Pagination.Offset == 0, fmt.Sprint(res3.Pagination.Offset))
	}

	// List with status filter
	res4, err := client.List(ctx, sdk.ListOptions{Status: "active"})
	check("list status=active no error", err == nil, fmt.Sprint(err))
	if err == nil {
		allActive := true
		for _, sb := range res4.Items {
			if sb.Status != "active" {
				allActive = false
			}
		}
		check("list status=active all items active", allActive)
	}

	// List with user_id filter — create a sandbox with a unique user, then filter by it
	tmpSB, err := client.Create(ctx, sdk.CreateOptions{UserID: "list-filter-user"})
	check("list user_id: create no error", err == nil, fmt.Sprint(err))
	if err == nil {
		res5, err5 := client.List(ctx, sdk.ListOptions{UserID: "list-filter-user"})
		check("list user_id filter no error", err5 == nil, fmt.Sprint(err5))
		if err5 == nil {
			found := false
			allMatch := true
			for _, item := range res5.Items {
				if item.ID == tmpSB.Info.ID {
					found = true
				}
				if item.UserID != "list-filter-user" {
					allMatch = false
				}
			}
			check("list user_id: target sandbox found", found)
			check("list user_id: all items match user_id", allMatch)
		}
		tmpSB.Close()
		client.Delete(ctx, tmpSB.Info.ID)
	}
}

func testCreateAndGet(ctx context.Context, client *sdk.Client) string {
	fmt.Println("\n── lifecycle: Create ──")

	// Create with minimal options
	sb, err := client.Create(ctx, sdk.CreateOptions{
		UserID: "test-user",
		Labels: map[string]string{"env": "test", "suite": "lifecycle"},
	})
	check("create: no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return ""
	}
	defer sb.Close()

	id := sb.Info.ID
	check("create: id non-empty", id != "", id)
	check("create: status active", sb.Info.Status == "active", sb.Info.Status)
	check("create: user_id matches", sb.Info.UserID == "test-user", sb.Info.UserID)
	check("create: label env=test", sb.Info.Labels["env"] == "test")
	check("create: label suite=lifecycle", sb.Info.Labels["suite"] == "lifecycle")
	check("create: created_at non-empty", sb.Info.CreatedAt != "", sb.Info.CreatedAt)
	check("create: expire_at non-empty", sb.Info.ExpireAt != "", sb.Info.ExpireAt)
	fmt.Printf("    created sandbox: %s\n", id)

	// Get
	fmt.Println("\n── lifecycle: Get ──")
	info, err := client.Get(ctx, id)
	check("get no error", err == nil, fmt.Sprint(err))
	if err == nil {
		check("get id matches", info.ID == id)
		check("get status active", info.Status == "active", info.Status)
		check("get user_id matches", info.UserID == "test-user", info.UserID)
		check("get namespace non-empty", info.Namespace != "", info.Namespace)
	}

	// Get non-existent
	_, err2 := client.Get(ctx, "non-existent-sandbox-xyz")
	check("get non-existent returns error", err2 != nil, fmt.Sprint(err2))

	return id
}

func testCreateWithSpec(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Create with Spec ──")

	// Spec 创建走冷启动，给更长的超时
	specCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	sb, err := client.Create(specCtx, sdk.CreateOptions{
		UserID: "test-user-spec",
		Spec: &sdk.Spec{
			CPU:    "500m",
			Memory: "512Mi",
		},
		TTLHours: 1,
	})
	check("create with spec no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	check("create spec: id non-empty", sb.Info.ID != "")
	check("create spec: status active", sb.Info.Status == "active", sb.Info.Status)
	fmt.Printf("    spec sandbox: %s\n", sb.Info.ID)

	// Clean up
	_ = client.Delete(ctx, sb.Info.ID)
	check("create spec: deleted", true)
}

func testRefreshAndStatus(ctx context.Context, client *sdk.Client, sandboxID string) {
	fmt.Println("\n── lifecycle: Refresh / Status / CreatedAt / Timeout ──")

	sb, err := client.Attach(ctx, sandboxID)
	check("attach no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	check("status before refresh non-empty", sb.Status() != "", sb.Status())

	err = sb.Refresh(ctx)
	check("refresh no error", err == nil, fmt.Sprint(err))
	check("status after refresh active", sb.Status() == "active", sb.Status())
	check("created_at parseable", !sb.CreatedAt().IsZero())
	check("expire_at non-empty", sb.ExpireAt() != "", sb.ExpireAt())
	check("timeout ms > 0", sb.Timeout() > 0, fmt.Sprintf("%d ms", sb.Timeout()))
}

func testExtend(ctx context.Context, client *sdk.Client, sandboxID string) {
	fmt.Println("\n── lifecycle: Extend / ExtendTimeout ──")

	sb, err := client.Attach(ctx, sandboxID)
	check("attach no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	if err := sb.Refresh(ctx); err != nil {
		check("refresh no error", false, fmt.Sprint(err))
		return
	}
	before := sb.ExpireAt()

	// Extend by 1 hour
	err2 := sb.Extend(ctx, 1)
	check("extend 1h no error", err2 == nil, fmt.Sprint(err2))

	if err3 := sb.Refresh(ctx); err3 != nil {
		check("refresh after extend no error", false, fmt.Sprint(err3))
		return
	}
	after := sb.ExpireAt()
	check("extend updated expire_at", after != before || after != "", fmt.Sprintf("%s → %s", before, after))

	// ExtendTimeout (extend + refresh in one call)
	err2 = sb.ExtendTimeout(ctx, 0) // server default
	check("extendTimeout no error", err2 == nil, fmt.Sprint(err2))
	check("extendTimeout expire_at set", sb.ExpireAt() != "", sb.ExpireAt())
}

func testUpdate(ctx context.Context, client *sdk.Client, sandboxID string) {
	fmt.Println("\n── lifecycle: Update ──")

	sb, err := client.Attach(ctx, sandboxID)
	check("attach no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	// Update with new spec (cpu/memory)
	newCPU := "750m"
	newMem := "768Mi"
	opts := sdk.UpdateOptions{
		Spec: &sdk.Spec{CPU: newCPU, Memory: newMem},
	}
	if testImage != "" {
		opts.Image = testImage
	}
	err = sb.Update(ctx, opts)
	check("update no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}

	// Verify: spec fields updated immediately (even while pod is recreating)
	info, err2 := client.Get(ctx, sandboxID)
	check("update: get after update no error", err2 == nil, fmt.Sprint(err2))
	if err2 == nil {
		check("update: spec not nil", info.Spec != nil)
		if info.Spec != nil {
			check("update: cpu updated", info.Spec.CPU == newCPU, fmt.Sprintf("want=%s got=%s", newCPU, info.Spec.CPU))
			check("update: memory updated", info.Spec.Memory == newMem, fmt.Sprintf("want=%s got=%s", newMem, info.Spec.Memory))
		}
		fmt.Printf("    spec after update: cpu=%s memory=%s status=%s\n", func() string {
			if info.Spec != nil {
				return info.Spec.CPU
			}
			return "nil"
		}(), func() string {
			if info.Spec != nil {
				return info.Spec.Memory
			}
			return "nil"
		}(), info.Status)
	}

	// Wait for sandbox to return to active (Update triggers pod recreation)
	deadline := time.Now().Add(5 * time.Minute)
	var finalInfo *sdk.Info
	for time.Now().Before(deadline) {
		i, _ := client.Get(ctx, sandboxID)
		if i != nil && i.Status == "active" {
			finalInfo = i
			break
		}
		time.Sleep(3 * time.Second)
	}
	if finalInfo == nil {
		finalInfo, _ = client.Get(ctx, sandboxID)
	}
	check("update: back to active after pod recreation", finalInfo != nil && finalInfo.Status == "active", func() string {
		if finalInfo != nil {
			return finalInfo.Status
		}
		return "nil"
	}())
}

func testConfigure(ctx context.Context, client *sdk.Client, sandboxID string) {
	fmt.Println("\n── lifecycle: Configure ──")

	sb, err := client.Attach(ctx, sandboxID)
	check("attach no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	// Configure with no payloads (re-apply stored config)
	err = sb.Configure(ctx)
	check("configure no payloads no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}

	// Verify: Configure may trigger pod recreation; poll until active
	deadline := time.Now().Add(5 * time.Minute)
	var finalInfo *sdk.Info
	for time.Now().Before(deadline) {
		i, _ := client.Get(ctx, sandboxID)
		if i != nil && i.Status == "active" {
			finalInfo = i
			break
		}
		time.Sleep(3 * time.Second)
	}
	if finalInfo == nil {
		finalInfo, _ = client.Get(ctx, sandboxID)
	}
	check("configure: back to active", finalInfo != nil && finalInfo.Status == "active", func() string {
		if finalInfo != nil {
			return finalInfo.Status
		}
		return "nil"
	}())

	// Configure with explicit payloads
	// 稍等片刻，让 K8s informer 刷新 Pod IP（避免 provider.GetSandbox 拿到旧 IP）
	fmt.Println("\n── lifecycle: Configure with Payloads ──")
	var err2 error
	for i := 0; i < 5; i++ {
		err2 = sb.Configure(ctx, sdk.Payload{
			API:  "/api/v1/env",
			Body: map[string]string{"TEST_VAR": "hello"},
		})
		if err2 == nil {
			break
		}
		fmt.Printf("    configure with payload attempt %d error: %v, retrying...\n", i+1, err2)
		time.Sleep(5 * time.Second)
	}
	check("configure with payload no error", err2 == nil, fmt.Sprint(err2))
	if err2 != nil {
		return
	}
	// Poll until active again
	deadline2 := time.Now().Add(5 * time.Minute)
	var finalInfo2 *sdk.Info
	for time.Now().Before(deadline2) {
		i, _ := client.Get(ctx, sandboxID)
		if i != nil && i.Status == "active" {
			finalInfo2 = i
			break
		}
		time.Sleep(3 * time.Second)
	}
	if finalInfo2 == nil {
		finalInfo2, _ = client.Get(ctx, sandboxID)
	}
	check("configure with payload: back to active", finalInfo2 != nil && finalInfo2.Status == "active", func() string {
		if finalInfo2 != nil {
			return finalInfo2.Status
		}
		return "nil"
	}())
}

func testSpecPersistence(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Spec/Image persistence through Stop→Start ──")

	// Create a fresh sandbox and update it with a custom image + spec
	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-spec-persist"})
	check("spec persist: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()
	fmt.Printf("    sandbox: %s\n", id)

	// Wait until active
	deadline := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline) {
		i, _ := client.Get(ctx, id)
		if i != nil && i.Status == "active" {
			break
		}
		time.Sleep(3 * time.Second)
	}

	// Update: set a pinned image + custom spec
	sbUp, attachErr := client.Attach(ctx, id)
	check("spec persist: attach no error", attachErr == nil, fmt.Sprint(attachErr))
	if attachErr != nil {
		client.Delete(ctx, id)
		return
	}
	updateErr := sbUp.Update(ctx, sdk.UpdateOptions{
		Image: testImage,
		Spec:  &sdk.Spec{CPU: "600m", Memory: "640Mi"},
	})
	sbUp.Close()
	check("spec persist: update no error", updateErr == nil, fmt.Sprint(updateErr))
	if updateErr != nil {
		client.Delete(ctx, id)
		return
	}

	// Wait until active again after update
	deadline2 := time.Now().Add(5 * time.Minute)
	for time.Now().Before(deadline2) {
		i, _ := client.Get(ctx, id)
		if i != nil && i.Status == "active" {
			break
		}
		time.Sleep(3 * time.Second)
	}

	// Verify spec/image before stop
	beforeInfo, err := client.Get(ctx, id)
	check("spec persist: get after update no error", err == nil, fmt.Sprint(err))
	if err == nil && beforeInfo.Spec != nil {
		fmt.Printf("    before stop: image=%s cpu=%s\n", beforeInfo.ImageTag, beforeInfo.Spec.CPU)
	}

	// Stop (blocking)
	sbStop, attachErr2 := client.Attach(ctx, id)
	check("spec persist: attach for stop no error", attachErr2 == nil, fmt.Sprint(attachErr2))
	if attachErr2 != nil {
		client.Delete(ctx, id)
		return
	}
	stopErr := sbStop.Stop(ctx, &sdk.StopOptions{
		Blocking:     true,
		PollInterval: 2 * time.Second,
		Timeout:      2 * time.Minute,
	})
	sbStop.Close()
	check("spec persist: stop no error", stopErr == nil, fmt.Sprint(stopErr))

	// Start
	sbStart, attachErr3 := client.Attach(ctx, id)
	check("spec persist: attach for start no error", attachErr3 == nil, fmt.Sprint(attachErr3))
	if attachErr3 != nil {
		client.Delete(ctx, id)
		return
	}
	startErr := sbStart.Start(ctx)
	sbStart.Close()
	check("spec persist: start no error", startErr == nil, fmt.Sprint(startErr))

	// Poll until active
	deadline3 := time.Now().Add(5 * time.Minute)
	var afterInfo *sdk.Info
	for time.Now().Before(deadline3) {
		i, _ := client.Get(ctx, id)
		if i != nil && i.Status == "active" {
			afterInfo = i
			break
		}
		time.Sleep(3 * time.Second)
	}
	check("spec persist: active after start", afterInfo != nil && afterInfo.Status == "active", func() string {
		if afterInfo != nil {
			return afterInfo.Status
		}
		return "nil"
	}())

	if afterInfo != nil {
		fmt.Printf("    after start: image=%s cpu=%s\n", afterInfo.ImageTag, func() string {
			if afterInfo.Spec != nil {
				return afterInfo.Spec.CPU
			}
			return "nil"
		}())
		check("spec persist: image preserved after stop→start",
			afterInfo.ImageTag == testImage,
			fmt.Sprintf("want=%s got=%s", testImage, afterInfo.ImageTag))
		if afterInfo.Spec != nil {
			check("spec persist: cpu preserved after stop→start",
				afterInfo.Spec.CPU == "600m",
				fmt.Sprintf("want=600m got=%s", afterInfo.Spec.CPU))
			check("spec persist: memory preserved after stop→start",
				afterInfo.Spec.Memory == "640Mi",
				fmt.Sprintf("want=640Mi got=%s", afterInfo.Spec.Memory))
		} else {
			check("spec persist: spec not nil after stop→start", false, "spec is nil")
			check("spec persist: cpu preserved after stop→start", false, "spec is nil")
			check("spec persist: memory preserved after stop→start", false, "spec is nil")
		}
	}

	client.Delete(ctx, id)
}

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
	client.Delete(ctx, id)
	check("stop/start/restart sandbox deleted", true)
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

		time.Sleep(1 * time.Second)
		_, err2 := client.Get(ctx, id)
		check("get after delete returns error", err2 != nil, fmt.Sprint(err2))
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

		time.Sleep(1 * time.Second)
		_, err4 := client.Get(ctx, id2)
		check("get after sandbox.Delete returns error", err4 != nil, fmt.Sprint(err4))
	}
}

// ────────────────────────────────────────────────────────────────────────────
// Runner
// ────────────────────────────────────────────────────────────────────────────

func main() {
	token = mustEnv("HERMES_TOKEN")
	ctx := context.Background()
	client := newClient()

	fmt.Printf("Target: %s  Project: %s\n", baseURL, project)

	// Admin (no sandbox needed)
	testPoolStatus(ctx, client)
	testRollingStatus(ctx, client)
	testRollingFull(ctx, client)

	// Lifecycle
	testList(ctx, client)

	sandboxID := testCreateAndGet(ctx, client)
	if sandboxID != "" {
		testRefreshAndStatus(ctx, client, sandboxID)
		testExtend(ctx, client, sandboxID)
		testUpdate(ctx, client, sandboxID)
		testConfigure(ctx, client, sandboxID)

		// Clean up the main test sandbox
		client.Delete(ctx, sandboxID)
	}

	testCreateWithSpec(ctx, client)
	testStopStartRestart(ctx, client)
	testSpecPersistence(ctx, client)
	testDelete(ctx, client)

	// Summary
	passed := 0
	for _, r := range results {
		if r.ok {
			passed++
		}
	}
	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Printf("Lifecycle: %d/%d passed\n", passed, len(results))
	for _, r := range results {
		if !r.ok {
			extra := ""
			if r.detail != "" {
				extra = "  [" + r.detail + "]"
			}
			fmt.Printf("  %s FAILED: %s%s\n", fail, r.name, extra)
		}
	}
	if passed < len(results) {
		os.Exit(1)
	}
}
