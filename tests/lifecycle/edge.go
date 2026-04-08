package main

import (
	"context"
	"fmt"
	"sync"
	"time"

	sdk "github.com/VtrixAI/sandbox-go/src"
)

// ────────────────────────────────────────────────────────────────────────────
// Edge case tests: context cancellation, concurrent ops, use-after-close, domain
// ────────────────────────────────────────────────────────────────────────────

func testContextCancellation(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Context cancellation ──")

	// ── 1. Get with already-cancelled context ──
	cancelledCtx, cancel := context.WithCancel(ctx)
	cancel() // cancel immediately
	_, err := client.Get(cancelledCtx, "any-id")
	check("ctx cancelled: Get returns error", err != nil, fmt.Sprint(err))
	fmt.Printf("    Get with cancelled ctx: %v\n", err)

	// ── 2. Create with very short deadline (should fail quickly) ──
	shortCtx, shortCancel := context.WithTimeout(ctx, 1*time.Millisecond)
	defer shortCancel()
	time.Sleep(2 * time.Millisecond) // ensure deadline already past
	_, err2 := client.Create(shortCtx, sdk.CreateOptions{UserID: "ctx-cancel-create"})
	check("ctx deadline: Create returns error", err2 != nil, fmt.Sprint(err2))
	fmt.Printf("    Create with expired ctx: %v\n", err2)

	// ── 3. Stop blocking — cancel context mid-poll ──
	sb, err3 := client.Create(ctx, sdk.CreateOptions{UserID: "ctx-cancel-stop"})
	check("ctx-cancel-stop: create no error", err3 == nil, fmt.Sprint(err3))
	if err3 != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	sbStop, attachErr := client.Attach(ctx, id)
	check("ctx-cancel-stop: attach no error", attachErr == nil, fmt.Sprint(attachErr))
	if attachErr != nil {
		client.Delete(ctx, id)
		return
	}

	stopCtx, stopCancel := context.WithTimeout(ctx, 200*time.Millisecond)
	defer stopCancel()
	// Use blocking stop with a very short context — it should return ctx error before completing
	stopErr := sbStop.Stop(stopCtx, &sdk.StopOptions{
		Blocking:     true,
		PollInterval: 1 * time.Second,
		Timeout:      5 * time.Minute,
	})
	sbStop.Close()
	check("ctx-cancel-stop: blocking stop respects ctx cancellation", stopErr != nil, fmt.Sprint(stopErr))
	fmt.Printf("    blocking stop cancelled ctx: %v\n", stopErr)

	// Clean up
	client.Delete(ctx, id)
}

func testUseAfterClose(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Use sandbox after Close() (expect error, not panic) ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-use-after-close"})
	check("use-after-close: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID

	// Close the WebSocket connection
	sb.Close()

	// Attempt Refresh on closed sandbox — should return an error, not panic
	refreshErr := sb.Refresh(ctx)
	check("use-after-close: Refresh returns error after Close", refreshErr != nil, fmt.Sprint(refreshErr))
	fmt.Printf("    use-after-close Refresh error: %v\n", refreshErr)

	// Clean up via client (not sandbox — it's already closed)
	client.Delete(ctx, id)
}

func testConcurrentCreate(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Concurrent Create (3 goroutines) ──")

	type createResult struct {
		sb  *sdk.Sandbox
		err error
	}

	const n = 3
	results := make([]createResult, n)
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			sb, err := client.Create(ctx, sdk.CreateOptions{
				UserID: fmt.Sprintf("concurrent-create-user-%d", i),
			})
			results[i] = createResult{sb, err}
		}()
	}
	wg.Wait()

	// All should succeed
	ids := make(map[string]bool)
	allOK := true
	for i, r := range results {
		check(fmt.Sprintf("concurrent-create: goroutine %d no error", i), r.err == nil, fmt.Sprint(r.err))
		if r.err != nil {
			allOK = false
		} else {
			ids[r.sb.Info.ID] = true
		}
	}
	if allOK {
		check("concurrent-create: all IDs unique", len(ids) == n, fmt.Sprintf("got %d unique IDs", len(ids)))
	}
	fmt.Printf("    concurrent-create: %d/%d succeeded, %d unique IDs\n", len(ids), n, len(ids))

	// Cleanup
	for _, r := range results {
		if r.sb != nil {
			r.sb.Close()
			client.Delete(ctx, r.sb.Info.ID)
		}
	}
}

func testConcurrentRefresh(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Concurrent Refresh (5 goroutines, same sandbox) ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-concurrent-refresh"})
	check("concurrent-refresh: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer func() {
		sb.Close()
		client.Delete(ctx, sb.Info.ID)
	}()

	const n = 5
	errs := make([]error, n)
	var wg sync.WaitGroup
	wg.Add(n)

	for i := 0; i < n; i++ {
		i := i
		go func() {
			defer wg.Done()
			errs[i] = sb.Refresh(ctx)
		}()
	}
	wg.Wait()

	errCount := 0
	for i, e := range errs {
		if e != nil {
			errCount++
			fmt.Printf("    concurrent-refresh goroutine %d error: %v\n", i, e)
		}
	}
	check("concurrent-refresh: all Refresh calls no error", errCount == 0,
		fmt.Sprintf("%d/%d failed", errCount, n))
}

func testDomain(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Domain() helper ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-domain"})
	check("domain: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer func() {
		sb.Close()
		client.Delete(ctx, sb.Info.ID)
	}()

	domain8080 := sb.Domain(8080)
	check("domain: Domain(8080) non-empty", domain8080 != "", domain8080)
	fmt.Printf("    Domain(8080): %s\n", domain8080)

	domain0 := sb.Domain(0)
	// Domain(0) behavior is implementation-defined; we only assert it doesn't panic
	check("domain: Domain(0) did not panic", true)
	fmt.Printf("    Domain(0): %q\n", domain0)
}
