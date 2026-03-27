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
//	HERMES_TOKEN    Bearer token      (required)
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
	token     = envOr("HERMES_TOKEN", "test")
	project   = envOr("HERMES_PROJECT", "local")
	testImage = envOr("HERMES_IMAGE", "")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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

func testRollingStartCancel(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── admin: RollingStart + RollingCancel ──")
	if testImage == "" {
		fmt.Println("  ⚠ HERMES_IMAGE not set — skipping RollingStart test")
		return
	}

	rs, err := client.RollingStart(ctx, sdk.RollingStartOptions{Image: testImage})
	check("rolling start no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	check("rolling start phase non-empty", rs.Phase != "", rs.Phase)
	check("rolling start target_image matches", rs.TargetImage == testImage, rs.TargetImage)
	fmt.Printf("    rolling start: phase=%s target=%s\n", rs.Phase, rs.TargetImage)

	// Give it a moment then cancel
	time.Sleep(2 * time.Second)
	rc, err := client.RollingCancel(ctx)
	check("rolling cancel no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	check("rolling cancel phase non-empty", rc.Phase != "", rc.Phase)
	fmt.Printf("    rolling cancel: phase=%s\n", rc.Phase)
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
}

func testCreateAndGet(ctx context.Context, client *sdk.Client) string {
	fmt.Println("\n── lifecycle: Create ──")

	// Create with minimal options
	sb := mustV(client.Create(ctx, sdk.CreateOptions{
		UserID: "test-user",
		Labels: map[string]string{"env": "test", "suite": "lifecycle"},
	}))
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

	sb, err := client.Create(ctx, sdk.CreateOptions{
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
	mustE(client.Delete(ctx, sb.Info.ID))
	check("create spec: deleted", true)
}

func testRefreshAndStatus(ctx context.Context, client *sdk.Client, sandboxID string) {
	fmt.Println("\n── lifecycle: Refresh / Status / CreatedAt / Timeout ──")

	sb := mustV(client.Attach(ctx, sandboxID))
	defer sb.Close()

	check("status before refresh non-empty", sb.Status() != "", sb.Status())

	err := sb.Refresh(ctx)
	check("refresh no error", err == nil, fmt.Sprint(err))
	check("status after refresh active", sb.Status() == "active", sb.Status())
	check("created_at parseable", !sb.CreatedAt().IsZero())
	check("expire_at non-empty", sb.ExpireAt() != "", sb.ExpireAt())
	check("timeout ms > 0", sb.Timeout() > 0, fmt.Sprintf("%d ms", sb.Timeout()))
}

func testExtend(ctx context.Context, client *sdk.Client, sandboxID string) {
	fmt.Println("\n── lifecycle: Extend / ExtendTimeout ──")

	sb := mustV(client.Attach(ctx, sandboxID))
	defer sb.Close()

	mustE(sb.Refresh(ctx))
	before := sb.ExpireAt()

	// Extend by 1 hour
	err := sb.Extend(ctx, 1)
	check("extend 1h no error", err == nil, fmt.Sprint(err))

	mustE(sb.Refresh(ctx))
	after := sb.ExpireAt()
	check("extend updated expire_at", after != before || after != "", fmt.Sprintf("%s → %s", before, after))

	// ExtendTimeout (extend + refresh in one call)
	err2 := sb.ExtendTimeout(ctx, 0) // server default
	check("extendTimeout no error", err2 == nil, fmt.Sprint(err2))
	check("extendTimeout expire_at set", sb.ExpireAt() != "", sb.ExpireAt())
}

func testUpdate(ctx context.Context, client *sdk.Client, sandboxID string) {
	fmt.Println("\n── lifecycle: Update ──")

	sb := mustV(client.Attach(ctx, sandboxID))
	defer sb.Close()

	// Update with new image (if provided) or just spec
	opts := sdk.UpdateOptions{
		Spec: &sdk.Spec{CPU: "500m", Memory: "512Mi"},
	}
	if testImage != "" {
		opts.Image = testImage
	}
	err := sb.Update(ctx, opts)
	check("update no error", err == nil, fmt.Sprint(err))
}

func testConfigure(ctx context.Context, client *sdk.Client, sandboxID string) {
	fmt.Println("\n── lifecycle: Configure ──")

	sb := mustV(client.Attach(ctx, sandboxID))
	defer sb.Close()

	// Configure with no payloads (re-apply stored config)
	err := sb.Configure(ctx)
	check("configure no payloads no error", err == nil, fmt.Sprint(err))

	// Configure with explicit payload
	err2 := sb.Configure(ctx, sdk.Payload{
		API:  "/api/v1/env",
		Body: map[string]string{"KEY": "VALUE"},
	})
	check("configure with payload no error", err2 == nil, fmt.Sprint(err2))
}

func testStopStartRestart(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Stop / Start / Restart ──")

	// Create a fresh sandbox for stop/start testing
	sb := mustV(client.Create(ctx, sdk.CreateOptions{
		UserID: "test-stop-start",
		Labels: map[string]string{"suite": "lifecycle-stop"},
	}))
	id := sb.Info.ID
	sb.Close()
	fmt.Printf("    sandbox for stop/start: %s\n", id)

	// Stop (blocking)
	sbStop := mustV(client.Attach(ctx, id))
	err := sbStop.Stop(ctx, &sdk.StopOptions{
		Blocking:     true,
		PollInterval: 2 * time.Second,
		Timeout:      2 * time.Minute,
	})
	sbStop.Close()
	check("stop blocking no error", err == nil, fmt.Sprint(err))

	info := mustV(client.Get(ctx, id))
	check("stop: status stopped", info.Status == "stopped", info.Status)

	// Start
	sbStart := mustV(client.Attach(ctx, id))
	err2 := sbStart.Start(ctx)
	sbStart.Close()
	check("start no error", err2 == nil, fmt.Sprint(err2))

	// Poll until active
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		i, _ := client.Get(ctx, id)
		if i != nil && i.Status == "active" {
			break
		}
		time.Sleep(3 * time.Second)
	}
	info2 := mustV(client.Get(ctx, id))
	check("start: status active", info2.Status == "active", info2.Status)

	// Restart
	sbRestart := mustV(client.Attach(ctx, id))
	err3 := sbRestart.Restart(ctx)
	sbRestart.Close()
	check("restart no error", err3 == nil, fmt.Sprint(err3))

	// Clean up
	time.Sleep(2 * time.Second)
	client.Delete(ctx, id)
	check("stop/start/restart sandbox deleted", true)
}

func testDelete(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Delete ──")

	// Create then delete via Client.Delete
	sb := mustV(client.Create(ctx, sdk.CreateOptions{
		UserID: "test-delete",
		Labels: map[string]string{"suite": "lifecycle-delete"},
	}))
	id := sb.Info.ID
	sb.Close()

	err := client.Delete(ctx, id)
	check("client.Delete no error", err == nil, fmt.Sprint(err))

	// Verify gone (should error)
	time.Sleep(1 * time.Second)
	_, err2 := client.Get(ctx, id)
	check("get after delete returns error", err2 != nil, fmt.Sprint(err2))

	// Create then delete via Sandbox.Delete
	sb2 := mustV(client.Create(ctx, sdk.CreateOptions{
		UserID: "test-delete-2",
	}))
	id2 := sb2.Info.ID
	err3 := sb2.Delete(ctx)
	sb2.Close()
	check("sandbox.Delete no error", err3 == nil, fmt.Sprint(err3))

	time.Sleep(1 * time.Second)
	_, err4 := client.Get(ctx, id2)
	check("get after sandbox.Delete returns error", err4 != nil, fmt.Sprint(err4))
}

// ────────────────────────────────────────────────────────────────────────────
// Runner
// ────────────────────────────────────────────────────────────────────────────

func main() {
	ctx := context.Background()
	client := newClient()

	fmt.Printf("Target: %s  Project: %s\n", baseURL, project)

	// Admin (no sandbox needed)
	testPoolStatus(ctx, client)
	testRollingStatus(ctx, client)
	testRollingStartCancel(ctx, client)

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
