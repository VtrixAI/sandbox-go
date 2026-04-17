package sandbox

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

// TemplateClientOpts configures a TemplateClient.
type TemplateClientOpts struct {
	APIKey           string
	BaseURL          string
	NamespaceID      string
	UserID           string
	RequestTimeoutMs int
}

// TemplateClient wraps the sandbox-builder API (/api/v1/templates).
type TemplateClient struct {
	apiKey           string
	baseURL          string
	namespaceID      string
	userID           string
	requestTimeoutMs int
	client           *http.Client
}

// NewTemplateClient creates a TemplateClient. APIKey defaults to SANDBOX_API_KEY env var.
func NewTemplateClient(opts TemplateClientOpts) (*TemplateClient, error) {
	apiKey := opts.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("SANDBOX_API_KEY")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("API key required: set SANDBOX_API_KEY or pass APIKey")
	}
	baseURL := opts.BaseURL
	if baseURL == "" {
		baseURL = os.Getenv("SANDBOX_BASE_URL")
	}
	if baseURL == "" {
		baseURL = "https://api.sandbox.vtrix.ai"
	}
	baseURL = strings.TrimRight(baseURL, "/")

	timeoutMs := opts.RequestTimeoutMs
	if timeoutMs == 0 {
		timeoutMs = 30_000
	}
	return &TemplateClient{
		apiKey:           apiKey,
		baseURL:          baseURL,
		namespaceID:      opts.NamespaceID,
		userID:           opts.UserID,
		requestTimeoutMs: timeoutMs,
		client:           &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond},
	}, nil
}

func (c *TemplateClient) headers() map[string]string {
	h := map[string]string{
		"X-API-Key":    c.apiKey,
		"Content-Type": "application/json",
	}
	if c.namespaceID != "" {
		h["X-Namespace-ID"] = c.namespaceID
	}
	if c.userID != "" {
		h["X-User-ID"] = c.userID
	}
	return h
}

func (c *TemplateClient) request(method, path string, body interface{}, params map[string]string) (map[string]interface{}, error) {
	var reqBody io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal body: %w", err)
		}
		reqBody = bytes.NewReader(b)
	}

	rawURL := c.baseURL + path
	if len(params) > 0 {
		q := url.Values{}
		for k, v := range params {
			q.Set(k, v)
		}
		rawURL += "?" + q.Encode()
	}

	req, err := http.NewRequest(method, rawURL, reqBody)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	for k, v := range c.headers() {
		req.Header.Set(k, v)
	}

	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	if resp.StatusCode == http.StatusNoContent {
		return map[string]interface{}{}, nil
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, respBytes)
	}

	var raw map[string]interface{}
	if err := json.Unmarshal(respBytes, &raw); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	if data, ok := raw["data"]; ok {
		if m, ok := data.(map[string]interface{}); ok {
			return m, nil
		}
	}
	return raw, nil
}

// --- Template CRUD ---

// TemplateCreateOpts holds fields for creating a template.
type TemplateCreateOpts struct {
	Name          string            `json:"name"`
	Visibility    string            `json:"visibility,omitempty"`
	Dockerfile    string            `json:"dockerfile,omitempty"`
	Image         string            `json:"image,omitempty"`
	CPUCount      int               `json:"cpuCount,omitempty"`
	MemoryMB      int               `json:"memoryMB,omitempty"`
	Envs          map[string]string `json:"envs,omitempty"`
	TTLSeconds    int               `json:"ttlSeconds,omitempty"`
	StorageType   string            `json:"storageType,omitempty"`
	StorageSizeGB int               `json:"storageSizeGB,omitempty"`
	DaemonImage   string            `json:"daemonImage,omitempty"`
	CloudsinkURL  string            `json:"cloudsinkURL,omitempty"`
}

// Create creates a new template.
func (c *TemplateClient) Create(opts TemplateCreateOpts) (map[string]interface{}, error) {
	body := map[string]interface{}{
		"name":       opts.Name,
		"visibility": "personal",
		"cpuCount":   1,
		"memoryMB":   512,
		"ttlSeconds": 300,
	}
	if opts.Visibility != "" {
		body["visibility"] = opts.Visibility
	}
	if opts.Dockerfile != "" {
		body["dockerfile"] = opts.Dockerfile
	}
	if opts.Image != "" {
		body["image"] = opts.Image
	}
	if opts.CPUCount > 0 {
		body["cpuCount"] = opts.CPUCount
	}
	if opts.MemoryMB > 0 {
		body["memoryMB"] = opts.MemoryMB
	}
	if opts.Envs != nil {
		body["envs"] = opts.Envs
	}
	if opts.TTLSeconds > 0 {
		body["ttlSeconds"] = opts.TTLSeconds
	}
	if opts.StorageType != "" {
		body["storageType"] = opts.StorageType
	}
	if opts.StorageSizeGB > 0 {
		body["storageSizeGB"] = opts.StorageSizeGB
	}
	if opts.DaemonImage != "" {
		body["daemonImage"] = opts.DaemonImage
	}
	if opts.CloudsinkURL != "" {
		body["cloudsinkURL"] = opts.CloudsinkURL
	}
	return c.request(http.MethodPost, "/api/v1/templates", body, nil)
}

// TemplateListOpts holds optional filters for listing templates.
type TemplateListOpts struct {
	Visibility string
	Limit      int
	Offset     int
}

// List returns a page of templates.
func (c *TemplateClient) List(opts *TemplateListOpts) (map[string]interface{}, error) {
	params := map[string]string{
		"limit":  "50",
		"offset": "0",
	}
	if opts != nil {
		if opts.Limit > 0 {
			params["limit"] = fmt.Sprintf("%d", opts.Limit)
		}
		if opts.Offset > 0 {
			params["offset"] = fmt.Sprintf("%d", opts.Offset)
		}
		if opts.Visibility != "" {
			params["visibility"] = opts.Visibility
		}
	}
	return c.request(http.MethodGet, "/api/v1/templates", nil, params)
}

// Get returns a template by ID.
func (c *TemplateClient) Get(templateID string) (map[string]interface{}, error) {
	return c.request(http.MethodGet, "/api/v1/templates/"+templateID, nil, nil)
}

// GetByAlias returns a template by alias.
func (c *TemplateClient) GetByAlias(alias string) (map[string]interface{}, error) {
	return c.request(http.MethodGet, "/api/v1/templates/aliases/"+alias, nil, nil)
}

// Update partially updates a template by ID.
func (c *TemplateClient) Update(templateID string, fields map[string]interface{}) (map[string]interface{}, error) {
	return c.request(http.MethodPatch, "/api/v1/templates/"+templateID, fields, nil)
}

// Delete deletes a template by ID.
func (c *TemplateClient) Delete(templateID string) error {
	_, err := c.request(http.MethodDelete, "/api/v1/templates/"+templateID, nil, nil)
	return err
}

// --- Build operations ---

// BuildOpts holds optional fields for triggering a build.
type BuildOpts struct {
	FromImage string
	FilesHash string
}

// Build triggers a new build for the given template.
func (c *TemplateClient) Build(templateID string, opts *BuildOpts) (map[string]interface{}, error) {
	body := map[string]interface{}{}
	if opts != nil {
		if opts.FromImage != "" {
			body["fromImage"] = opts.FromImage
		}
		if opts.FilesHash != "" {
			body["filesHash"] = opts.FilesHash
		}
	}
	return c.request(http.MethodPost, "/api/v1/templates/"+templateID+"/builds", body, nil)
}

// Rollback rolls back a template to a specific build.
func (c *TemplateClient) Rollback(templateID, buildID string) (map[string]interface{}, error) {
	return c.request(http.MethodPost, "/api/v1/templates/"+templateID+"/rollback",
		map[string]string{"buildId": buildID}, nil)
}

// BuildListOpts holds optional pagination for listing builds.
type BuildListOpts struct {
	Limit  int
	Offset int
}

// ListBuilds lists all builds for a template.
func (c *TemplateClient) ListBuilds(templateID string, opts *BuildListOpts) (map[string]interface{}, error) {
	params := map[string]string{"limit": "50", "offset": "0"}
	if opts != nil {
		if opts.Limit > 0 {
			params["limit"] = fmt.Sprintf("%d", opts.Limit)
		}
		if opts.Offset > 0 {
			params["offset"] = fmt.Sprintf("%d", opts.Offset)
		}
	}
	return c.request(http.MethodGet, "/api/v1/templates/"+templateID+"/builds", nil, params)
}

// GetBuild returns details for a specific build.
func (c *TemplateClient) GetBuild(templateID, buildID string) (map[string]interface{}, error) {
	return c.request(http.MethodGet, "/api/v1/templates/"+templateID+"/builds/"+buildID, nil, nil)
}

// BuildStatusOpts holds optional parameters for querying build status.
type BuildStatusOpts struct {
	LogsOffset int
	Limit      int
	Level      string
}

// GetBuildStatus returns the status (and recent logs) for a build.
func (c *TemplateClient) GetBuildStatus(templateID, buildID string, opts *BuildStatusOpts) (map[string]interface{}, error) {
	params := map[string]string{"logsOffset": "0", "limit": "100"}
	if opts != nil {
		if opts.LogsOffset > 0 {
			params["logsOffset"] = fmt.Sprintf("%d", opts.LogsOffset)
		}
		if opts.Limit > 0 {
			params["limit"] = fmt.Sprintf("%d", opts.Limit)
		}
		if opts.Level != "" {
			params["level"] = opts.Level
		}
	}
	return c.request(http.MethodGet, "/api/v1/templates/"+templateID+"/builds/"+buildID+"/status", nil, params)
}

// BuildLogsOpts holds optional pagination/filtering for fetching build logs.
type BuildLogsOpts struct {
	Cursor    int
	Limit     int
	Direction string
	Level     string
	Source    string
}

// GetBuildLogs returns paginated build logs.
func (c *TemplateClient) GetBuildLogs(templateID, buildID string, opts *BuildLogsOpts) (map[string]interface{}, error) {
	params := map[string]string{
		"cursor":    "0",
		"limit":     "100",
		"direction": "forward",
		"source":    "temporary",
	}
	if opts != nil {
		if opts.Cursor > 0 {
			params["cursor"] = fmt.Sprintf("%d", opts.Cursor)
		}
		if opts.Limit > 0 {
			params["limit"] = fmt.Sprintf("%d", opts.Limit)
		}
		if opts.Direction != "" {
			params["direction"] = opts.Direction
		}
		if opts.Level != "" {
			params["level"] = opts.Level
		}
		if opts.Source != "" {
			params["source"] = opts.Source
		}
	}
	return c.request(http.MethodGet, "/api/v1/templates/"+templateID+"/builds/"+buildID+"/logs", nil, params)
}

// FilesUploadInfo is returned by GetFilesUploadURL.
type FilesUploadInfo struct {
	Present bool
	URL     string
}

// GetFilesUploadURL checks whether files are already uploaded and returns a presigned URL if not.
func (c *TemplateClient) GetFilesUploadURL(templateID, filesHash string) (*FilesUploadInfo, error) {
	raw, err := c.request(http.MethodGet, "/api/v1/templates/"+templateID+"/files/"+filesHash, nil, nil)
	if err != nil {
		return nil, err
	}
	info := &FilesUploadInfo{}
	if v, ok := raw["present"].(bool); ok {
		info.Present = v
	}
	if v, ok := raw["url"].(string); ok {
		info.URL = v
	}
	return info, nil
}

// QuickBuildResult holds the response from QuickBuild.
type QuickBuildResult struct {
	TemplateID    string `json:"templateID"`
	BuildID       string `json:"buildID"`
	ImageFullName string `json:"imageFullName"`
}

// QuickBuild directly builds an image without pre-creating a template (no auth required).
func QuickBuild(project, image, tag, dockerfile string, baseURL string, timeoutMs int) (*QuickBuildResult, error) {
	if baseURL == "" {
		baseURL = os.Getenv("SANDBOX_BASE_URL")
	}
	if baseURL == "" {
		baseURL = "https://api.sandbox.vtrix.ai"
	}
	baseURL = strings.TrimRight(baseURL, "/")
	if timeoutMs <= 0 {
		timeoutMs = 30_000
	}
	client := &http.Client{Timeout: time.Duration(timeoutMs) * time.Millisecond}

	body := map[string]string{
		"project":    project,
		"image":      image,
		"tag":        tag,
		"dockerfile": dockerfile,
	}
	b, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal body: %w", err)
	}
	req, err := http.NewRequest(http.MethodPost, baseURL+"/build", bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("execute request: %w", err)
	}
	defer resp.Body.Close()
	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, respBytes)
	}
	var result QuickBuildResult
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("decode response: %w", err)
	}
	return &result, nil
}
