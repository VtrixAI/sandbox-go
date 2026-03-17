package sandbox

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// Read reads a file from the sandbox.
func (s *Sandbox) Read(ctx context.Context, path string) (*ReadResult, error) {
	resp, err := s.call(ctx, "read", map[string]any{"path": path}, nil)
	if err != nil {
		return nil, err
	}
	var result ReadResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("read result parse: %w", err)
	}
	return &result, nil
}

// Write writes a text file to the sandbox.
func (s *Sandbox) Write(ctx context.Context, path, content string) (*WriteResult, error) {
	resp, err := s.call(ctx, "write", map[string]any{"path": path, "content": content}, nil)
	if err != nil {
		return nil, err
	}
	var result WriteResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("write result parse: %w", err)
	}
	return &result, nil
}

// Edit replaces old_text with new_text in a file inside the sandbox.
func (s *Sandbox) Edit(ctx context.Context, path, oldText, newText string) (*EditResult, error) {
	resp, err := s.call(ctx, "edit", map[string]any{"path": path, "old_text": oldText, "new_text": newText}, nil)
	if err != nil {
		return nil, err
	}
	var result EditResult
	if err := json.Unmarshal(resp.Result, &result); err != nil {
		return nil, fmt.Errorf("edit result parse: %w", err)
	}
	return &result, nil
}

// WriteFiles writes multiple files at once. Content is raw bytes (base64-encoded over the wire).
func (s *Sandbox) WriteFiles(ctx context.Context, files []WriteFileEntry) error {
	for _, f := range files {
		encoded := base64.StdEncoding.EncodeToString(f.Content)
		_, err := s.call(ctx, "write_binary", map[string]any{"path": f.Path, "data": encoded}, nil)
		if err != nil {
			return fmt.Errorf("write_binary %q: %w", f.Path, err)
		}
	}
	return nil
}

// ReadToBuffer reads a file and returns its raw bytes.
// For images the base64 data is decoded; for text the content is returned as-is.
func (s *Sandbox) ReadToBuffer(ctx context.Context, path string) ([]byte, error) {
	result, err := s.Read(ctx, path)
	if err != nil {
		return nil, err
	}
	if result.Type == "image" {
		data, err := base64.StdEncoding.DecodeString(result.Data)
		if err != nil {
			return nil, fmt.Errorf("base64 decode: %w", err)
		}
		return data, nil
	}
	return []byte(result.Content), nil
}

// MkDir creates a directory (and all parents) inside the sandbox.
func (s *Sandbox) MkDir(ctx context.Context, path string) error {
	_, err := s.Execute(ctx, fmt.Sprintf("mkdir -p %q", path), nil)
	return err
}

// DownloadFile downloads a file from the sandbox to a local path.
// If opts.MkdirRecursive is true, parent directories are created automatically.
// Returns the absolute local path of the saved file.
func (s *Sandbox) DownloadFile(ctx context.Context, sandboxPath, localPath string, opts *DownloadOptions) (string, error) {
	data, err := s.ReadToBuffer(ctx, sandboxPath)
	if err != nil {
		return "", fmt.Errorf("download %q: %w", sandboxPath, err)
	}

	abs, err := filepath.Abs(localPath)
	if err != nil {
		return "", err
	}

	if opts != nil && opts.MkdirRecursive {
		if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
			return "", fmt.Errorf("mkdir local: %w", err)
		}
	}

	if err := os.WriteFile(abs, data, 0o644); err != nil {
		return "", fmt.Errorf("write local %q: %w", abs, err)
	}
	return abs, nil
}

// DownloadFiles downloads multiple files concurrently from the sandbox to local paths.
// Returns a map of sandboxPath → absolute local path for successful downloads.
// If any download fails, returns the error immediately (partial results may exist on disk).
func (s *Sandbox) DownloadFiles(ctx context.Context, files []DownloadEntry, opts *DownloadOptions) (map[string]string, error) {
	type result struct {
		sandboxPath string
		localPath   string
		err         error
	}

	results := make(chan result, len(files))
	for _, f := range files {
		f := f // capture loop var
		go func() {
			localPath, err := s.DownloadFile(ctx, f.SandboxPath, f.LocalPath, opts)
			results <- result{sandboxPath: f.SandboxPath, localPath: localPath, err: err}
		}()
	}

	downloaded := make(map[string]string, len(files))
	for i := 0; i < len(files); i++ {
		r := <-results
		if r.err != nil {
			return downloaded, fmt.Errorf("download %q: %w", r.sandboxPath, r.err)
		}
		downloaded[r.sandboxPath] = r.localPath
	}
	return downloaded, nil
}

// Domain returns the publicly accessible URL for the given port on this sandbox.
// It uses Info.PreviewHost if set, otherwise falls back to Info.PreviewURL.
func (s *Sandbox) Domain(port int) string {
	if s.Info.PreviewHost != "" {
		return fmt.Sprintf("https://%d-%s", port, s.Info.PreviewHost)
	}
	return s.Info.PreviewURL
}

// UploadFile reads a local file and writes it into the sandbox at sandboxPath.
// If opts.MkdirRecursive is true, parent directories are created in the sandbox first.
func (s *Sandbox) UploadFile(ctx context.Context, localPath, sandboxPath string, opts *DownloadOptions) error {
	data, err := os.ReadFile(localPath)
	if err != nil {
		return fmt.Errorf("read local %q: %w", localPath, err)
	}
	if opts != nil && opts.MkdirRecursive {
		if err := s.MkDir(ctx, filepath.Dir(sandboxPath)); err != nil {
			return fmt.Errorf("mkdir sandbox: %w", err)
		}
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	_, err = s.call(ctx, "write_binary", map[string]any{"path": sandboxPath, "data": encoded}, nil)
	return err
}

// ListFiles lists the contents of a directory inside the sandbox.
func (s *Sandbox) ListFiles(ctx context.Context, path string) ([]FileEntry, error) {
	resp, err := s.call(ctx, "list_files", map[string]any{"path": path}, nil)
	if err != nil {
		return nil, err
	}
	var entries []FileEntry
	if err := json.Unmarshal(resp.Result, &entries); err != nil {
		return nil, fmt.Errorf("list_files parse: %w", err)
	}
	return entries, nil
}

// Stat returns metadata about a file or directory inside the sandbox.
// FileInfo.Exists will be false if the path does not exist (no error returned).
func (s *Sandbox) Stat(ctx context.Context, path string) (*FileInfo, error) {
	resp, err := s.call(ctx, "stat", map[string]any{"path": path}, nil)
	if err != nil {
		return nil, err
	}
	var info FileInfo
	if err := json.Unmarshal(resp.Result, &info); err != nil {
		return nil, fmt.Errorf("stat parse: %w", err)
	}
	return &info, nil
}

// Exists reports whether the given path exists inside the sandbox.
func (s *Sandbox) Exists(ctx context.Context, path string) (bool, error) {
	info, err := s.Stat(ctx, path)
	if err != nil {
		return false, err
	}
	return info.Exists, nil
}

// ReadStream streams a file in base64-encoded chunks.
// It returns two read-only channels: one for chunks (base64 strings) and one for the
// final result. Both channels are closed after the stream finishes or an error occurs.
// An error is returned immediately if the RPC call cannot be started.
func (s *Sandbox) ReadStream(ctx context.Context, path string, chunkSize int) (<-chan ReadStreamChunk, <-chan ReadStreamResult, <-chan error) {
	chunkCh := make(chan ReadStreamChunk, 16)
	resultCh := make(chan ReadStreamResult, 1)
	errCh := make(chan error, 1)

	params := map[string]any{"path": path}
	if chunkSize > 0 {
		params["chunk_size"] = chunkSize
	}

	notif := make(chan rpcResponse, 64)

	go func() {
		defer close(chunkCh)
		defer close(resultCh)
		defer close(errCh)

		done := make(chan struct{})
		var callErr error
		var callResp *rpcResponse

		go func() {
			defer close(done)
			callResp, callErr = s.callWithNotif(ctx, "read_stream", params, notif)
		}()

		// Drain notifications until the call goroutine finishes.
		for {
			select {
			case n, ok := <-notif:
				if !ok {
					goto wait
				}
				sendReadStreamChunk(chunkCh, n)
			case <-done:
				// Flush any remaining buffered notifications.
				drainNotifChunks(notif, chunkCh)
				goto wait
			}
		}
	wait:
		if callErr != nil {
			errCh <- callErr
			return
		}

		var result ReadStreamResult
		if callResp != nil && callResp.Result != nil {
			_ = json.Unmarshal(callResp.Result, &result)
		}
		resultCh <- result
	}()

	return chunkCh, resultCh, errCh
}

// sendReadStreamChunk forwards a single read_stream.chunk notification to chunkCh.
func sendReadStreamChunk(chunkCh chan<- ReadStreamChunk, n rpcResponse) {
	if n.Method != "read_stream.chunk" {
		return
	}
	var np readStreamNotifParams
	if n.Params != nil {
		_ = json.Unmarshal(n.Params, &np)
	}
	chunkCh <- ReadStreamChunk{Data: np.Data}
}

// drainNotifChunks flushes any already-buffered notifications from notif.
func drainNotifChunks(notif <-chan rpcResponse, chunkCh chan<- ReadStreamChunk) {
	for {
		select {
		case n, ok := <-notif:
			if !ok {
				return
			}
			sendReadStreamChunk(chunkCh, n)
		default:
			return
		}
	}
}
