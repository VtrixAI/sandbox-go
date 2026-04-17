package sandbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	defaultTimeout  = 300
	defaultTemplate = "base"
)

// Sandbox represents a live sandbox instance.
type Sandbox struct {
	SandboxID     string
	SandboxDomain string // domain where the sandbox is hosted, e.g. "e2b.app"
	Files         *Filesystem
	Commands      *Commands
	Pty           *Pty

	cfg    ConnectionConfig
	client *http.Client
}

// ---- internal helpers ----

// doManagement performs an authenticated request to the hermes management API.
func (s *Sandbox) doManagement(method, path string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	url := strings.TrimRight(s.cfg.BaseURL, "/") + path
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-API-Key", s.cfg.APIKey)

	resp, err := s.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	return resp, nil
}

// readBody reads the full response body and closes it.
func readBody(resp *http.Response) ([]byte, error) {
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

// checkResponse returns a typed error if the response status indicates failure.
func checkResponse(resp *http.Response) error {
	if resp.StatusCode >= 200 && resp.StatusCode < 300 {
		return nil
	}
	body, _ := readBody(resp)
	return parseAPIError(resp.StatusCode, body)
}

// newClient returns an http.Client with the given timeout.
func newClient(timeout time.Duration) *http.Client {
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	return &http.Client{Timeout: timeout}
}

// ---- wire types for Management API ----

type createRequest struct {
	TemplateID string            `json:"templateID"`
	Timeout    int               `json:"timeout"`
	Metadata   map[string]string `json:"metadata,omitempty"`
	EnvVars    map[string]string `json:"envVars,omitempty"`
}

// sandboxResponse maps Atlas E2BSandboxDetail / E2BSandbox responses.
type sandboxResponse struct {
	SandboxID       string            `json:"sandboxID"`
	TemplateID      string            `json:"templateID"`
	Alias           string            `json:"alias"`
	StartedAt       string            `json:"startedAt"`
	EndAt           string            `json:"endAt"`
	Metadata        map[string]string `json:"metadata"`
	Status          string            `json:"status"` // list API
	State           string            `json:"state"`  // detail API
	EnvdAccessToken string            `json:"envdAccessToken"`
	EnvdUrl         string            `json:"envdUrl"`
}

func (r *sandboxResponse) toSandboxInfo() SandboxInfo {
	state := r.State
	if state == "" {
		state = r.Status
	}
	info := SandboxInfo{
		SandboxID:  r.SandboxID,
		TemplateID: r.TemplateID,
		Alias:      r.Alias,
		Metadata:   r.Metadata,
		State:      state,
	}
	if t, err := time.Parse(time.RFC3339, r.StartedAt); err == nil {
		info.StartedAt = t
	}
	if t, err := time.Parse(time.RFC3339, r.EndAt); err == nil {
		info.EndAt = t
	}
	return info
}

// buildSandbox constructs a Sandbox value from a response and the original opts.
// Returns an error if envdUrl is empty — direct sandbox access requires a configured
// envd_base_domain; there is no hermes-proxy fallback.
func buildSandbox(r *sandboxResponse, opts SandboxOpts) (*Sandbox, error) {
	if r.EnvdUrl == "" {
		return nil, fmt.Errorf("sandbox %s has no envdUrl: envd_base_domain is not configured on the server", r.SandboxID)
	}
	cfg := ConnectionConfig{
		SandboxID:   r.SandboxID,
		EnvdURL:     r.EnvdUrl,
		AccessToken: r.EnvdAccessToken,
		APIKey:      opts.APIKey,
		BaseURL:     opts.BaseURL,
		Timeout:     30 * time.Second,
	}
	client := newClient(cfg.Timeout)

	s := &Sandbox{
		SandboxID:     r.SandboxID,
		SandboxDomain: sandboxDomain(opts.BaseURL),
		cfg:           cfg,
		client:        client,
	}
	s.Files = &Filesystem{cfg: cfg, client: client}
	s.Commands = &Commands{cfg: cfg, client: client}
	s.Pty = &Pty{cfg: cfg, client: client}
	return s, nil
}

// sandboxDomain extracts the bare hostname from a base URL.
// "https://api.example.com" → "api.example.com"
func sandboxDomain(baseURL string) string {
	d := strings.TrimPrefix(baseURL, "https://")
	d = strings.TrimPrefix(d, "http://")
	return strings.TrimRight(d, "/")
}

// NewFromConfig constructs a Sandbox directly from a ConnectionConfig,
// bypassing the management API (Create / Connect).
// This is primarily intended for testing and local development.
func NewFromConfig(cfg ConnectionConfig) *Sandbox {
	if cfg.Timeout == 0 {
		cfg.Timeout = 30 * time.Second
	}
	client := newClient(cfg.Timeout)
	s := &Sandbox{
		SandboxID:     cfg.SandboxID,
		SandboxDomain: sandboxDomain(cfg.BaseURL),
		cfg:           cfg,
		client:        client,
	}
	s.Files = &Filesystem{cfg: cfg, client: client}
	s.Commands = &Commands{cfg: cfg, client: client}
	s.Pty = &Pty{cfg: cfg, client: client}
	return s
}

// ---- Public API ----

// Create creates a new sandbox and returns a connected Sandbox value.
func Create(opts SandboxOpts) (*Sandbox, error) {
	if opts.Timeout == 0 {
		opts.Timeout = defaultTimeout
	}
	if opts.Template == "" {
		opts.Template = defaultTemplate
	}

	reqBody := createRequest{
		TemplateID: opts.Template,
		Timeout:    opts.Timeout,
		Metadata:   opts.Metadata,
		EnvVars:    opts.Envs,
	}

	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal create request: %w", err)
	}

	url := strings.TrimRight(opts.BaseURL, "/") + "/api/v1/sandboxes"
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-API-Key", opts.APIKey)

	client := newClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /api/v1/sandboxes: %w", err)
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	var sr sandboxResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("decode create response: %w", err)
	}

	sb, err := buildSandbox(&sr, opts)
	if err != nil {
		return nil, err
	}

	// Wait for the sandbox to become ready (state: "running" / "active").
	// Atlas returns immediately after scheduling — the pod may still be starting.
	if err := waitUntilReady(sb, opts, 120*time.Second); err != nil {
		return nil, err
	}

	return sb, nil
}

// Connect connects to an existing sandbox by ID.
// If the sandbox is paused, it is resumed via POST /connect.
func Connect(sandboxID string, opts SandboxOpts) (*Sandbox, error) {
	url := strings.TrimRight(opts.BaseURL, "/") + "/api/v1/sandboxes/" + sandboxID
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	req.Header.Set("X-API-Key", opts.APIKey)

	client := newClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /api/v1/sandboxes/%s: %w", sandboxID, err)
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	var sr sandboxResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("decode sandbox response: %w", err)
	}

	s, err := buildSandbox(&sr, opts)
	if err != nil {
		return nil, err
	}

	// Resume if paused
	sbState := sr.State
	if sbState == "" {
		sbState = sr.Status
	}
	if sbState == "paused" || sbState == "stopped" {
		resumeURL := strings.TrimRight(opts.BaseURL, "/") + "/api/v1/sandboxes/" + sandboxID + "/connect"
		resumeReq, err := http.NewRequest(http.MethodPost, resumeURL, strings.NewReader(`{"timeout":300}`))
		if err != nil {
			return nil, fmt.Errorf("create resume request: %w", err)
		}
		resumeReq.Header.Set("Content-Type", "application/json")
		resumeReq.Header.Set("X-API-Key", opts.APIKey)
		resumeResp, err := client.Do(resumeReq)
		if err != nil {
			return nil, fmt.Errorf("POST /api/v1/sandboxes/%s/connect: %w", sandboxID, err)
		}
		if err := checkResponse(resumeResp); err != nil {
			return nil, fmt.Errorf("resume sandbox: %w", err)
		}

		// POST /connect triggers doInit on atlas, which generates a new envdAccessToken
		// for the new pod and persists it to Redis. Re-fetch sandbox detail so that
		// s.cfg.AccessToken reflects the new token; using the pre-resume token would
		// cause all direct nano-executor RPC calls to return 401.
		freshReq, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			return nil, fmt.Errorf("create re-fetch request after resume: %w", err)
		}
		freshReq.Header.Set("X-API-Key", opts.APIKey)
		freshResp, err := client.Do(freshReq)
		if err != nil {
			return nil, fmt.Errorf("GET /api/v1/sandboxes/%s after resume: %w", sandboxID, err)
		}
		freshBody, _ := readBody(freshResp)
		if freshResp.StatusCode < 200 || freshResp.StatusCode >= 300 {
			return nil, parseAPIError(freshResp.StatusCode, freshBody)
		}
		var freshSr sandboxResponse
		if err := json.Unmarshal(freshBody, &freshSr); err != nil {
			return nil, fmt.Errorf("decode sandbox response after resume: %w", err)
		}
		// Propagate the fresh token to all sub-clients.
		s.cfg.AccessToken = freshSr.EnvdAccessToken
		s.Files.cfg.AccessToken = freshSr.EnvdAccessToken
		s.Commands.cfg.AccessToken = freshSr.EnvdAccessToken
		s.Pty.cfg.AccessToken = freshSr.EnvdAccessToken
	}

	return s, nil
}

// Kill terminates the sandbox.
func (s *Sandbox) Kill() error {
	resp, err := s.doManagement(http.MethodDelete, "/api/v1/sandboxes/"+s.SandboxID, nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// SetTimeout updates the sandbox timeout (seconds).
func (s *Sandbox) SetTimeout(seconds int) error {
	body := map[string]int{"timeout": seconds}
	resp, err := s.doManagement(http.MethodPost, "/api/v1/sandboxes/"+s.SandboxID+"/timeout", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// GetInfo retrieves current metadata about the sandbox.
func (s *Sandbox) GetInfo() (*SandboxInfo, error) {
	resp, err := s.doManagement(http.MethodGet, "/api/v1/sandboxes/"+s.SandboxID, nil)
	if err != nil {
		return nil, err
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	var sr sandboxResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("decode sandbox info: %w", err)
	}
	info := sr.toSandboxInfo()
	return &info, nil
}

// IsRunning returns true if the sandbox status is running/active.
func (s *Sandbox) IsRunning() bool {
	info, err := s.GetInfo()
	if err != nil {
		return false
	}
	return info.State == "running" || info.State == "active"
}

// GetHost returns the proxy host for a port inside the sandbox.
// Format: <port>-<sandboxId>.<domain>
func (s *Sandbox) GetHost(port int) string {
	if s.SandboxDomain == "" {
		return fmt.Sprintf("%d-%s.sandbox", port, s.SandboxID)
	}
	return fmt.Sprintf("%d-%s.%s", port, s.SandboxID, s.SandboxDomain)
}

// DownloadURL returns a short-lived signed URL for directly downloading a file
// from the sandbox. The URL bypasses hermes and hits the nano-executor directly.
// Returns the URL string directly, matching the e2b SDK interface.
func (s *Sandbox) DownloadURL(path string, opts ...SandboxUrlOpts) (string, error) {
	return s.signedFileURL(path, "download-url", opts...)
}

// UploadURL returns a short-lived signed URL for directly uploading a file
// into the sandbox. Send a POST request to this URL with the file as multipart/form-data.
// Returns the URL string directly, matching the e2b SDK interface.
func (s *Sandbox) UploadURL(path string, opts ...SandboxUrlOpts) (string, error) {
	return s.signedFileURL(path, "upload-url", opts...)
}

func (s *Sandbox) signedFileURL(path, endpoint string, opts ...SandboxUrlOpts) (string, error) {
	expires := 300
	if len(opts) > 0 && opts[0].UseSignatureExpiration > 0 {
		expires = opts[0].UseSignatureExpiration
	}
	u := strings.TrimRight(s.cfg.BaseURL, "/") +
		"/api/v1/sandboxes/" + s.SandboxID +
		"/exec/files/" + endpoint +
		"?path=" + url.QueryEscape(path) +
		"&expires=" + fmt.Sprintf("%d", expires)
	if len(opts) > 0 && opts[0].User != "" {
		u += "&username=" + url.QueryEscape(opts[0].User)
	}

	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return "", fmt.Errorf("create signed URL request: %w", err)
	}
	if s.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", s.cfg.APIKey)
	}

	resp, err := s.client.Do(req)
	if err != nil {
		return "", fmt.Errorf("GET /exec/files/%s: %w", endpoint, err)
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", parseAPIError(resp.StatusCode, body)
	}

	var result struct {
		URL string `json:"url"`
	}
	if err := json.Unmarshal(body, &result); err != nil {
		return "", fmt.Errorf("decode signed URL response: %w", err)
	}
	return result.URL, nil
}

// SandboxMetrics holds CPU and memory utilization for a sandbox.
type SandboxMetrics struct {
	CPUUsedPct float64 // percentage 0–100
	MemUsedMiB float64 // megabytes
}

// GetMetrics fetches current CPU and memory utilization for the sandbox.
func (s *Sandbox) GetMetrics() (*SandboxMetrics, error) {
	resp, err := s.doManagement(http.MethodGet, "/api/v1/sandboxes/"+s.SandboxID+"/metrics", nil)
	if err != nil {
		return nil, err
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	var raw struct {
		CPUUsedPct float64 `json:"cpuUsedPct"`
		MemUsedMiB float64 `json:"memUsedMiB"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode metrics: %w", err)
	}
	return &SandboxMetrics{CPUUsedPct: raw.CPUUsedPct, MemUsedMiB: raw.MemUsedMiB}, nil
}

// BetaPause pauses (snapshots) the sandbox. Resume later with Connect.
func (s *Sandbox) BetaPause() error {
	resp, err := s.doManagement(http.MethodPost, "/api/v1/sandboxes/"+s.SandboxID+"/pause", nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// Refresh extends the sandbox lifetime by duration seconds (max 3600).
// Pass 0 to use the server default.
func (s *Sandbox) Refresh(duration int) error {
	var body map[string]int
	if duration > 0 {
		body = map[string]int{"duration": duration}
	}
	resp, err := s.doManagement(http.MethodPost, "/api/v1/sandboxes/"+s.SandboxID+"/refreshes", body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// ResizeDisk expands the sandbox disk to sizeMB megabytes.
// Sends PATCH /api/v1/sandboxes/:id with {"spec":{"storage_size":"<n>Gi"}}
// (or "<n>Mi" when sizeMB is not a whole number of gibibytes).
// Atlas performs an in-place PVC expansion — the sandbox does not restart.
func (s *Sandbox) ResizeDisk(sizeMB int) error {
	if sizeMB <= 0 {
		return fmt.Errorf("resize disk: sizeMB must be positive")
	}
	storageSize := mbToStorageSize(sizeMB)
	body := map[string]interface{}{
		"spec": map[string]string{"storage_size": storageSize},
	}
	resp, err := s.doManagement(http.MethodPatch, "/api/v1/sandboxes/"+s.SandboxID, body)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// List returns all sandboxes visible to the caller.
func List(opts SandboxOpts) ([]SandboxInfo, error) {
	page, err := ListPage(SandboxListOpts{APIKey: opts.APIKey, BaseURL: opts.BaseURL})
	if err != nil {
		return nil, err
	}
	return page.Items, nil
}

// ListPage returns one page of sandboxes.
// Use SandboxListOpts.Limit and SandboxListOpts.PageToken to paginate.
// When HasMore is true, pass the returned Next token as PageToken for the next call.
func ListPage(opts SandboxListOpts) (*SandboxPage, error) {
	u := strings.TrimRight(opts.BaseURL, "/") + "/api/v1/sandboxes"
	if opts.Limit > 0 || opts.PageToken != "" {
		q := "?"
		if opts.Limit > 0 {
			q += fmt.Sprintf("limit=%d&", opts.Limit)
		}
		if opts.PageToken != "" {
			q += "page_token=" + opts.PageToken
		}
		u += strings.TrimRight(q, "&")
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create http request: %w", err)
	}
	req.Header.Set("X-API-Key", opts.APIKey)

	client := newClient(30 * time.Second)
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /api/v1/sandboxes: %w", err)
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	// Atlas may return either a plain array or a paginated envelope.
	// Try envelope first; fall back to plain array.
	var envelope struct {
		Items     []sandboxResponse `json:"items"`
		NextToken string            `json:"nextToken"`
		HasMore   bool              `json:"hasMore"`
	}
	if err := json.Unmarshal(body, &envelope); err == nil && envelope.Items != nil {
		items := make([]SandboxInfo, len(envelope.Items))
		for i, r := range envelope.Items {
			items[i] = r.toSandboxInfo()
		}
		return &SandboxPage{Items: items, HasMore: envelope.HasMore, Next: envelope.NextToken}, nil
	}

	var raw []sandboxResponse
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode list response: %w", err)
	}
	items := make([]SandboxInfo, len(raw))
	for i, r := range raw {
		items[i] = r.toSandboxInfo()
	}
	return &SandboxPage{Items: items, HasMore: false, Next: ""}, nil
}

// waitUntilReady polls GetInfo until the sandbox state is "running" or "active",
// or until the timeout is exceeded.
func waitUntilReady(sb *Sandbox, opts SandboxOpts, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		info, err := sb.GetInfo()
		if err == nil && (info.State == "running" || info.State == "active") {
			return nil
		}
		time.Sleep(1 * time.Second)
	}
	return fmt.Errorf("sandbox %s did not become ready within %s", sb.SandboxID, timeout)
}

// ---- Static management helpers (no Sandbox instance required) ----

// ListMetrics returns CPU/memory metrics for all running sandboxes.
func ListMetrics(opts SandboxOpts) ([]SandboxMetrics, error) {
	u := strings.TrimRight(opts.BaseURL, "/") + "/api/v1/sandboxes/metrics"
	resp, err := doStaticManagement(http.MethodGet, u, opts.APIKey, nil)
	if err != nil {
		return nil, err
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}
	var raw []struct {
		CPUUsedPct float64 `json:"cpuUsedPct"`
		MemUsedMiB float64 `json:"memUsedMiB"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode list metrics: %w", err)
	}
	out := make([]SandboxMetrics, len(raw))
	for i, r := range raw {
		out[i] = SandboxMetrics{CPUUsedPct: r.CPUUsedPct, MemUsedMiB: r.MemUsedMiB}
	}
	return out, nil
}


// doStaticManagement performs an authenticated management request without a Sandbox instance.
func doStaticManagement(method, url string, apiKey string, body interface{}) (*http.Response, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	req.Header.Set("X-API-Key", apiKey)
	client := newClient(30 * time.Second)
	return client.Do(req)
}

// KillSandbox terminates the sandbox identified by sandboxID without requiring a Sandbox instance.
func KillSandbox(sandboxID string, opts SandboxOpts) error {
	url := strings.TrimRight(opts.BaseURL, "/") + "/api/v1/sandboxes/" + sandboxID
	resp, err := doStaticManagement(http.MethodDelete, url, opts.APIKey, nil)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// GetSandboxInfo returns metadata about the sandbox identified by sandboxID.
func GetSandboxInfo(sandboxID string, opts SandboxOpts) (*SandboxInfo, error) {
	url := strings.TrimRight(opts.BaseURL, "/") + "/api/v1/sandboxes/" + sandboxID
	resp, err := doStaticManagement(http.MethodGet, url, opts.APIKey, nil)
	if err != nil {
		return nil, err
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}
	var sr sandboxResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("decode sandbox info: %w", err)
	}
	info := sr.toSandboxInfo()
	return &info, nil
}

// SetSandboxTimeout updates the lifetime of the sandbox identified by sandboxID (seconds).
func SetSandboxTimeout(sandboxID string, seconds int, opts SandboxOpts) error {
	url := strings.TrimRight(opts.BaseURL, "/") + "/api/v1/sandboxes/" + sandboxID + "/timeout"
	resp, err := doStaticManagement(http.MethodPost, url, opts.APIKey, map[string]int{"timeout": seconds})
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// GetSandboxMetrics returns the CPU and memory metrics for the sandbox identified by sandboxID.
func GetSandboxMetrics(sandboxID string, opts SandboxOpts) (*SandboxMetrics, error) {
	url := strings.TrimRight(opts.BaseURL, "/") + "/api/v1/sandboxes/" + sandboxID + "/metrics"
	resp, err := doStaticManagement(http.MethodGet, url, opts.APIKey, nil)
	if err != nil {
		return nil, err
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}
	var raw struct {
		CPUUsedPct float64 `json:"cpuUsedPct"`
		MemUsedMiB float64 `json:"memUsedMiB"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return nil, fmt.Errorf("decode metrics: %w", err)
	}
	return &SandboxMetrics{CPUUsedPct: raw.CPUUsedPct, MemUsedMiB: raw.MemUsedMiB}, nil
}

// ResizeSandboxDisk expands the disk of the sandbox identified by sandboxID to sizeMB megabytes.
func ResizeSandboxDisk(sandboxID string, sizeMB int, opts SandboxOpts) error {
	if sizeMB <= 0 {
		return fmt.Errorf("resize disk: sizeMB must be positive")
	}
	storageSize := mbToStorageSize(sizeMB)
	url := strings.TrimRight(opts.BaseURL, "/") + "/api/v1/sandboxes/" + sandboxID
	resp, err := doStaticManagement(http.MethodPatch, url, opts.APIKey,
		map[string]interface{}{"spec": map[string]string{"storage_size": storageSize}})
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// mbToStorageSize converts megabytes to a Kubernetes storage-size string.
// Uses Gi when sizeMB is a whole number of gibibytes, Mi otherwise.
func mbToStorageSize(sizeMB int) string {
	if sizeMB%1024 == 0 {
		return fmt.Sprintf("%dGi", sizeMB/1024)
	}
	return fmt.Sprintf("%dMi", sizeMB)
}
