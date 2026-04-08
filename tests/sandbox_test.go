package tests

import (
	"os"
	"strings"
	"testing"

	sandbox "github.com/VtrixAI/sandbox-go"
)

// ---------------------------------------------------------------------------
// Sandbox management tests (require Atlas — skipped by default)
// ---------------------------------------------------------------------------

func TestSandbox_Management(t *testing.T) {
	if os.Getenv("ATLAS_BASE_URL") == "" {
		t.Skip("ATLAS_BASE_URL not set — skipping management API tests")
	}

	apiKey := os.Getenv("SANDBOX_API_KEY")
	if apiKey == "" {
		apiKey = "test-key"
	}
	opts := sandbox.SandboxOpts{
		APIKey:   apiKey,
		BaseURL:  hermesAddr,
		Template: "base",
		Timeout:  60,
	}

	t.Run("create_and_kill", func(t *testing.T) {
		s, err := sandbox.Create(opts)
		noErr(t, err, "Create")
		t.Logf("created sandbox: %s", s.SandboxID)

		info, err := s.GetInfo()
		noErr(t, err, "GetInfo")
		t.Logf("state: %s", info.State)

		if !s.IsRunning() {
			t.Error("IsRunning should be true after Create")
		}

		noErr(t, s.Kill(), "Kill")
	})

	t.Run("list", func(t *testing.T) {
		infos, err := sandbox.List(opts)
		noErr(t, err, "List")
		t.Logf("found %d sandboxes", len(infos))
	})

	t.Run("list_page", func(t *testing.T) {
		page, err := sandbox.ListPage(sandbox.SandboxListOpts{
			APIKey:  apiKey,
			BaseURL: hermesAddr,
			Limit:   5,
		})
		noErr(t, err, "ListPage")
		t.Logf("ListPage: %d items, hasMore=%v", len(page.Items), page.HasMore)
	})

	t.Run("set_timeout", func(t *testing.T) {
		s, err := sandbox.Create(opts)
		noErr(t, err, "Create")
		defer s.Kill() //nolint:errcheck
		noErr(t, s.SetTimeout(120), "SetTimeout")
	})

	t.Run("connect", func(t *testing.T) {
		s, err := sandbox.Create(opts)
		noErr(t, err, "Create")
		defer s.Kill() //nolint:errcheck

		s2, err := sandbox.Connect(s.SandboxID, opts)
		noErr(t, err, "Connect")

		result, err := s2.Commands.Run("echo 'connected_ok'")
		noErr(t, err, "Run after Connect")
		if !strings.Contains(result.Stdout, "connected_ok") {
			t.Errorf("unexpected stdout: %q", result.Stdout)
		}
	})

	t.Run("get_host", func(t *testing.T) {
		s, err := sandbox.Create(opts)
		noErr(t, err, "Create")
		defer s.Kill() //nolint:errcheck

		host := s.GetHost(3000)
		if !strings.Contains(host, s.SandboxID) {
			t.Errorf("GetHost(3000) = %q, expected to contain sandboxId %q", host, s.SandboxID)
		}
		if !strings.Contains(host, "3000") {
			t.Errorf("GetHost(3000) = %q, expected to contain port 3000", host)
		}
		t.Logf("GetHost(3000) = %q", host)
	})

	t.Run("sandbox_domain", func(t *testing.T) {
		s, err := sandbox.Create(opts)
		noErr(t, err, "Create")
		defer s.Kill() //nolint:errcheck

		if s.SandboxDomain == "" {
			t.Error("SandboxDomain should not be empty")
		}
		t.Logf("SandboxDomain = %q", s.SandboxDomain)
	})

	t.Run("get_metrics", func(t *testing.T) {
		s, err := sandbox.Create(opts)
		noErr(t, err, "Create")
		defer s.Kill() //nolint:errcheck

		metrics, err := s.GetMetrics()
		noErr(t, err, "GetMetrics")
		t.Logf("metrics: cpu=%.2f%% mem=%.2fMiB", metrics.CPUUsedPct, metrics.MemUsedMiB)
	})

	t.Run("download_url", func(t *testing.T) {
		s, err := sandbox.Create(opts)
		noErr(t, err, "Create")
		defer s.Kill() //nolint:errcheck

		// Write a file first so the path is valid.
		_, err = s.Files.WriteText("/tmp/dl_test.txt", "download url test")
		noErr(t, err, "WriteText before DownloadURL")

		dlURL, err := s.DownloadURL("/tmp/dl_test.txt")
		noErr(t, err, "DownloadURL")
		if dlURL == "" {
			t.Fatal("DownloadURL returned empty string")
		}
		if !strings.Contains(dlURL, "signature") {
			t.Errorf("DownloadURL = %q, expected to contain 'signature'", dlURL)
		}
		t.Logf("DownloadURL = %q", dlURL)
	})

	t.Run("upload_url", func(t *testing.T) {
		s, err := sandbox.Create(opts)
		noErr(t, err, "Create")
		defer s.Kill() //nolint:errcheck

		upURL, err := s.UploadURL("/tmp/up_test.txt")
		noErr(t, err, "UploadURL")
		if upURL == "" {
			t.Fatal("UploadURL returned empty string")
		}
		if !strings.Contains(upURL, "signature") {
			t.Errorf("UploadURL = %q, expected to contain 'signature'", upURL)
		}
		t.Logf("UploadURL = %q", upURL)
	})
}
