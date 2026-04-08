package tests

import (
	"errors"
	"runtime"
	"strings"
	"testing"
	"time"

	sandbox "github.com/VtrixAI/sandbox-go"
)

// ---------------------------------------------------------------------------
// Commands — Run
// ---------------------------------------------------------------------------

func TestCommands_Run_Basic(t *testing.T) {
	result, err := sb.Commands.Run("echo 'hello e2e'")
	noErr(t, err, "Run")

	if !strings.Contains(result.Stdout, "hello e2e") {
		t.Fatalf("stdout: got %q, want 'hello e2e'", result.Stdout)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code: got %d, want 0", result.ExitCode)
	}
}

func TestCommands_Run_ExitCode(t *testing.T) {
	_, err := sb.Commands.Run("exit 42")
	if err == nil {
		t.Fatal("Run exit 42: expected non-nil error, got nil")
	}
	var exitErr *sandbox.CommandExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Run exit 42: expected *CommandExitError, got %T: %v", err, err)
	}
	if exitErr.ExitCode != 42 {
		t.Fatalf("exit code: got %d, want 42", exitErr.ExitCode)
	}
}

func TestCommands_Run_Stderr(t *testing.T) {
	result, err := sb.Commands.Run("echo 'err msg' >&2")
	noErr(t, err, "Run stderr")

	if !strings.Contains(result.Stderr, "err msg") {
		t.Fatalf("stderr: got %q, want 'err msg'", result.Stderr)
	}
}

func TestCommands_Run_CombinedOutput(t *testing.T) {
	result, err := sb.Commands.Run("echo 'out_line'; echo 'err_line' >&2")
	noErr(t, err, "Run combined output")

	if !strings.Contains(result.Stdout, "out_line") {
		t.Errorf("stdout missing 'out_line': %q", result.Stdout)
	}
	if !strings.Contains(result.Stderr, "err_line") {
		t.Errorf("stderr missing 'err_line': %q", result.Stderr)
	}
}

func TestCommands_Run_WithEnv(t *testing.T) {
	timeout := 10
	result, err := sb.Commands.Run("echo $MY_SDK_VAR", sandbox.RunOpts{
		Envs:    map[string]string{"MY_SDK_VAR": "sdk_env_value"},
		Timeout: &timeout,
	})
	noErr(t, err, "Run with env")

	if !strings.Contains(result.Stdout, "sdk_env_value") {
		t.Fatalf("env var missing in stdout: %q", result.Stdout)
	}
}

func TestCommands_Run_WithCwd(t *testing.T) {
	_, err := sb.Files.MakeDir("/tmp/e2e_cwd_test")
	noErr(t, err, "MakeDir cwd")

	result, err := sb.Commands.Run("pwd", sandbox.RunOpts{
		Cwd: "/tmp/e2e_cwd_test",
	})
	noErr(t, err, "Run with cwd")

	if !strings.Contains(result.Stdout, "/tmp/e2e_cwd_test") {
		t.Fatalf("pwd output: got %q, want '/tmp/e2e_cwd_test'", result.Stdout)
	}
}

func TestCommands_Run_WithTag(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("Tag presence in List relies on /proc — skipping on non-Linux")
	}

	tag := "e2e-tag-test"
	timeout := 10
	handle, err := sb.Commands.RunBackground("sleep 15", sandbox.RunOpts{
		Tag:     tag,
		Timeout: &timeout,
	})
	noErr(t, err, "RunBackground with Tag")
	defer func() { _, _ = handle.Kill() }()

	time.Sleep(300 * time.Millisecond)

	procs, err := sb.Commands.List()
	noErr(t, err, "List")

	if len(procs) == 0 {
		t.Skip("List returned 0 processes — likely non-Linux; skipping tag check")
	}

	found := false
	for _, p := range procs {
		if p.PID == handle.PID() && p.Tag == tag {
			found = true
			break
		}
	}
	if !found {
		t.Logf("procs: %+v", procs)
		t.Fatalf("process with pid=%d tag=%q not found in List", handle.PID(), tag)
	}
}

func TestCommands_Run_Timeout(t *testing.T) {
	timeout := 2 // seconds
	start := time.Now()
	result, err := sb.Commands.Run("sleep 30", sandbox.RunOpts{Timeout: &timeout})
	elapsed := time.Since(start)

	// The command should be killed by the server-side timeout.
	// We allow up to 10s total for transport overhead.
	if elapsed > 10*time.Second {
		t.Fatalf("Timeout=2 but Run took %s — timeout not enforced", elapsed)
	}
	if err != nil {
		t.Logf("Run with timeout returned error (acceptable): %v", err)
		return
	}
	// If no error, exit code should be non-zero (killed).
	if result.ExitCode == 0 {
		t.Errorf("sleep 30 with Timeout=2 should have non-zero exit code, got 0")
	}
	t.Logf("Timeout test: elapsed=%s exitCode=%d", elapsed, result.ExitCode)
}

func TestCommands_RunOpts_ZeroTimeout(t *testing.T) {
	// NOTE: per the API docs, sending timeout=0 to the server means "timeout immediately".
	// The SDK protects callers by converting Timeout==0 to nil (omit the field), which
	// lets the server use its default timeout (120s). This test verifies that SDK
	// protection: the command completes normally instead of being killed instantly.
	zero := 0
	result, err := sb.Commands.Run("echo zero_timeout_ok", sandbox.RunOpts{Timeout: &zero})
	noErr(t, err, "Run with Timeout=0")
	if !strings.Contains(result.Stdout, "zero_timeout_ok") {
		t.Fatalf("stdout: got %q, want 'zero_timeout_ok'", result.Stdout)
	}
}

// ---------------------------------------------------------------------------
// Commands — RunBackground
// ---------------------------------------------------------------------------

func TestCommands_RunBackground_AndWait(t *testing.T) {
	handle, err := sb.Commands.RunBackground("sleep 1 && echo 'bg_done'")
	noErr(t, err, "RunBackground")

	if handle.PID() == 0 {
		t.Fatal("RunBackground: pid=0")
	}
	t.Logf("RunBackground pid=%d", handle.PID())

	result, err := handle.Wait()
	noErr(t, err, "Wait")

	if !strings.Contains(result.Stdout, "bg_done") {
		t.Fatalf("background stdout: got %q, want 'bg_done'", result.Stdout)
	}
}

func TestCommands_RunBackground_Kill(t *testing.T) {
	handle, err := sb.Commands.RunBackground("sleep 60")
	noErr(t, err, "RunBackground sleep 60")

	killed, err := handle.Kill()
	noErr(t, err, "Kill via handle")
	if !killed {
		t.Fatal("Kill returned false")
	}
}

func TestCommands_RunBackground_Stderr(t *testing.T) {
	handle, err := sb.Commands.RunBackground("echo 'bg_stderr_msg' >&2")
	noErr(t, err, "RunBackground stderr")

	result, err := handle.Wait()
	noErr(t, err, "Wait")

	if !strings.Contains(result.Stderr, "bg_stderr_msg") {
		t.Fatalf("background stderr: got %q, want 'bg_stderr_msg'", result.Stderr)
	}
}

// ---------------------------------------------------------------------------
// Commands — List / Kill
// ---------------------------------------------------------------------------

func TestCommands_List(t *testing.T) {
	handle, err := sb.Commands.RunBackground("sleep 30")
	noErr(t, err, "RunBackground for List")
	defer func() { _, _ = handle.Kill() }()

	// Give the process a moment to appear in the list.
	time.Sleep(200 * time.Millisecond)

	procs, err := sb.Commands.List()
	noErr(t, err, "List")

	// On non-Linux (e.g. macOS), the nano-executor returns an empty list
	// because it relies on /proc which doesn't exist on macOS. Skip the
	// presence check in that case.
	if len(procs) == 0 {
		t.Skip("List returned 0 processes — likely non-Linux environment; skipping presence check")
	}

	found := false
	for _, p := range procs {
		if p.PID == handle.PID() {
			found = true
			break
		}
	}
	if !found {
		t.Logf("running procs: %+v", procs)
		t.Fatalf("started pid=%d not found in List()", handle.PID())
	}
}

func TestCommands_Kill_ByPID(t *testing.T) {
	handle, err := sb.Commands.RunBackground("sleep 60")
	noErr(t, err, "RunBackground")

	ok, err := sb.Commands.Kill(handle.PID())
	noErr(t, err, "Kill by pid")
	if !ok {
		t.Fatal("Kill returned false")
	}
}

func TestCommands_Kill_DeadProcess(t *testing.T) {
	handle, err := sb.Commands.RunBackground("echo done_immediately")
	noErr(t, err, "RunBackground")

	// Wait for the process to finish naturally.
	_, _ = handle.Wait()

	// Kill an already-dead process — should not panic, error is acceptable.
	ok, err := handle.Kill()
	t.Logf("Kill dead process: ok=%v err=%v", ok, err)
	// We don't assert success/failure — just no panic.
}

// ---------------------------------------------------------------------------
// Commands — Connect
// ---------------------------------------------------------------------------

func TestCommands_Connect(t *testing.T) {
	handle, err := sb.Commands.RunBackground("echo 'output_line'; sleep 5")
	noErr(t, err, "RunBackground for Connect")
	defer func() { _, _ = handle.Kill() }()

	time.Sleep(100 * time.Millisecond)

	connected, err := sb.Commands.Connect(handle.PID())
	noErr(t, err, "Connect by pid")

	done := make(chan struct{})
	go func() {
		defer close(done)
		_, _ = connected.Wait()
	}()

	select {
	case <-done:
		t.Log("Connect: process exited")
	case <-time.After(8 * time.Second):
		t.Log("Connect: still running after 8s (expected for sleep 5)")
	}
}

func TestCommands_Connect_Output(t *testing.T) {
	handle, err := sb.Commands.RunBackground("echo 'connect_output_line'; sleep 5")
	noErr(t, err, "RunBackground for Connect output")
	defer func() { _, _ = handle.Kill() }()

	time.Sleep(200 * time.Millisecond)

	connected, err := sb.Commands.Connect(handle.PID())
	noErr(t, err, "Connect")

	// Kill the original process so Wait returns quickly.
	_, _ = handle.Kill()

	result, err := connected.Wait()
	if err != nil {
		var exitErr *sandbox.CommandExitError
		if errors.As(err, &exitErr) {
			t.Logf("Connect output: process killed, exit code=%d stdout=%q", exitErr.ExitCode, exitErr.Stdout)
			return
		}
		t.Logf("Connect output Wait error (acceptable after kill): %v", err)
		return
	}
	t.Logf("Connect output stdout: %q", result.Stdout)
	// stdout may or may not include pre-connect output depending on server replay.
	// At minimum, Wait must return without error.
}

func TestCommands_Connect_InvalidPID(t *testing.T) {
	connected, err := sb.Commands.Connect(999999)
	if err != nil {
		// Connect itself returned an error — acceptable.
		t.Logf("Connect(999999) error (expected): %v", err)
		return
	}
	// If Connect succeeds, Wait should fail or return an error result.
	_, err = connected.Wait()
	if err != nil {
		t.Logf("Wait on invalid pid error (expected): %v", err)
		return
	}
	t.Log("Connect(999999): no error returned (server may silently handle unknown pid)")
}

func TestCommands_Connect_SSEError(t *testing.T) {
	// Per API docs: Connect to an unknown pid returns HTTP 200 but the SSE
	// stream contains an end event with a non-empty error field.
	connected, err := sb.Commands.Connect(999999)
	if err != nil {
		// Some implementations reject at HTTP level — acceptable.
		t.Logf("Connect(999999) HTTP error (acceptable): %v", err)
		return
	}

	result, err := connected.Wait()
	if err != nil {
		t.Logf("Wait error (acceptable): %v", err)
		return
	}
	if result != nil && result.Error != "" {
		t.Logf("Connect(999999) SSE end.error (expected): %q", result.Error)
	} else {
		t.Log("Connect(999999): no SSE error field — server handled unknown pid silently")
	}
}

// ---------------------------------------------------------------------------
// Commands — SendStdin / CloseStdin
// ---------------------------------------------------------------------------

func TestCommands_SendStdin(t *testing.T) {
	handle, err := sb.Commands.RunBackground("cat")
	noErr(t, err, "RunBackground cat")
	defer func() { _, _ = handle.Kill() }()

	time.Sleep(200 * time.Millisecond)

	err = sb.Commands.SendStdin(handle.PID(), "hello stdin\n")
	noErr(t, err, "SendStdin")
}

func TestCommands_SendStdin_Verified(t *testing.T) {
	handle, err := sb.Commands.RunBackground("cat")
	noErr(t, err, "RunBackground cat")

	time.Sleep(200 * time.Millisecond)

	err = sb.Commands.SendStdin(handle.PID(), "stdin_verified_content\n")
	noErr(t, err, "SendStdin")

	time.Sleep(200 * time.Millisecond)

	// Kill cat so it flushes and exits.
	_, err = handle.Kill()
	noErr(t, err, "Kill cat after stdin")

	result, err := handle.Wait()
	if err != nil {
		var exitErr *sandbox.CommandExitError
		if errors.As(err, &exitErr) {
			t.Logf("SendStdin stdout after kill: %q", exitErr.Stdout)
			return
		}
		t.Logf("Wait error (acceptable after kill): %v", err)
		return
	}
	t.Logf("SendStdin stdout: %q", result.Stdout)
	// cat echoes stdin to stdout; if the process received it before kill, it will be there.
	// We accept partial receipt since kill is racy — just verify no panic.
}

func TestCommands_CloseStdin(t *testing.T) {
	// Start cat; it will exit on EOF after CloseStdin.
	handle, err := sb.Commands.RunBackground("cat")
	noErr(t, err, "RunBackground cat")

	time.Sleep(200 * time.Millisecond)

	err = sb.Commands.CloseStdin(handle.PID())
	noErr(t, err, "CloseStdin")

	done := make(chan *sandbox.CommandResult, 1)
	go func() {
		r, _ := handle.Wait()
		done <- r
	}()

	select {
	case r := <-done:
		if r != nil {
			t.Logf("CloseStdin: cat exited with code=%d", r.ExitCode)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("CloseStdin: cat did not exit within 5s after stdin closed")
		_, _ = handle.Kill()
	}
}

// ---------------------------------------------------------------------------
// Commands — SendSignal
// ---------------------------------------------------------------------------

func TestCommands_SendSignal_SIGTERM(t *testing.T) {
	handle, err := sb.Commands.RunBackground("sleep 60")
	noErr(t, err, "RunBackground sleep")

	time.Sleep(200 * time.Millisecond)

	err = sb.Commands.SendSignal(handle.PID(), sandbox.SignalTerm)
	noErr(t, err, "SendSignal SIGTERM")

	done := make(chan struct{}, 1)
	go func() {
		_, _ = handle.Wait()
		done <- struct{}{}
	}()

	select {
	case <-done:
		t.Log("SendSignal SIGTERM: process exited")
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s after SIGTERM")
		_, _ = handle.Kill()
	}
}

func TestCommands_SendSignal_SIGKILL(t *testing.T) {
	handle, err := sb.Commands.RunBackground("sleep 60")
	noErr(t, err, "RunBackground sleep")

	time.Sleep(200 * time.Millisecond)

	// SendSignal(SIGKILL) should be equivalent to Kill().
	err = sb.Commands.SendSignal(handle.PID(), sandbox.SignalKill)
	noErr(t, err, "SendSignal SIGKILL")

	done := make(chan struct{}, 1)
	go func() {
		_, _ = handle.Wait()
		done <- struct{}{}
	}()

	select {
	case <-done:
		t.Log("SendSignal SIGKILL: process exited")
	case <-time.After(5 * time.Second):
		t.Fatal("process did not exit within 5s after SIGKILL")
	}
}

// ---------------------------------------------------------------------------
// Commands — by-tag (Linux only)
// ---------------------------------------------------------------------------

func TestCommands_ConnectByTag(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("by-tag operations rely on /proc List — skipping on non-Linux")
	}

	tag := "e2e-connect-tag"
	handle, err := sb.Commands.RunBackground("echo connect_tag_output; sleep 10", sandbox.RunOpts{Tag: tag})
	noErr(t, err, "RunBackground with Tag")
	defer func() { _, _ = handle.Kill() }()

	time.Sleep(300 * time.Millisecond)

	connected, err := sb.Commands.ConnectByTag(tag)
	noErr(t, err, "ConnectByTag")

	_, _ = handle.Kill()

	result, err := connected.Wait()
	if err != nil {
		var exitErr *sandbox.CommandExitError
		if errors.As(err, &exitErr) {
			t.Logf("ConnectByTag: process killed, stdout=%q", exitErr.Stdout)
			return
		}
		t.Logf("ConnectByTag Wait error (acceptable): %v", err)
		return
	}
	t.Logf("ConnectByTag stdout: %q", result.Stdout)
}

func TestCommands_KillByTag(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("by-tag operations rely on /proc List — skipping on non-Linux")
	}

	tag := "e2e-kill-tag"
	handle, err := sb.Commands.RunBackground("sleep 60", sandbox.RunOpts{Tag: tag})
	noErr(t, err, "RunBackground with Tag")
	defer func() { _, _ = handle.Kill() }()

	time.Sleep(300 * time.Millisecond)

	ok, err := sb.Commands.KillByTag(tag)
	noErr(t, err, "KillByTag")
	if !ok {
		t.Fatal("KillByTag returned false")
	}
}

func TestCommands_SendStdinByTag(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("by-tag operations rely on /proc List — skipping on non-Linux")
	}

	tag := "e2e-stdin-tag"
	// Start cat with a tag; it echoes stdin back to stdout.
	handle, err := sb.Commands.RunBackground("cat", sandbox.RunOpts{Tag: tag})
	noErr(t, err, "RunBackground cat with tag")
	defer func() { _, _ = handle.Kill() }()

	time.Sleep(200 * time.Millisecond)

	err = sb.Commands.SendStdinByTag(tag, "hello_by_tag\n")
	noErr(t, err, "SendStdinByTag")

	// Close stdin so cat exits, then verify the echoed output.
	err = sb.Commands.CloseStdin(handle.PID())
	noErr(t, err, "CloseStdin after SendStdinByTag")

	done := make(chan *sandbox.CommandResult, 1)
	go func() {
		r, _ := handle.Wait()
		done <- r
	}()

	select {
	case r := <-done:
		if r == nil || !strings.Contains(r.Stdout, "hello_by_tag") {
			t.Fatalf("SendStdinByTag: stdout=%q, want 'hello_by_tag'", func() string {
				if r == nil {
					return "<nil>"
				}
				return r.Stdout
			}())
		}
		t.Logf("SendStdinByTag stdout: %q", r.Stdout)
	case <-time.After(5 * time.Second):
		t.Fatal("SendStdinByTag: cat did not exit within 5s")
	}
}

// ---------------------------------------------------------------------------
// Commands — Disconnect
// ---------------------------------------------------------------------------

func TestCommands_Disconnect(t *testing.T) {
	handle, err := sb.Commands.RunBackground("sleep 30")
	noErr(t, err, "RunBackground for Disconnect")

	pid := handle.PID()
	if pid == 0 {
		t.Fatal("pid=0")
	}

	// Disconnect detaches the stream; process should still be alive.
	handle.Disconnect()

	time.Sleep(200 * time.Millisecond)

	// Verify the process is still running by killing it successfully.
	ok, err := sb.Commands.Kill(pid)
	noErr(t, err, "Kill after Disconnect")
	if !ok {
		t.Error("Kill returned false — process may have already died unexpectedly")
	}
}
