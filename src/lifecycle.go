package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// ── Client lifecycle methods ──────────────────────────────

// List queries sandboxes via Atlas (proxied through Hermes).
func (c *Client) List(ctx context.Context, opts ListOptions) (*ListResult, error) {
	body, _ := json.Marshal(opts)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/sandbox/list", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req, CreateOptions{})

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var env atlasEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("list parse: %w", err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("atlas error %d: %s", env.Code, env.Message)
	}

	var result ListResult
	if err := json.Unmarshal(env.Data, &result); err != nil {
		return nil, fmt.Errorf("list data parse: %w", err)
	}
	return &result, nil
}

// Get fetches a sandbox by ID without opening a WebSocket connection.
func (c *Client) Get(ctx context.Context, sandboxID string) (*Info, error) {
	info, err := c.getSandbox(ctx, sandboxID, CreateOptions{})
	if err != nil {
		return nil, err
	}
	return &info, nil
}

// Delete permanently deletes a sandbox.
func (c *Client) Delete(ctx context.Context, sandboxID string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete,
		c.baseURL+"/api/v1/sandbox/"+sandboxID, nil)
	if err != nil {
		return err
	}
	c.applyAuth(req, CreateOptions{})
	return c.doSimple(req)
}

// ── Sandbox lifecycle methods ─────────────────────────────

// Refresh re-fetches this sandbox's metadata from Atlas and updates sb.Info.
func (sb *Sandbox) Refresh(ctx context.Context) error {
	info, err := sb.client.getSandbox(ctx, sb.Info.ID, CreateOptions{})
	if err != nil {
		return err
	}
	sb.Info = info
	return nil
}

// Stop pauses the sandbox without deleting it.
// If opts.Blocking is true, polls until status is "stopped" or "failed".
func (sb *Sandbox) Stop(ctx context.Context, opts *StopOptions) error {
	if err := sb.client.doPost(ctx, "/api/v1/sandbox/"+sb.Info.ID+"/stop", nil); err != nil {
		return err
	}
	if opts == nil || !opts.Blocking {
		return nil
	}

	interval := opts.PollInterval
	if interval <= 0 {
		interval = 2 * time.Second
	}
	deadline := opts.Timeout
	if deadline <= 0 {
		deadline = 5 * time.Minute
	}

	ctx, cancel := context.WithTimeout(ctx, deadline)
	defer cancel()

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return fmt.Errorf("stop timeout: sandbox %s did not reach stopped state", sb.Info.ID)
		case <-ticker.C:
			info, err := sb.client.Get(ctx, sb.Info.ID)
			if err != nil {
				return err
			}
			sb.Info = *info
			if info.Status == "stopped" || info.Status == "failed" {
				return nil
			}
		}
	}
}

// Start resumes a stopped sandbox.
func (sb *Sandbox) Start(ctx context.Context) error {
	return sb.client.doPost(ctx, "/api/v1/sandbox/"+sb.Info.ID+"/start", nil)
}

// Restart restarts the sandbox.
func (sb *Sandbox) Restart(ctx context.Context) error {
	return sb.client.doPost(ctx, "/api/v1/sandbox/"+sb.Info.ID+"/restart", nil)
}

// Extend extends the sandbox TTL by the given duration in seconds (Atlas POST .../extend, field "seconds").
// Seconds must be in (0, MaxExtendSeconds].
func (sb *Sandbox) Extend(ctx context.Context, seconds int) error {
	if err := validateExtendSeconds(seconds); err != nil {
		return err
	}
	return sb.client.doPost(ctx, "/api/v1/sandbox/"+sb.Info.ID+"/extend",
		map[string]int{"seconds": seconds})
}

// ExtendTimeout extends the sandbox TTL by seconds and refreshes Info.
func (sb *Sandbox) ExtendTimeout(ctx context.Context, seconds int) error {
	if err := sb.Extend(ctx, seconds); err != nil {
		return err
	}
	return sb.Refresh(ctx)
}

// Status returns the current status from the cached Info (call Refresh first for live data).
func (sb *Sandbox) Status() string { return sb.Info.Status }

// CreatedAt returns the sandbox creation time parsed from the cached Info.CreatedAt RFC3339 string.
// Returns zero time if the field is empty or unparsable.
func (sb *Sandbox) CreatedAt() time.Time {
	if sb.Info.CreatedAt == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, sb.Info.CreatedAt)
	if err != nil {
		return time.Time{}
	}
	return t
}

// ExpireAt returns the sandbox expiry time from the cached Info (RFC3339).
func (sb *Sandbox) ExpireAt() string { return sb.Info.ExpireAt }

// Timeout returns the remaining sandbox lifetime in milliseconds based on the
// cached Info.ExpireAt field. Returns 0 if ExpireAt is empty or already past.
// Call Refresh first to get a fresh value.
func (sb *Sandbox) Timeout() int64 {
	if sb.Info.ExpireAt == "" {
		return 0
	}
	t, err := time.Parse(time.RFC3339, sb.Info.ExpireAt)
	if err != nil {
		return 0
	}
	ms := time.Until(t).Milliseconds()
	if ms < 0 {
		return 0
	}
	return ms
}

// Update patches the sandbox spec/image (Atlas PATCH /api/v1/sandbox/:id). At least one field in opts must be set.
func (sb *Sandbox) Update(ctx context.Context, opts UpdateOptions) error {
	body, _ := json.Marshal(opts)
	req, err := http.NewRequestWithContext(ctx, http.MethodPatch,
		sb.client.baseURL+"/api/v1/sandbox/"+sb.Info.ID, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	sb.client.applyAuth(req, CreateOptions{})
	return sb.client.doSimple(req)
}

// Configure immediately applies the current sandbox configuration to the pod.
// Optionally pass payloads to override the stored payloads for this apply.
func (sb *Sandbox) Configure(ctx context.Context, payloads ...Payload) error {
	var body any
	if len(payloads) > 0 {
		body = map[string]any{"payloads": payloads}
	}
	return sb.client.doPost(ctx, "/api/v1/sandbox/"+sb.Info.ID+"/configure", body)
}

// Delete permanently deletes this sandbox.
func (sb *Sandbox) Delete(ctx context.Context) error {
	return sb.client.Delete(ctx, sb.Info.ID)
}

func validateExtendSeconds(seconds int) error {
	if seconds <= 0 {
		return fmt.Errorf("extend: seconds must be greater than 0")
	}
	if seconds > MaxExtendSeconds {
		return fmt.Errorf("extend: seconds cannot exceed %d", MaxExtendSeconds)
	}
	return nil
}

// ── Shared HTTP helpers ───────────────────────────────────

func (c *Client) doPost(ctx context.Context, path string, body any) error {
	var req *http.Request
	var err error
	if body != nil {
		b, _ := json.Marshal(body)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost,
			c.baseURL+path, bytes.NewReader(b))
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
	}
	if err != nil {
		return err
	}
	c.applyAuth(req, CreateOptions{})
	return c.doSimple(req)
}

func (c *Client) doSimple(req *http.Request) error {
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	var env atlasEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return fmt.Errorf("response parse: %w", err)
	}
	if env.Code != 0 {
		return fmt.Errorf("atlas error %d: %s", env.Code, env.Message)
	}
	return nil
}
