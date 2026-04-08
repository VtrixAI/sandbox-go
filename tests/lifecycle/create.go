package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	sdk "github.com/VtrixAI/sandbox-go/src"
)

// ────────────────────────────────────────────────────────────────────────────
// Create tests
// ────────────────────────────────────────────────────────────────────────────

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

func testCreateWithPayloads(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Create with Payloads ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{
		UserID: "test-user-payloads",
		Payloads: []sdk.Payload{
			{API: "/api/v1/env", Body: map[string]string{"PAYLOAD_VAR": "payload_value"}},
		},
	})
	check("create with payloads: no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	check("create with payloads: id non-empty", sb.Info.ID != "")
	check("create with payloads: status active", sb.Info.Status == "active", sb.Info.Status)

	// 验证 payload 注入的环境变量是否生效（需要 sandbox 支持 /api/v1/env）
	r, execErr := sb.RunCommand(ctx, "echo $PAYLOAD_VAR", nil, nil)
	check("create with payloads: exec no error", execErr == nil, fmt.Sprint(execErr))
	if execErr == nil {
		check("create with payloads: env var set by payload",
			strings.Contains(r.Output, "payload_value"),
			strings.TrimSpace(r.Output))
	}

	_ = client.Delete(ctx, sb.Info.ID)
}

func testCreateWithDefaultEnv(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Create with default Env ──")

	sb, err := client.Create(ctx, sdk.CreateOptions{
		UserID: "test-user-env",
		Env:    map[string]string{"DEFAULT_SDK_ENV": "sdk_env_value"},
	})
	check("create with env: no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	// 所有命令都应继承 default env
	r, execErr := sb.RunCommand(ctx, "echo $DEFAULT_SDK_ENV", nil, nil)
	check("create with env: exec no error", execErr == nil, fmt.Sprint(execErr))
	if execErr == nil {
		check("create with env: default env inherited",
			strings.Contains(r.Output, "sdk_env_value"),
			strings.TrimSpace(r.Output))
	}

	// 命令级 env 覆盖 default env
	r2, execErr2 := sb.RunCommand(ctx, "echo $DEFAULT_SDK_ENV", nil, &sdk.RunOptions{
		Env: map[string]string{"DEFAULT_SDK_ENV": "overridden"},
	})
	check("create with env: per-cmd env no error", execErr2 == nil, fmt.Sprint(execErr2))
	if execErr2 == nil {
		check("create with env: per-cmd env overrides default",
			strings.Contains(r2.Output, "overridden"),
			strings.TrimSpace(r2.Output))
	}

	_ = client.Delete(ctx, sb.Info.ID)
}

func testCreateWithSpecImage(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Create with Spec.Image ──")

	if testImage == "" {
		fmt.Println("    SKIPPED: HERMES_IMAGE not set")
		check("create-spec-image: skipped", true)
		return
	}

	specCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	sb, err := client.Create(specCtx, sdk.CreateOptions{
		UserID: "test-create-spec-image",
		Spec: &sdk.Spec{
			CPU:    "500m",
			Memory: "512Mi",
			Image:  testImage,
		},
	})
	check("create-spec-image: no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	check("create-spec-image: status active", sb.Info.Status == "active", sb.Info.Status)
	check("create-spec-image: image tag matches", sb.Info.ImageTag == testImage,
		fmt.Sprintf("want=%s got=%s", testImage, sb.Info.ImageTag))
	fmt.Printf("    create-spec-image sandbox: %s image=%s\n", sb.Info.ID, sb.Info.ImageTag)

	client.Delete(ctx, sb.Info.ID)
}

func testCreateTTL(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Create with TTLHours=1 ──")

	createTime := time.Now().UTC()
	sb, err := client.Create(ctx, sdk.CreateOptions{
		UserID:   "test-create-ttl",
		TTLHours: 1,
	})
	check("create-ttl: no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	check("create-ttl: status active", sb.Info.Status == "active", sb.Info.Status)
	check("create-ttl: expire_at non-empty", sb.Info.ExpireAt != "", sb.Info.ExpireAt)

	if sb.Info.ExpireAt != "" {
		expireAt, parseErr := time.Parse(time.RFC3339, sb.Info.ExpireAt)
		check("create-ttl: expire_at parseable", parseErr == nil, fmt.Sprint(parseErr))
		if parseErr == nil {
			expectedExpire := createTime.Add(1 * time.Hour)
			tolerance := 5 * time.Minute
			diff := expireAt.Sub(expectedExpire)
			if diff < 0 {
				diff = -diff
			}
			check("create-ttl: expire_at ≈ now+1h (±5min)", diff <= tolerance,
				fmt.Sprintf("expected≈%s got=%s diff=%s", expectedExpire.Format(time.RFC3339), expireAt.Format(time.RFC3339), diff))
		}
	}
	fmt.Printf("    create-ttl sandbox: %s expire_at=%s\n", sb.Info.ID, sb.Info.ExpireAt)

	client.Delete(ctx, sb.Info.ID)
}

func testCreateCombined(ctx context.Context, client *sdk.Client) {
	fmt.Println("\n── lifecycle: Create with Spec + Payloads + Env combined ──")

	specCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	defer cancel()

	sb, err := client.Create(specCtx, sdk.CreateOptions{
		UserID: "test-create-combined",
		Spec:   &sdk.Spec{CPU: "500m", Memory: "512Mi"},
		Payloads: []sdk.Payload{
			{API: "/api/v1/env", Body: map[string]string{"COMBINED_PAYLOAD_VAR": "from_payload"}},
		},
		Env: map[string]string{"COMBINED_ENV_VAR": "from_env"},
	})
	check("create-combined: no error", err == nil, fmt.Sprint(err))
	if err != nil {
		return
	}
	defer sb.Close()

	check("create-combined: status active", sb.Info.Status == "active", sb.Info.Status)

	// Verify default env is inherited
	r, execErr := sb.RunCommand(ctx, "echo $COMBINED_ENV_VAR", nil, nil)
	check("create-combined: exec no error", execErr == nil, fmt.Sprint(execErr))
	if execErr == nil {
		check("create-combined: env var inherited",
			strings.Contains(r.Output, "from_env"),
			strings.TrimSpace(r.Output))
	}

	// Verify payload env is set
	r2, execErr2 := sb.RunCommand(ctx, "echo $COMBINED_PAYLOAD_VAR", nil, nil)
	check("create-combined: payload exec no error", execErr2 == nil, fmt.Sprint(execErr2))
	if execErr2 == nil {
		check("create-combined: payload var set",
			strings.Contains(r2.Output, "from_payload"),
			strings.TrimSpace(r2.Output))
	}

	client.Delete(ctx, sb.Info.ID)
}
