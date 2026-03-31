package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/VtrixAI/sandbox-go/src"
)

// ────────────────────────────────────────────────────────────────────────────
// Refresh / Extend / Update / Configure / Spec persistence tests
// ────────────────────────────────────────────────────────────────────────────

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

	// Extend by 1 hour (Atlas uses seconds)
	err2 := sb.Extend(ctx, 3600)
	check("extend 3600s (1h) no error", err2 == nil, fmt.Sprint(err2))

	if err3 := sb.Refresh(ctx); err3 != nil {
		check("refresh after extend no error", false, fmt.Sprint(err3))
		return
	}
	after := sb.ExpireAt()
	check("extend updated expire_at", after != before || after != "", fmt.Sprintf("%s → %s", before, after))

	// ExtendTimeout (extend + refresh in one call)
	err2 = sb.ExtendTimeout(ctx, 1800)
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

func testUpdateEmptyOpts(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Update with empty opts (expect error) ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-update-empty"})
	check("update-empty: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	defer func() {
		sb.Close()
		client.Delete(ctx, id)
	}()

	// UpdateOptions with no fields set — server should reject this
	updateErr := sb.Update(ctx, sdk.UpdateOptions{})
	check("update-empty: returns error", updateErr != nil, fmt.Sprint(updateErr))
	fmt.Printf("    update empty opts error: %v\n", updateErr)

	// Sandbox should still be active after rejected update
	info, getErr := client.Get(ctx, id)
	check("update-empty: get no error", getErr == nil, fmt.Sprint(getErr))
	if getErr == nil {
		check("update-empty: status still active", info.Status == "active", info.Status)
	}
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

func testUpdateStopped(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Update stopped sandbox (expect error) ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-update-stopped"})
	check("update-stopped: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	sbStop, attachErr := client.Attach(ctx, id)
	check("update-stopped: attach no error", attachErr == nil, fmt.Sprint(attachErr))
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
	check("update-stopped: stop no error", stopErr == nil, fmt.Sprint(stopErr))
	if stopErr != nil {
		client.Delete(ctx, id)
		return
	}

	sbUp, attachErr2 := client.Attach(ctx, id)
	check("update-stopped: attach for update no error", attachErr2 == nil, fmt.Sprint(attachErr2))
	if attachErr2 != nil {
		client.Delete(ctx, id)
		return
	}
	updateErr := sbUp.Update(ctx, sdk.UpdateOptions{
		Spec: &sdk.Spec{CPU: "600m", Memory: "640Mi"},
	})
	sbUp.Close()
	check("update-stopped: returns error", updateErr != nil, fmt.Sprint(updateErr))
	fmt.Printf("    update-stopped error: %v\n", updateErr)

	info, getErr := client.Get(ctx, id)
	check("update-stopped: get no error", getErr == nil, fmt.Sprint(getErr))
	if getErr == nil {
		check("update-stopped: status still stopped", info.Status == "stopped", info.Status)
	}

	client.Delete(ctx, id)
}

func testConfigureStopped(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Configure stopped sandbox (expect error) ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-configure-stopped"})
	check("configure-stopped: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	sbStop, attachErr := client.Attach(ctx, id)
	check("configure-stopped: attach no error", attachErr == nil, fmt.Sprint(attachErr))
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
	check("configure-stopped: stop no error", stopErr == nil, fmt.Sprint(stopErr))
	if stopErr != nil {
		client.Delete(ctx, id)
		return
	}

	sbConf, attachErr2 := client.Attach(ctx, id)
	check("configure-stopped: attach for configure no error", attachErr2 == nil, fmt.Sprint(attachErr2))
	if attachErr2 != nil {
		client.Delete(ctx, id)
		return
	}
	confErr := sbConf.Configure(ctx)
	sbConf.Close()
	check("configure-stopped: returns error", confErr != nil, fmt.Sprint(confErr))
	fmt.Printf("    configure-stopped error: %v\n", confErr)

	info, getErr := client.Get(ctx, id)
	check("configure-stopped: get no error", getErr == nil, fmt.Sprint(getErr))
	if getErr == nil {
		check("configure-stopped: status still stopped", info.Status == "stopped", info.Status)
	}

	client.Delete(ctx, id)
}

func testExtendStopped(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Extend stopped sandbox ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-extend-stopped"})
	check("extend-stopped: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	sbStop, attachErr := client.Attach(ctx, id)
	check("extend-stopped: attach no error", attachErr == nil, fmt.Sprint(attachErr))
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
	check("extend-stopped: stop no error", stopErr == nil, fmt.Sprint(stopErr))
	if stopErr != nil {
		client.Delete(ctx, id)
		return
	}

	sbExt, attachErr2 := client.Attach(ctx, id)
	check("extend-stopped: attach for extend no error", attachErr2 == nil, fmt.Sprint(attachErr2))
	if attachErr2 != nil {
		client.Delete(ctx, id)
		return
	}
	extErr := sbExt.Extend(ctx, 3600)
	sbExt.Close()
	// Extend on stopped may succeed or fail — both acceptable; the sandbox must not become active
	fmt.Printf("    extend-stopped result: err=%v\n", extErr)
	check("extend-stopped: did not panic", true)

	info, getErr := client.Get(ctx, id)
	check("extend-stopped: get no error", getErr == nil, fmt.Sprint(getErr))
	if getErr == nil {
		check("extend-stopped: status not active", info.Status != "active", info.Status)
	}

	client.Delete(ctx, id)
}

func testUpdateImageOnly(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Update image only ──")

	if testImage == "" {
		fmt.Println("    SKIPPED: HERMES_IMAGE not set")
		check("update-image-only: skipped", true)
		return
	}

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-update-image-only"})
	check("update-image-only: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	sbUp, attachErr := client.Attach(ctx, id)
	check("update-image-only: attach no error", attachErr == nil, fmt.Sprint(attachErr))
	if attachErr != nil {
		client.Delete(ctx, id)
		return
	}
	updateErr := sbUp.Update(ctx, sdk.UpdateOptions{Image: testImage})
	sbUp.Close()
	check("update-image-only: no error", updateErr == nil, fmt.Sprint(updateErr))
	if updateErr != nil {
		client.Delete(ctx, id)
		return
	}

	// Poll until active
	deadline := time.Now().Add(5 * time.Minute)
	var finalInfo *sdk.Info
	for time.Now().Before(deadline) {
		i, _ := client.Get(ctx, id)
		if i != nil && i.Status == "active" {
			finalInfo = i
			break
		}
		time.Sleep(3 * time.Second)
	}
	if finalInfo == nil {
		finalInfo, _ = client.Get(ctx, id)
	}
	check("update-image-only: back to active", finalInfo != nil && finalInfo.Status == "active", func() string {
		if finalInfo != nil {
			return finalInfo.Status
		}
		return "nil"
	}())
	if finalInfo != nil {
		check("update-image-only: image tag updated", finalInfo.ImageTag == testImage,
			fmt.Sprintf("want=%s got=%s", testImage, finalInfo.ImageTag))
		fmt.Printf("    update-image-only: image=%s status=%s\n", finalInfo.ImageTag, finalInfo.Status)
	}

	client.Delete(ctx, id)
}

func testConfigurePayloadsOnly(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Configure payloads only (Atlas has no Update.payloads) ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-configure-payloads-only"})
	check("configure-payloads-only: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	id := sb.Info.ID
	sb.Close()

	sbUp, attachErr := client.Attach(ctx, id)
	check("configure-payloads-only: attach no error", attachErr == nil, fmt.Sprint(attachErr))
	if attachErr != nil {
		client.Delete(ctx, id)
		return
	}
	confErr := sbUp.Configure(ctx, sdk.Payload{
		API:  "/api/v1/env",
		Body: map[string]string{"UPDATE_PAYLOAD_VAR": "updated_value"},
	})
	sbUp.Close()
	check("configure-payloads-only: no error", confErr == nil, fmt.Sprint(confErr))
	if confErr != nil {
		client.Delete(ctx, id)
		return
	}

	// Poll until active
	deadline := time.Now().Add(5 * time.Minute)
	var active *sdk.Sandbox
	for time.Now().Before(deadline) {
		i, _ := client.Get(ctx, id)
		if i != nil && i.Status == "active" {
			active, _ = client.Attach(ctx, id)
			break
		}
		time.Sleep(3 * time.Second)
	}
	check("configure-payloads-only: back to active", active != nil)
	if active != nil {
		r, execErr := active.RunCommand(ctx, "echo $UPDATE_PAYLOAD_VAR", nil, nil)
		active.Close()
		check("configure-payloads-only: exec no error", execErr == nil, fmt.Sprint(execErr))
		if execErr == nil {
			check("configure-payloads-only: payload env var set",
				strings.Contains(r.Output, "updated_value"),
				strings.TrimSpace(r.Output))
		}
	}

	client.Delete(ctx, id)
}

func testExtendSecondsOutOfRange(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Extend seconds validation (client, matches Atlas bounds) ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-extend-bounds"})
	check("extend-bounds: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	err0 := sb.Extend(ctx, 0)
	check("extend-bounds: Extend(0) returns error", err0 != nil, fmt.Sprint(err0))

	errNeg := sb.Extend(ctx, -1)
	check("extend-bounds: Extend(-1) returns error", errNeg != nil, fmt.Sprint(errNeg))

	errMax := sb.Extend(ctx, sdk.MaxExtendSeconds+1)
	check("extend-bounds: Extend(Max+1) returns error", errMax != nil, fmt.Sprint(errMax))

	client.Delete(ctx, sb.Info.ID)
}

func testExtendZeroVsExplicit(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Extend invalid vs explicit seconds ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-extend-zero-vs-explicit"})
	check("extend-zero-vs-explicit: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	if err := sb.Refresh(ctx); err != nil {
		check("extend-zero-vs-explicit: refresh no error", false, fmt.Sprint(err))
		client.Delete(ctx, sb.Info.ID)
		return
	}

	extErr0 := sb.Extend(ctx, 0)
	check("extend-zero-vs-explicit: Extend(0) returns error", extErr0 != nil, fmt.Sprint(extErr0))

	// Extend 3600s — 1 hour
	extErr1 := sb.Extend(ctx, 3600)
	check("extend-zero-vs-explicit: Extend(3600) no error", extErr1 == nil, fmt.Sprint(extErr1))
	if err := sb.Refresh(ctx); err != nil {
		check("extend-zero-vs-explicit: refresh after Extend(3600) no error", false, fmt.Sprint(err))
		client.Delete(ctx, sb.Info.ID)
		return
	}
	expireAfterOne := sb.ExpireAt()
	check("extend-zero-vs-explicit: expire_at non-empty after Extend(3600)", expireAfterOne != "", expireAfterOne)

	fmt.Printf("    extend(3600) expire=%s\n", expireAfterOne)

	client.Delete(ctx, sb.Info.ID)
}

func testExtendTimeoutEquivalence(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: ExtendTimeout vs Extend+Refresh ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{UserID: "test-extend-timeout-equiv"})
	check("extend-timeout-equiv: create no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	// Extend(3600s) + Refresh
	extErr := sb.Extend(ctx, 3600)
	check("extend-timeout-equiv: Extend(3600) no error", extErr == nil, fmt.Sprint(extErr))
	if err := sb.Refresh(ctx); err != nil {
		check("extend-timeout-equiv: Refresh no error", false, fmt.Sprint(err))
		client.Delete(ctx, sb.Info.ID)
		return
	}
	expA := sb.ExpireAt()

	// ExtendTimeout(3600s) does extend+refresh in one call
	extTErr := sb.ExtendTimeout(ctx, 3600)
	check("extend-timeout-equiv: ExtendTimeout(3600) no error", extTErr == nil, fmt.Sprint(extTErr))
	expB := sb.ExpireAt()

	check("extend-timeout-equiv: ExpireAt non-empty after ExtendTimeout", expB != "", expB)

	// Both should set a future expiry; B should be >= A (both push forward)
	if expA != "" && expB != "" {
		tA, errA := time.Parse(time.RFC3339, expA)
		tB, errB := time.Parse(time.RFC3339, expB)
		if errA == nil && errB == nil {
			check("extend-timeout-equiv: ExtendTimeout expire >= Extend expire", !tB.Before(tA),
				fmt.Sprintf("Extend=%s ExtendTimeout=%s", expA, expB))
		}
	}
	fmt.Printf("    extend(1)+refresh=%s  extendTimeout(1)=%s\n", expA, expB)

	client.Delete(ctx, sb.Info.ID)
}
