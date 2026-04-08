package tests

import (
	"strings"
	"testing"
	"time"

	sandbox "github.com/VtrixAI/sandbox-go"
)

// ---------------------------------------------------------------------------
// PTY — basic create / kill / resize
// ---------------------------------------------------------------------------

func TestPty_Create_And_Kill(t *testing.T) {
	handle, err := sb.Pty.Create(sandbox.PtySize{Rows: 24, Cols: 80})
	noErr(t, err, "Pty.Create")

	if handle.PID() == 0 {
		t.Fatal("Pty.Create: pid=0")
	}
	t.Logf("PTY started pid=%d", handle.PID())

	ok, err := sb.Pty.Kill(handle.PID())
	noErr(t, err, "Pty.Kill")
	if !ok {
		t.Fatal("Pty.Kill returned false")
	}
}

func TestPty_Resize(t *testing.T) {
	handle, err := sb.Pty.Create(sandbox.PtySize{Rows: 24, Cols: 80})
	noErr(t, err, "Pty.Create for Resize")
	defer func() { _, _ = sb.Pty.Kill(handle.PID()) }()

	err = sb.Pty.Resize(handle.PID(), sandbox.PtySize{Rows: 40, Cols: 200})
	noErr(t, err, "Pty.Resize")
}

func TestPty_Create_CustomCmd(t *testing.T) {
	handle, err := sb.Pty.Create(
		sandbox.PtySize{Rows: 24, Cols: 80},
		sandbox.PtyCreateOpts{Cmd: "/bin/sh"},
	)
	noErr(t, err, "Pty.Create /bin/sh")
	defer func() { _, _ = sb.Pty.Kill(handle.PID()) }()

	if handle.PID() == 0 {
		t.Fatal("pid=0")
	}
}

// ---------------------------------------------------------------------------
// PTY — options
// ---------------------------------------------------------------------------

func TestPty_Create_WithEnvs(t *testing.T) {
	handle, err := sb.Pty.Create(
		sandbox.PtySize{Rows: 24, Cols: 80},
		sandbox.PtyCreateOpts{Envs: map[string]string{"E2E_PTY_VAR": "pty_env_ok"}},
	)
	noErr(t, err, "Pty.Create with Envs")
	defer func() { _, _ = sb.Pty.Kill(handle.PID()) }()

	if handle.PID() == 0 {
		t.Fatal("pid=0")
	}
	t.Logf("Pty with Envs pid=%d", handle.PID())
}

func TestPty_Create_WithCwd(t *testing.T) {
	handle, err := sb.Pty.Create(
		sandbox.PtySize{Rows: 24, Cols: 80},
		sandbox.PtyCreateOpts{Cwd: "/tmp"},
	)
	noErr(t, err, "Pty.Create with Cwd")
	defer func() { _, _ = sb.Pty.Kill(handle.PID()) }()

	if handle.PID() == 0 {
		t.Fatal("pid=0")
	}
	t.Logf("Pty with Cwd=/tmp pid=%d", handle.PID())
}

// ---------------------------------------------------------------------------
// PTY — input / output
// ---------------------------------------------------------------------------

func TestPty_SendInput(t *testing.T) {
	handle, err := sb.Pty.Create(sandbox.PtySize{Rows: 24, Cols: 80})
	noErr(t, err, "Pty.Create for SendInput")
	defer func() { _, _ = sb.Pty.Kill(handle.PID()) }()

	time.Sleep(200 * time.Millisecond)

	err = sb.Pty.SendInput(handle.PID(), []byte("echo pty_input_test\n"))
	noErr(t, err, "Pty.SendInput")
}

func TestPty_SendInput_ReadOutput(t *testing.T) {
	handle, err := sb.Pty.Create(sandbox.PtySize{Rows: 24, Cols: 80})
	noErr(t, err, "Pty.Create for SendInput ReadOutput")
	defer func() { _, _ = sb.Pty.Kill(handle.PID()) }()

	// Give bash time to initialise prompt.
	time.Sleep(300 * time.Millisecond)

	err = sb.Pty.SendInput(handle.PID(), []byte("echo pty_echo_test\n"))
	noErr(t, err, "Pty.SendInput")

	// Collect output briefly.
	done := make(chan string, 1)
	go func() {
		result, _ := handle.Wait()
		if result != nil {
			done <- result.Stdout
		}
	}()

	time.Sleep(500 * time.Millisecond)
	_, _ = sb.Pty.Kill(handle.PID())

	select {
	case out := <-done:
		t.Logf("PTY output: %q", out)
		if !strings.Contains(out, "pty_echo_test") {
			t.Errorf("PTY stdout missing 'pty_echo_test': %q", out)
		}
	case <-time.After(5 * time.Second):
		t.Log("PTY Wait timed out — kill may have raced with Wait")
	}
}

// ---------------------------------------------------------------------------
// PTY — wait / exit
// ---------------------------------------------------------------------------

func TestPty_Create_And_Wait(t *testing.T) {
	handle, err := sb.Pty.Create(sandbox.PtySize{Rows: 24, Cols: 80})
	noErr(t, err, "Pty.Create")

	time.Sleep(300 * time.Millisecond)

	// Send exit to bash so it terminates cleanly.
	err = sb.Pty.SendInput(handle.PID(), []byte("exit\n"))
	noErr(t, err, "Pty.SendInput exit")

	done := make(chan *sandbox.CommandResult, 1)
	go func() {
		r, _ := handle.Wait()
		done <- r
	}()

	select {
	case r := <-done:
		if r != nil {
			t.Logf("PTY Wait ExitCode=%d", r.ExitCode)
			if r.ExitCode != 0 {
				t.Errorf("expected exit code 0 after 'exit', got %d", r.ExitCode)
			}
		}
	case <-time.After(10 * time.Second):
		t.Log("PTY Wait timed out waiting for exit — killing")
		_, _ = sb.Pty.Kill(handle.PID())
	}
}
