// Hermes Go SDK — Lifecycle & Admin Integration Test
//
// Tests: Create, Get, List, Update, Extend, Stop, Start, Restart, Delete,
//
//	Configure, Refresh, PoolStatus, RollingStatus, RollingStart, RollingCancel,
//	Create with Payloads, Create with default Env, Stop non-blocking,
//	Start on active (error), Delete non-existent (error), Attach non-existent (error),
//	State machine boundaries, Create variants (TTL/Spec.Image/combined),
//	List edge cases (pagination/combined filter/limit=0),
//	Update single-field (image/spec), Configure payloads, Extend (seconds) edge cases,
//	Context cancellation, Concurrent ops, Use-after-close, Domain helper
//
// Run:
//
//	cd sdk/go/tests/lifecycle
//	HERMES_URL=https://hermes-gateway.sandbox.cloud.vtrix.ai \
//	HERMES_TOKEN=<token> HERMES_PROJECT=<project> \
//	go run .
//
// Environment variables:
//
//	HERMES_URL      Gateway base URL  (required)
//	HERMES_TOKEN    Bearer token（必填，与网关 auth.token 一致；勿写入源码）
//	HERMES_PROJECT  Project ID        (required)
//	HERMES_IMAGE    Sandbox image tag (optional, for image-specific tests)
package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	sdk "github.com/VtrixAI/sandbox-go/src"
)

var (
	baseURL   = envOr("HERMES_URL", "http://localhost:8080")
	token     string // set in main from HERMES_TOKEN
	project   = envOr("HERMES_PROJECT", "local")
	testImage = envOr("HERMES_IMAGE", "harbor.ops.seaart.ai/seacloud-develop/nano-executor:latest")
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
		BaseURL:    baseURL,
		Token:      token,
		ProjectID:  project,
		HTTPClient: &http.Client{Timeout: 120 * time.Second},
	})
}

func main() {
	token = mustEnv("HERMES_TOKEN")
	ctx := context.Background()
	client := newClient()

	fmt.Printf("Target: %s  Project: %s\n", baseURL, project)

	// Admin (no sandbox needed)
	testPoolStatus(ctx, client)
	testRollingStatus(ctx, client)
	testRollingFull(ctx, client)
	testRollingStartWithImage(ctx, client)
	testRollingCancelWhenIdle(ctx, client)

	// List
	testList(ctx, client)

	// Create + Get (shared sandbox for subsequent tests)
	sandboxID := testCreateAndGet(ctx, client)
	if sandboxID != "" {
		testRefreshAndStatus(ctx, client, sandboxID)
		testExtend(ctx, client, sandboxID)
		testUpdate(ctx, client, sandboxID)
		testConfigure(ctx, client, sandboxID)
		client.Delete(ctx, sandboxID)
	}

	// Create variants
	testCreateWithSpec(ctx, client)
	testCreateWithPayloads(ctx, client)
	testCreateWithDefaultEnv(ctx, client)
	testCreateWithSpecImage(ctx, client)
	testCreateTTL(ctx, client)
	testCreateCombined(ctx, client)

	// Stop / Start / Restart / Delete flows
	testStopStartRestart(ctx, client)
	testStopNonBlocking(ctx, client)
	testStartOnActive(ctx, client)
	testStopOnStopped(ctx, client)
	testRestartOnStopped(ctx, client)
	testDelete(ctx, client)
	testDeleteNonExistent(ctx, client)
	testAttachNonExistent(ctx, client)
	testDeleteTwice(ctx, client)
	testGetEmptyID(ctx, client)

	// Update / Configure / Extend variants
	testUpdateEmptyOpts(ctx, client)
	testSpecPersistence(ctx, client)
	testUpdateStopped(ctx, client)
	testConfigureStopped(ctx, client)
	testExtendStopped(ctx, client)
	testUpdateImageOnly(ctx, client)
	testConfigurePayloadsOnly(ctx, client)
	testExtendSecondsOutOfRange(ctx, client)
	testExtendZeroVsExplicit(ctx, client)
	testExtendTimeoutEquivalence(ctx, client)

	// State machine boundary: delete stopped
	testDeleteStopped(ctx, client)

	// List edge cases
	testListCombinedFilter(ctx, client)
	testListPagination(ctx, client)
	testListLimitZero(ctx, client)

	// Edge cases
	testContextCancellation(ctx, client)
	testUseAfterClose(ctx, client)
	testConcurrentCreate(ctx, client)
	testConcurrentRefresh(ctx, client)
	testDomain(ctx, client)

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
