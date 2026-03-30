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

// ── Admin types ───────────────────────────────────────────

// PoolStatus is the response from Client.PoolStatus.
type PoolStatus struct {
	Total    int `json:"total"`
	Warm     int `json:"warm"`
	Active   int `json:"active"`
	Creating int `json:"creating"`
	Deleting int `json:"deleting"`
	Deleted  int `json:"deleted"`

	WarmPoolSize int `json:"warm_pool_size"`
	MaxTotal     int `json:"max_total"`

	Utilization float64 `json:"utilization"`
	WarmRatio   float64 `json:"warm_ratio"`

	Healthy       bool   `json:"healthy"`
	HealthMessage string `json:"health_message,omitempty"`

	LastScaleAt    *time.Time `json:"last_scale_at,omitempty"`
	LastAllocateAt *time.Time `json:"last_allocate_at,omitempty"`
}

// RollingStatus is the response from rolling update endpoints.
type RollingStatus struct {
	ID          string `json:"id,omitempty"`
	Phase       string `json:"phase"`
	TargetImage string `json:"target_image,omitempty"`

	Progress      float64 `json:"progress"`
	WarmTotal     int     `json:"warm_total"`
	WarmUpdated   int     `json:"warm_updated"`
	ActiveTotal   int     `json:"active_total"`
	ActiveUpdated int     `json:"active_updated"`

	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	Duration    string     `json:"duration,omitempty"`

	Message string `json:"message,omitempty"`
	Error   string `json:"error,omitempty"`
}

// RollingStartOptions configures a rolling update.
type RollingStartOptions struct {
	// Image is the target image to roll out.
	// If empty, triggers a rolling restart using the current default image.
	Image string `json:"image,omitempty"`
}

// ── Admin methods on Client ───────────────────────────────

// PoolStatus returns the current sandbox pool status.
// Calls GET /admin/pool/status (proxied through Hermes to Atlas).
func (c *Client) PoolStatus(ctx context.Context) (*PoolStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/admin/pool/status", nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req, CreateOptions{})

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var env atlasEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("pool status parse: %w", err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("atlas error %d: %s", env.Code, env.Message)
	}

	var status PoolStatus
	if err := json.Unmarshal(env.Data, &status); err != nil {
		return nil, fmt.Errorf("pool status data parse: %w", err)
	}
	return &status, nil
}

// RollingStart initiates a rolling update to the given image.
// Calls POST /admin/rolling/start (proxied through Hermes to Atlas).
func (c *Client) RollingStart(ctx context.Context, opts RollingStartOptions) (*RollingStatus, error) {
	return c.doRollingPost(ctx, "/admin/rolling/start", opts)
}

// RollingStatus returns the current rolling update status.
// Calls GET /admin/rolling/status (proxied through Hermes to Atlas).
func (c *Client) RollingStatus(ctx context.Context) (*RollingStatus, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/admin/rolling/status", nil)
	if err != nil {
		return nil, err
	}
	c.applyAuth(req, CreateOptions{})

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var env atlasEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("rolling status parse: %w", err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("atlas error %d: %s", env.Code, env.Message)
	}

	var status RollingStatus
	if err := json.Unmarshal(env.Data, &status); err != nil {
		return nil, fmt.Errorf("rolling status data parse: %w", err)
	}
	return &status, nil
}

// RollingCancel cancels an in-progress rolling update.
// Calls POST /admin/rolling/cancel (proxied through Hermes to Atlas).
func (c *Client) RollingCancel(ctx context.Context) (*RollingStatus, error) {
	return c.doRollingPost(ctx, "/admin/rolling/cancel", nil)
}

// doRollingPost is shared by RollingStart and RollingCancel.
func (c *Client) doRollingPost(ctx context.Context, path string, body any) (*RollingStatus, error) {
	var req *http.Request
	var err error
	if body != nil {
		b, _ := json.Marshal(body)
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, bytes.NewReader(b))
		if err != nil {
			return nil, err
		}
		req.Header.Set("Content-Type", "application/json")
	} else {
		req, err = http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+path, nil)
		if err != nil {
			return nil, err
		}
	}
	c.applyAuth(req, CreateOptions{})

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var env atlasEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, fmt.Errorf("rolling response parse: %w", err)
	}
	if env.Code != 0 {
		return nil, fmt.Errorf("atlas error %d: %s", env.Code, env.Message)
	}

	var status RollingStatus
	if err := json.Unmarshal(env.Data, &status); err != nil {
		return nil, fmt.Errorf("rolling response data parse: %w", err)
	}
	return &status, nil
}
