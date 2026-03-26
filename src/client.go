package sandbox

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

const (
	defaultPollInterval = 2 * time.Second
	defaultPollTimeout  = 3 * time.Minute
)

// Client is the entry point for all sandbox operations via the Hermes gateway.
type Client struct {
	baseURL   string
	token     string
	projectID string
	http      *http.Client
}

// ClientOptions configures a Client.
type ClientOptions struct {
	// BaseURL is the Hermes gateway URL, e.g. "http://hermes:8080".
	BaseURL string
	// Token is the Bearer token forwarded to Atlas.
	Token string
	// ProjectID is the X-Project-ID header value, used to identify the calling project.
	ProjectID string
	// HTTPClient overrides the default HTTP client.
	HTTPClient *http.Client
}

// NewClient creates a new sandbox Client.
func NewClient(opts ClientOptions) *Client {
	hc := opts.HTTPClient
	if hc == nil {
		hc = &http.Client{Timeout: 30 * time.Second}
	}
	return &Client{
		baseURL:   strings.TrimRight(opts.BaseURL, "/"),
		token:     opts.Token,
		projectID: opts.ProjectID,
		http:      hc,
	}
}

// ── Create ────────────────────────────────────────────────

// Create creates a new sandbox, polls until it is active, and opens a WebSocket
// connection to it. The returned *Sandbox is ready to use.
func (c *Client) Create(ctx context.Context, opts CreateOptions) (*Sandbox, error) {
	// 1. Create via Atlas (proxied through Hermes)
	info, err := c.createSandbox(ctx, opts)
	if err != nil {
		return nil, fmt.Errorf("create sandbox: %w", err)
	}

	// 2. Poll until status == "active"
	info, err = c.pollUntilActive(ctx, info.ID, opts)
	if err != nil {
		return nil, fmt.Errorf("poll sandbox: %w", err)
	}

	// 3. Open WS connect
	sb, err := c.connect(ctx, info, opts)
	if err != nil {
		return nil, fmt.Errorf("connect sandbox: %w", err)
	}

	return sb, nil
}

// Attach fetches an existing sandbox and opens a WebSocket connection to it.
// Use this when you already have a sandbox ID. Auth uses the client-level token.
func (c *Client) Attach(ctx context.Context, sandboxID string) (*Sandbox, error) {
	info, err := c.getSandbox(ctx, sandboxID, CreateOptions{})
	if err != nil {
		return nil, err
	}
	return c.connect(ctx, info, CreateOptions{})
}

// ── HTTP helpers ──────────────────────────────────────────

func (c *Client) createSandbox(ctx context.Context, opts CreateOptions) (Info, error) {
	body, err := json.Marshal(opts)
	if err != nil {
		return Info{}, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		c.baseURL+"/api/v1/sandbox/create", bytes.NewReader(body))
	if err != nil {
		return Info{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	c.applyAuth(req, opts)

	resp, err := c.http.Do(req)
	if err != nil {
		return Info{}, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var env atlasEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Info{}, fmt.Errorf("atlas response parse: %w", err)
	}
	if env.Code != 0 {
		return Info{}, fmt.Errorf("atlas error %d: %s", env.Code, env.Message)
	}

	var data createData
	if err := json.Unmarshal(env.Data, &data); err != nil {
		return Info{}, fmt.Errorf("atlas create data parse: %w", err)
	}
	return data.Sandbox, nil
}

func (c *Client) getSandbox(ctx context.Context, sandboxID string, opts CreateOptions) (Info, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		c.baseURL+"/api/v1/sandbox/"+sandboxID, nil)
	if err != nil {
		return Info{}, err
	}
	c.applyAuth(req, opts)

	resp, err := c.http.Do(req)
	if err != nil {
		return Info{}, err
	}
	defer resp.Body.Close()

	raw, _ := io.ReadAll(resp.Body)
	var env atlasEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return Info{}, fmt.Errorf("atlas response parse: %w", err)
	}
	if env.Code != 0 {
		return Info{}, fmt.Errorf("atlas error %d: %s", env.Code, env.Message)
	}

	var info Info
	if err := json.Unmarshal(env.Data, &info); err != nil {
		return Info{}, fmt.Errorf("sandbox info parse: %w", err)
	}
	return info, nil
}

// pollUntilActive polls GET /api/v1/sandbox/:id until status is "active" (or a
// terminal error status). Respects ctx cancellation and a built-in timeout.
func (c *Client) pollUntilActive(ctx context.Context, sandboxID string, opts CreateOptions) (Info, error) {
	pollCtx, cancel := context.WithTimeout(ctx, defaultPollTimeout)
	defer cancel()

	ticker := time.NewTicker(defaultPollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-pollCtx.Done():
			return Info{}, fmt.Errorf("timed out waiting for sandbox %s to become active", sandboxID)
		case <-ticker.C:
			info, err := c.getSandbox(pollCtx, sandboxID, opts)
			if err != nil {
				// transient — keep polling
				continue
			}
			switch info.Status {
			case "active":
				return info, nil
			case "stopped", "destroying":
				return Info{}, fmt.Errorf("sandbox %s entered terminal status %q", sandboxID, info.Status)
			}
			// creating / warming / warm — keep polling
		}
	}
}

// connect dials the Hermes /api/v1/sandbox/:id/connect WS endpoint.
func (c *Client) connect(ctx context.Context, info Info, opts CreateOptions) (*Sandbox, error) {
	u, err := url.Parse(c.baseURL)
	if err != nil {
		return nil, err
	}

	// Convert http(s) → ws(s)
	switch u.Scheme {
	case "https":
		u.Scheme = "wss"
	default:
		u.Scheme = "ws"
	}
	u.Path = fmt.Sprintf("/api/v1/sandbox/%s/connect", info.ID)

	headers := http.Header{}
	token := opts.Token
	if token == "" {
		token = c.token
	}
	if token != "" {
		headers.Set("Authorization", "Bearer "+token)
	}

	sid := opts.ProjectID
	if sid == "" {
		sid = c.projectID
	}
	if sid != "" {
		headers.Set("X-Project-ID", sid)
	}

	dialer := websocket.Dialer{HandshakeTimeout: 10 * time.Second}
	conn, _, err := dialer.DialContext(ctx, u.String(), headers)
	if err != nil {
		return nil, fmt.Errorf("ws dial: %w", err)
	}

	sb := &Sandbox{
		Info:       info,
		client:     c,
		conn:       conn,
		pending:    make(map[int64]*pendingCall),
		closed:     make(chan struct{}),
		defaultEnv: opts.Env,
	}
	go sb.readLoop()
	return sb, nil
}

// applyAuth sets Authorization and X-Project-ID headers from opts (falling back
// to Client-level defaults).
func (c *Client) applyAuth(req *http.Request, opts CreateOptions) {
	token := opts.Token
	if token == "" {
		token = c.token
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}

	sid := opts.ProjectID
	if sid == "" {
		sid = c.projectID
	}
	if sid != "" {
		req.Header.Set("X-Project-ID", sid)
	}
}
