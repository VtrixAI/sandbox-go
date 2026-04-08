package sandbox

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Filesystem provides file-system operations against a running sandbox.
type Filesystem struct {
	cfg    ConnectionConfig
	client *http.Client
}

// ---- internal helpers ----

// envdURL returns the full URL for a path on the sandbox's nano-executor.
func (f *Filesystem) envdURL(path string) string {
	return strings.TrimRight(f.cfg.EnvdURL, "/") + path
}

// setAccessToken adds the X-Access-Token header when a token is configured,
// or falls back to X-API-Key when only an API key is available.
func (f *Filesystem) setAccessToken(req *http.Request) {
	if f.cfg.AccessToken != "" {
		req.Header.Set("X-Access-Token", f.cfg.AccessToken)
	} else if f.cfg.APIKey != "" {
		req.Header.Set("X-API-Key", f.cfg.APIKey)
	}
}

// doRPC issues a Connect-protocol JSON RPC call and returns the raw response.
// If body is a map[string]interface{} and user is non-empty, "username" is injected into it.
func (f *Filesystem) doRPC(method string, reqBody interface{}) (*http.Response, error) {
	return f.doRPCWithUser(method, reqBody, "", 0)
}

// doRPCWithUser is like doRPC but injects username into the request body when non-empty,
// and applies a per-request timeout when timeoutMs > 0.
func (f *Filesystem) doRPCWithUser(method string, reqBody interface{}, user string, timeoutMs int) (*http.Response, error) {
	if user != "" {
		if m, ok := reqBody.(map[string]interface{}); ok {
			m["username"] = user
		}
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal rpc body: %w", err)
	}

	ctx := context.Background()
	if timeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, msToTimeout(timeoutMs))
		_ = cancel // context will be cleaned up when request completes
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.envdURL(method), bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create rpc request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	f.setAccessToken(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("rpc %s: %w", method, err)
	}
	return resp, nil
}

// ---- wire types ----

type entryInfoWire struct {
	Name          string `json:"name"`
	Type          string `json:"type"`
	Path          string `json:"path"`
	Size          int64  `json:"size"`
	Mode          uint32 `json:"mode"`
	Permissions   string `json:"permissions"`
	Owner         string `json:"owner"`
	Group         string `json:"group"`
	ModifiedTime  string `json:"modifiedTime"`
	SymlinkTarget string `json:"symlinkTarget"`
}

func (e *entryInfoWire) toEntryInfo() EntryInfo {
	info := EntryInfo{
		Name:          e.Name,
		Type:          e.Type,
		Path:          e.Path,
		Size:          e.Size,
		Mode:          e.Mode,
		Permissions:   e.Permissions,
		Owner:         e.Owner,
		Group:         e.Group,
		SymlinkTarget: e.SymlinkTarget,
	}
	if t, err := time.Parse(time.RFC3339, e.ModifiedTime); err == nil {
		info.ModifiedTime = t
	}
	return info
}

type listDirResponse struct {
	Entries []entryInfoWire `json:"entries"`
}

type statResponse struct {
	Entry entryInfoWire `json:"entry"`
}

type moveResponse struct {
	Entry entryInfoWire `json:"entry"`
}

type watchDirEvent struct {
	Filesystem struct {
		Name string `json:"name"`
		Type string `json:"type"`
	} `json:"filesystem"`
}

// ---- Public API ----

// Read returns the raw contents of a file at path.
func (f *Filesystem) Read(path string, opts ...ReadOpts) ([]byte, error) {
	u := f.envdURL("/files") + "?path=" + url.QueryEscape(path)
	if len(opts) > 0 && opts[0].User != "" {
		u += "&username=" + url.QueryEscape(opts[0].User)
	}
	ctx := context.Background()
	if len(opts) > 0 && opts[0].RequestTimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, msToTimeout(opts[0].RequestTimeoutMs))
		_ = cancel
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create read request: %w", err)
	}
	f.setAccessToken(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /files: %w", err)
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}
	return body, nil
}

// ReadStream returns the file contents as a streaming io.ReadCloser.
// The caller is responsible for closing the returned reader.
// Use this for large files to avoid loading the entire content into memory.
func (f *Filesystem) ReadStream(path string, opts ...ReadOpts) (io.ReadCloser, error) {
	u := f.envdURL("/files") + "?path=" + url.QueryEscape(path)
	if len(opts) > 0 && opts[0].User != "" {
		u += "&username=" + url.QueryEscape(opts[0].User)
	}
	req, err := http.NewRequest(http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("create read request: %w", err)
	}
	f.setAccessToken(req)

	// Use a client without a read deadline so large files don't time out mid-stream.
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET /files: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := readBody(resp)
		return nil, parseAPIError(resp.StatusCode, body)
	}
	return resp.Body, nil
}

// ReadText returns the file contents as a UTF-8 string.
func (f *Filesystem) ReadText(path string, opts ...ReadOpts) (string, error) {
	b, err := f.Read(path, opts...)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

// Write uploads raw bytes to path.
func (f *Filesystem) Write(path string, data []byte, opts ...WriteOpts) (*WriteInfo, error) {
	u := f.envdURL("/files") + "?path=" + url.QueryEscape(path)
	if len(opts) > 0 && opts[0].User != "" {
		u += "&username=" + url.QueryEscape(opts[0].User)
	}
	ctx := context.Background()
	if len(opts) > 0 && opts[0].RequestTimeoutMs > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, msToTimeout(opts[0].RequestTimeoutMs))
		_ = cancel
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("create write request: %w", err)
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	f.setAccessToken(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /files: %w", err)
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	var wi WriteInfo
	if err := json.Unmarshal(body, &wi); err != nil {
		// Response may be empty on success for some implementations; return a minimal WriteInfo.
		wi = WriteInfo{Path: path}
	}
	return &wi, nil
}

// WriteText uploads a string as UTF-8 to path.
func (f *Filesystem) WriteText(path string, data string, opts ...WriteOpts) (*WriteInfo, error) {
	return f.Write(path, []byte(data), opts...)
}

// batchWriteEntry is the wire format for a single entry in a batch write.
// Content is a plain string; the nano-executor writes it verbatim to disk.
type batchWriteEntry struct {
	Path    string `json:"path"`
	Content string `json:"content"`
}

// batchWriteRequest is the wire format for a batch write request.
type batchWriteRequest struct {
	Files []batchWriteEntry `json:"files"`
}

// batchWriteResult is a single result from a batch write response.
type batchWriteResult struct {
	Path         string `json:"path"`
	BytesWritten int64  `json:"bytes_written"`
}

// WriteFiles uploads multiple files in a single batch request.
func (f *Filesystem) WriteFiles(files []WriteEntry, opts ...WriteOpts) ([]WriteInfo, error) {
	entries := make([]batchWriteEntry, len(files))
	for i, fe := range files {
		entries[i] = batchWriteEntry{Path: fe.Path, Content: string(fe.Content)}
	}

	b, err := json.Marshal(batchWriteRequest{Files: entries})
	if err != nil {
		return nil, fmt.Errorf("marshal batch write: %w", err)
	}

	req, err := http.NewRequest(http.MethodPost, f.envdURL("/files/batch"), bytes.NewReader(b))
	if err != nil {
		return nil, fmt.Errorf("create batch write request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	f.setAccessToken(req)

	resp, err := f.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("POST /files/batch: %w", err)
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	var result struct {
		Files []batchWriteResult `json:"files"`
	}
	infos := make([]WriteInfo, len(files))
	if err := json.Unmarshal(body, &result); err == nil && len(result.Files) > 0 {
		for i, r := range result.Files {
			infos[i] = WriteInfo{Path: r.Path}
		}
	} else {
		for i, fe := range files {
			infos[i] = WriteInfo{Path: fe.Path}
		}
	}
	return infos, nil
}

// List returns directory entries for the given path.
func (f *Filesystem) List(path string, opts ...ListOpts) ([]EntryInfo, error) {
	body := map[string]interface{}{"path": path}
	var user string
	var timeoutMs int
	if len(opts) > 0 {
		if opts[0].Depth > 0 {
			body["depth"] = opts[0].Depth
		}
		user = opts[0].User
		timeoutMs = opts[0].RequestTimeoutMs
	}
	resp, err := f.doRPCWithUser("/filesystem.Filesystem/ListDir", body, user, timeoutMs)
	if err != nil {
		return nil, err
	}
	body2, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body2)
	}

	var lr listDirResponse
	if err := json.Unmarshal(body2, &lr); err != nil {
		return nil, fmt.Errorf("decode ListDir response: %w", err)
	}

	entries := make([]EntryInfo, len(lr.Entries))
	for i, e := range lr.Entries {
		entries[i] = e.toEntryInfo()
	}
	return entries, nil
}

// Exists returns true if a file or directory exists at path.
func (f *Filesystem) Exists(path string, opts ...FSRequestOpts) (bool, error) {
	user := ""
	timeoutMs := 0
	if len(opts) > 0 {
		user = opts[0].User
		timeoutMs = opts[0].RequestTimeoutMs
	}
	resp, err := f.doRPCWithUser("/filesystem.Filesystem/Stat", map[string]interface{}{"path": path}, user, timeoutMs)
	if err != nil {
		return false, err
	}
	if resp.StatusCode == http.StatusNotFound {
		defer resp.Body.Close()
		return false, nil
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, parseAPIError(resp.StatusCode, body)
	}
	return true, nil
}

// GetInfo returns metadata about a file or directory.
func (f *Filesystem) GetInfo(path string, opts ...FSRequestOpts) (*EntryInfo, error) {
	user := ""
	timeoutMs := 0
	if len(opts) > 0 {
		user = opts[0].User
		timeoutMs = opts[0].RequestTimeoutMs
	}
	resp, err := f.doRPCWithUser("/filesystem.Filesystem/Stat", map[string]interface{}{"path": path}, user, timeoutMs)
	if err != nil {
		return nil, err
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, body)
	}

	var sr statResponse
	if err := json.Unmarshal(body, &sr); err != nil {
		return nil, fmt.Errorf("decode Stat response: %w", err)
	}
	info := sr.Entry.toEntryInfo()
	return &info, nil
}

// Remove deletes a file or directory at path.
func (f *Filesystem) Remove(path string, opts ...FSRequestOpts) error {
	user := ""
	timeoutMs := 0
	if len(opts) > 0 {
		user = opts[0].User
		timeoutMs = opts[0].RequestTimeoutMs
	}
	resp, err := f.doRPCWithUser("/filesystem.Filesystem/Remove", map[string]interface{}{"path": path}, user, timeoutMs)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// Rename moves oldPath to newPath and returns the updated entry info.
func (f *Filesystem) Rename(oldPath, newPath string, opts ...FSRequestOpts) (*EntryInfo, error) {
	user := ""
	timeoutMs := 0
	if len(opts) > 0 {
		user = opts[0].User
		timeoutMs = opts[0].RequestTimeoutMs
	}
	body := map[string]interface{}{"source": oldPath, "destination": newPath}
	resp, err := f.doRPCWithUser("/filesystem.Filesystem/Move", body, user, timeoutMs)
	if err != nil {
		return nil, err
	}
	raw, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, parseAPIError(resp.StatusCode, raw)
	}

	var mr moveResponse
	if err := json.Unmarshal(raw, &mr); err != nil {
		return nil, fmt.Errorf("decode Move response: %w", err)
	}
	info := mr.Entry.toEntryInfo()
	return &info, nil
}

// MakeDir creates a directory (and parents) at path.
// Returns true if the call succeeded (directory now exists), false on error.
// Note: nano-executor does not distinguish between newly created and already-existing
// directories in its response, so this always returns true on success.
func (f *Filesystem) MakeDir(path string, opts ...FSRequestOpts) (bool, error) {
	user := ""
	timeoutMs := 0
	if len(opts) > 0 {
		user = opts[0].User
		timeoutMs = opts[0].RequestTimeoutMs
	}
	resp, err := f.doRPCWithUser("/filesystem.Filesystem/MakeDir", map[string]interface{}{"path": path}, user, timeoutMs)
	if err != nil {
		return false, err
	}
	body, _ := readBody(resp)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return false, parseAPIError(resp.StatusCode, body)
	}
	return true, nil
}

// Edit performs an in-place text substitution in the file at path.
// oldText must appear exactly once in the file; if it is not found or appears
// more than once the server returns HTTP 422 (InvalidArgumentError).
func (f *Filesystem) Edit(path, oldText, newText string, opts ...FSRequestOpts) error {
	user := ""
	timeoutMs := 0
	if len(opts) > 0 {
		user = opts[0].User
		timeoutMs = opts[0].RequestTimeoutMs
	}
	body := map[string]interface{}{
		"path":    path,
		"oldText": oldText,
		"newText": newText,
	}
	resp, err := f.doRPCWithUser("/filesystem.Filesystem/Edit", body, user, timeoutMs)
	if err != nil {
		return err
	}
	return checkResponse(resp)
}

// WatchHandle allows the caller to stop an active WatchDir stream.
type WatchHandle struct {
	cancel context.CancelFunc
	done   <-chan struct{}
}

// Stop terminates the WatchDir stream.
func (w *WatchHandle) Stop() {
	w.cancel()
	<-w.done
}

// WatchDir watches a directory for filesystem events and calls onEvent for each one.
// The returned WatchHandle can be used to stop watching.
func (f *Filesystem) WatchDir(path string, onEvent func(FilesystemEvent), opts ...WatchOpts) (*WatchHandle, error) {
	watchBody := map[string]interface{}{"path": path}
	var onExit func(error)
	if len(opts) > 0 {
		if opts[0].Recursive {
			watchBody["recursive"] = true
		}
		onExit = opts[0].OnExit
	}

	b, err := json.Marshal(watchBody)
	if err != nil {
		return nil, fmt.Errorf("marshal WatchDir body: %w", err)
	}

	var ctx context.Context
	var cancel context.CancelFunc
	if len(opts) > 0 && opts[0].TimeoutMs > 0 {
		ctx, cancel = context.WithTimeout(context.Background(), msToTimeout(opts[0].TimeoutMs))
	} else {
		ctx, cancel = context.WithCancel(context.Background())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, f.envdURL("/filesystem.Filesystem/WatchDir"), bytes.NewReader(b))
	if err != nil {
		cancel()
		return nil, fmt.Errorf("create WatchDir request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Connect-Protocol-Version", "1")
	req.Header.Set("Accept", "text/event-stream")
	f.setAccessToken(req)

	// Use a client without a timeout for streaming.
	streamClient := &http.Client{}
	resp, err := streamClient.Do(req)
	if err != nil {
		cancel()
		return nil, fmt.Errorf("WatchDir connect: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := readBody(resp)
		cancel()
		return nil, parseAPIError(resp.StatusCode, body)
	}

	done := make(chan struct{})
	handle := &WatchHandle{cancel: cancel, done: done}

	go func() {
		defer close(done)
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		var dataBuf strings.Builder
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "data:") {
				dataBuf.WriteString(strings.TrimPrefix(line, "data:"))
			} else if line == "" && dataBuf.Len() > 0 {
				// End of SSE event block.
				raw := strings.TrimSpace(dataBuf.String())
				dataBuf.Reset()
				if raw == "" {
					continue
				}
				var evt watchDirEvent
				if err := json.Unmarshal([]byte(raw), &evt); err == nil && evt.Filesystem.Name != "" {
					onEvent(FilesystemEvent{
						Name: evt.Filesystem.Name,
						Type: evt.Filesystem.Type,
					})
				}
			}
		}
		if onExit != nil {
			onExit(scanner.Err())
		}
	}()

	return handle, nil
}

