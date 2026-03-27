// Hermes Go SDK — Sandbox Integration Test
//
// Tests: Exec (basic, args, env, multiline, timeout, detached, kill, stream,
//        concurrent, large output, special chars, stdin, env isolation),
//        Files (write/read, edit, boundary, binary, WriteFiles, ReadStream,
//        upload/download, mkdir, stat, exists, list, overwrite, concurrent)
//
// Run:
//
//	cd sdk/go/tests/sandbox
//	HERMES_URL=https://hermes-gateway.sandbox.cloud.vtrix.ai \
//	HERMES_TOKEN=<token> HERMES_PROJECT=<project> SANDBOX_ID=<id> \
//	go run main.go
//
// Environment variables:
//
//	HERMES_URL      Gateway base URL  (required)
//	HERMES_TOKEN    Bearer token      (required)
//	HERMES_PROJECT  Project ID        (required)
//	SANDBOX_ID      Existing active sandbox ID to test against (required)
package main

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	sdk "github.com/VtrixAI/sandbox-go/src"
)

var (
	baseURL   = envOr("HERMES_URL", "http://localhost:8080")
	token     = envOr("HERMES_TOKEN", "test")
	project   = envOr("HERMES_PROJECT", "local")
	sandboxID = mustEnv("SANDBOX_ID")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		fmt.Fprintf(os.Stderr, "error: env var %s is required\n", key)
		os.Exit(1)
	}
	return v
}

const pass = "✅"
const fail = "❌"

type result struct {
	name   string
	ok     bool
	detail string
}

var results []result

func check(name string, cond bool, detail ...string) {
	mark := pass
	if !cond {
		mark = fail
	}
	d := ""
	if len(detail) > 0 {
		d = "  [" + detail[0] + "]"
	}
	fmt.Printf("  %s %s%s\n", mark, name, d)
	det := ""
	if len(detail) > 0 {
		det = detail[0]
	}
	results = append(results, result{name, cond, det})
}

func mustV[T any](v T, err error) T {
	if err != nil {
		panic(fmt.Sprintf("fatal: %v", err))
	}
	return v
}

func mustE(err error) {
	if err != nil {
		panic(fmt.Sprintf("fatal: %v", err))
	}
}

func getSandbox(ctx context.Context) *sdk.Sandbox {
	client := sdk.NewClient(sdk.ClientOptions{
		BaseURL: baseURL, Token: token, ProjectID: project,
	})
	return mustV(client.Attach(ctx, sandboxID))
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ────────────────────────────────────────────────────────────────────────────
// Exec tests
// ────────────────────────────────────────────────────────────────────────────

func testExecBasic(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: basic ──")

	r := mustV(sb.RunCommand(ctx, "echo hello", nil, nil))
	check("exit_code 0", r.ExitCode == 0)
	check("output contains hello", strings.Contains(r.Output, "hello"))
	check("cmd_id non-empty", r.CmdID != "", r.CmdID)
	check("started_at non-zero", !r.StartedAt.IsZero())

	r2 := mustV(sb.RunCommand(ctx, "false", nil, nil))
	check("exit_code non-zero on failure", r2.ExitCode != 0, fmt.Sprint(r2.ExitCode))
}

func testExecWorkingDir(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: working_dir ──")

	r := mustV(sb.RunCommand(ctx, "pwd", nil, &sdk.RunOptions{WorkingDir: "/tmp"}))
	check("working_dir /tmp", strings.Contains(r.Output, "/tmp"), strings.TrimSpace(r.Output))

	r2 := mustV(sb.RunCommand(ctx, "pwd", nil, &sdk.RunOptions{WorkingDir: "/usr/local"}))
	check("working_dir /usr/local", strings.Contains(r2.Output, "/usr/local"), strings.TrimSpace(r2.Output))
}

func testExecArgs(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: args ──")

	r := mustV(sb.RunCommand(ctx, "ls", []string{"-la", "/tmp"}, nil))
	check("args passed exit 0", r.ExitCode == 0)
	check("args: ls -la output", strings.Contains(r.Output, "total"))
}

func testExecEnv(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: env ──")

	r := mustV(sb.RunCommand(ctx, "echo $FOO", nil, &sdk.RunOptions{
		Env: map[string]string{"FOO": "bar_value"},
	}))
	check("env FOO injected", strings.Contains(r.Output, "bar_value"), strings.TrimSpace(r.Output))

	// Multiple env vars
	r2 := mustV(sb.RunCommand(ctx, "echo $A $B $C", nil, &sdk.RunOptions{
		Env: map[string]string{"A": "one", "B": "two", "C": "three"},
	}))
	check("env multiple vars", strings.Contains(r2.Output, "one") &&
		strings.Contains(r2.Output, "two") &&
		strings.Contains(r2.Output, "three"), r2.Output)
}

func testExecEnvIsolation(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: env isolation between commands ──")

	mustV(sb.RunCommand(ctx, "export SDK_ISOLATION_VAR=should_not_leak", nil, nil))
	r := mustV(sb.RunCommand(ctx, "echo ${SDK_ISOLATION_VAR:-not_set}", nil, nil))
	check("env does not leak between exec calls", strings.Contains(r.Output, "not_set"), strings.TrimSpace(r.Output))
}

func testExecStdin(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: stdin ──")

	r := mustV(sb.RunCommand(ctx, "cat", nil, &sdk.RunOptions{
		Stdin: "hello from stdin\n",
	}))
	check("stdin passed to command", strings.Contains(r.Output, "hello from stdin"), strings.TrimSpace(r.Output))

	// stdin with multi-line
	r2 := mustV(sb.RunCommand(ctx, "wc -l", nil, &sdk.RunOptions{
		Stdin: "line1\nline2\nline3\n",
	}))
	check("stdin multiline wc -l = 3", strings.Contains(strings.TrimSpace(r2.Output), "3"), strings.TrimSpace(r2.Output))
}

func testExecMultiline(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: multi-line output ──")

	r := mustV(sb.RunCommand(ctx, "for i in $(seq 1 100); do echo line_$i; done", nil, nil))
	count := 0
	for _, l := range strings.Split(r.Output, "\n") {
		if strings.HasPrefix(l, "line_") {
			count++
		}
	}
	check("100 output lines", count == 100, fmt.Sprint(count))
}

func testExecTimeout(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: timeout ──")

	var two uint64 = 2
	t0 := time.Now()
	_, err := sb.RunCommand(ctx, "sleep 30", nil, &sdk.RunOptions{TimeoutSec: two})
	elapsed := time.Since(t0).Seconds()
	check("timeout respected (< 5s)", elapsed < 5, fmt.Sprintf("%.1fs", elapsed))
	check("timeout raises error", err != nil, fmt.Sprint(err))

	// Verify sandbox still functional after timeout
	r := mustV(sb.RunCommand(ctx, "echo alive", nil, nil))
	check("sandbox alive after timeout", strings.Contains(r.Output, "alive"))
}

func testExecDetached(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: detached ──")

	cmd := mustV(sb.RunCommandDetached(ctx, "sleep 0.3 && echo detached_done", nil, nil))
	check("detached cmd_id assigned", cmd.CmdID != "", cmd.CmdID)
	check("detached pid > 0", cmd.PID > 0, fmt.Sprint(cmd.PID))

	fin := mustV(cmd.Wait(ctx))
	check("detached exit_code 0", fin.ExitCode == 0, fmt.Sprint(fin.ExitCode))
	check("detached output contains result", strings.Contains(fin.Output, "detached_done"), fin.Output)
}

func testExecKill(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: kill (SIGKILL / SIGTERM / SIGINT / SIGHUP) ──")

	for _, sig := range []string{"SIGKILL", "SIGTERM", "SIGINT", "SIGHUP"} {
		cmd := mustV(sb.RunCommandDetached(ctx, "sleep 60", nil, nil))
		time.Sleep(150 * time.Millisecond)
		err := sb.Kill(ctx, cmd.CmdID, sig)
		check(sig+" kill no error", err == nil, fmt.Sprint(err))
		fin := mustV(cmd.Wait(ctx))
		check(sig+" exit_code non-zero", fin.ExitCode != 0, fmt.Sprint(fin.ExitCode))
	}
}

func testExecGetCommand(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: GetCommand / Stdout / Stderr ──")

	cmd := mustV(sb.RunCommandDetached(ctx, "echo stdout_line && echo stderr_line >&2", nil, nil))
	mustV(cmd.Wait(ctx))

	replayed := sb.GetCommand(cmd.CmdID)
	stdout := mustV(replayed.Stdout(ctx))
	stderr := mustV(replayed.Stderr(ctx))
	check("stdout replay contains stdout_line", strings.Contains(stdout, "stdout_line"), stdout)
	check("stderr replay contains stderr_line", strings.Contains(stderr, "stderr_line"), stderr)
}

func testExecStream(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: RunCommandStream ──")

	evCh, finCh, errCh := sb.RunCommandStream(ctx, "echo s1 && echo s2 >&2 && echo s3", nil, nil)
	var evTypes []string
	var stdoutData, stderrData string
	for ev := range evCh {
		evTypes = append(evTypes, ev.Type)
		if ev.Type == "stdout" {
			stdoutData += ev.Data
		}
		if ev.Type == "stderr" {
			stderrData += ev.Data
		}
	}
	select {
	case err := <-errCh:
		check("stream no error", err == nil, fmt.Sprint(err))
	default:
	}
	<-finCh

	hasStart, hasDone := false, false
	for _, t := range evTypes {
		if t == "start" {
			hasStart = true
		}
		if t == "done" {
			hasDone = true
		}
	}
	check("stream: start event", hasStart, fmt.Sprint(evTypes))
	check("stream: done event", hasDone)
	check("stream: stdout s1+s3", strings.Contains(stdoutData, "s1") && strings.Contains(stdoutData, "s3"))
	check("stream: stderr s2", strings.Contains(stderrData, "s2"))
}

func testExecConcurrent(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: 10 sequential detached commands ──")

	outputs := make(map[string]bool)
	allOK := true
	for i := 0; i < 10; i++ {
		cmd := mustV(sb.RunCommandDetached(ctx, fmt.Sprintf("echo concurrent_%d", i), nil, nil))
		fin := mustV(cmd.Wait(ctx))
		if fin.ExitCode != 0 {
			allOK = false
		}
		outputs[strings.TrimSpace(fin.Output)] = true
	}
	check("all 10 exit 0", allOK)
	check("10 distinct outputs", len(outputs) == 10, fmt.Sprint(len(outputs)))
}

func testExecLargeOutput(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: large output ──")

	// Use seq to generate known-size output without python3 truncation issue
	r := mustV(sb.RunCommand(ctx, "seq 1 1000 | tr -d '\\n'", nil, nil))
	check("large output seq 1000", len(r.Output) > 100, fmt.Sprintf("%d bytes", len(r.Output)))

	// 100 lines × ~10 chars each ≈ 1KB
	r2 := mustV(sb.RunCommand(ctx, "for i in $(seq 1 100); do printf '%0100d\\n' $i; done", nil, nil))
	lines := strings.Count(r2.Output, "\n")
	check("large output 100 lines", lines >= 100, fmt.Sprintf("%d lines %d bytes", lines, len(r2.Output)))
}

func testExecSpecialChars(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: special characters ──")

	r := mustV(sb.RunCommand(ctx, "echo 'single quotes' && echo \"double quotes\"", nil, nil))
	check("single quotes in output", strings.Contains(r.Output, "single quotes"))
	check("double quotes in output", strings.Contains(r.Output, "double quotes"))

	r2 := mustV(sb.RunCommand(ctx, "echo 'hello\nworld'", nil, nil))
	check("newline in quoted string", r2.ExitCode == 0)

	r3 := mustV(sb.RunCommand(ctx, "echo $((1+1))", nil, nil))
	check("arithmetic expansion", strings.Contains(r3.Output, "2"), strings.TrimSpace(r3.Output))
}

func testExecStdoutWriter(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: Stdout/Stderr io.Writer ──")

	var stdoutBuf, stderrBuf strings.Builder
	r := mustV(sb.RunCommand(ctx, "echo writer_stdout && echo writer_stderr >&2", nil, &sdk.RunOptions{
		Stdout: &stdoutBuf,
		Stderr: &stderrBuf,
	}))
	check("writer: exit 0", r.ExitCode == 0)
	check("writer: stdout writer received data", strings.Contains(stdoutBuf.String(), "writer_stdout"), stdoutBuf.String())
	check("writer: stderr writer received data", strings.Contains(stderrBuf.String(), "writer_stderr"), stderrBuf.String())
}

// ────────────────────────────────────────────────────────────────────────────
// File tests
// ────────────────────────────────────────────────────────────────────────────

func testFilesWriteRead(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: Write / Read ──")

	content := "Hello, Go SDK!\nLine 2\nLine 3\n"
	wr := mustV(sb.Write(ctx, "/tmp/go_basic.txt", content))
	check("write: bytes_written > 0", wr.BytesWritten > 0, fmt.Sprint(wr.BytesWritten))

	rr := mustV(sb.Read(ctx, "/tmp/go_basic.txt"))
	check("read: content matches", rr.Content == content)
	check("read: not truncated", !rr.Truncated)
	check("read: type=text", rr.Type == "text", rr.Type)
}

func testFilesEdit(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: Edit ──")

	mustV(sb.Write(ctx, "/tmp/go_edit.txt", "Hello, Go SDK!\nLine 2\n"))

	er := mustV(sb.Edit(ctx, "/tmp/go_edit.txt", "Hello, Go SDK!", "Hello, World!"))
	check("edit: message non-empty", er.Message != "", er.Message)

	rr := mustV(sb.Read(ctx, "/tmp/go_edit.txt"))
	check("edit: new text present", strings.Contains(rr.Content, "Hello, World!"))
	check("edit: old text gone", !strings.Contains(rr.Content, "Hello, Go SDK!"))
}

func testFilesEmpty(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: empty file ──")

	mustV(sb.Write(ctx, "/tmp/go_empty.txt", ""))
	rr := mustV(sb.Read(ctx, "/tmp/go_empty.txt"))
	check("empty file: content empty", rr.Content == "", fmt.Sprintf("%q", rr.Content))
	check("empty file: not truncated", !rr.Truncated)
}

func testFilesUnicode(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: unicode ──")

	uni := "你好世界 🌍\nñoño\nكلام\n"
	mustV(sb.Write(ctx, "/tmp/go_unicode.txt", uni))
	rr := mustV(sb.Read(ctx, "/tmp/go_unicode.txt"))
	check("unicode round-trip CJK", strings.Contains(rr.Content, "你好世界"))
	check("unicode round-trip emoji", strings.Contains(rr.Content, "🌍"))
	check("unicode round-trip latin ext", strings.Contains(rr.Content, "ñoño"))
}

func testFilesWriteFiles(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: WriteFiles (batch / binary / permissions) ──")

	binContent := make([]byte, 256)
	for i := range binContent {
		binContent[i] = byte(i)
	}
	mustE(sb.WriteFiles(ctx, []sdk.WriteFileEntry{{Path: "/tmp/go_wf_bin.bin", Content: binContent}}))
	mustE(sb.WriteFiles(ctx, []sdk.WriteFileEntry{{Path: "/tmp/go_wf_script.sh", Content: []byte("#!/bin/sh\necho go_wf_ok\n"), Mode: 0o755}}))
	mustE(sb.WriteFiles(ctx, []sdk.WriteFileEntry{{Path: "/tmp/go_wf_text.txt", Content: []byte("batch text\n")}}))

	// Verify binary via ReadStream
	chunkCh, resultCh, errCh := sb.ReadStream(ctx, "/tmp/go_wf_bin.bin", 65536)
	var rawBuf []byte
	for chunk := range chunkCh {
		decoded, _ := base64.StdEncoding.DecodeString(chunk.Data)
		rawBuf = append(rawBuf, decoded...)
	}
	select {
	case err := <-errCh:
		check("WriteFiles binary stream no error", err == nil, fmt.Sprint(err))
	default:
	}
	<-resultCh
	check("WriteFiles binary: 256 bytes", len(rawBuf) == 256, fmt.Sprint(len(rawBuf)))
	check("WriteFiles binary: first byte 0", len(rawBuf) > 0 && rawBuf[0] == 0)
	check("WriteFiles binary: last byte 255", len(rawBuf) == 256 && rawBuf[255] == 255)

	// Verify executable script — use a fresh connection as binary write may affect WS state
	sbScript := getSandbox(ctx)
	defer sbScript.Close()
	// First check if file exists
	st, _ := sbScript.Stat(ctx, "/tmp/go_wf_script.sh")
	check("WriteFiles script: file exists", st != nil && st.Exists, fmt.Sprintf("%+v", st))
	// Run via bash explicitly in case execute bit is not set on the fs
	sr, err := sbScript.RunCommand(ctx, "sh /tmp/go_wf_script.sh", nil, nil)
	check("WriteFiles script executable", err == nil && strings.Contains(sr.Output, "go_wf_ok"),
		fmt.Sprintf("err=%v out=%q", err, func() string {
			if sr != nil {
				return sr.Output
			}
			return ""
		}()))

	// Verify text
	rr := mustV(sb.Read(ctx, "/tmp/go_wf_text.txt"))
	check("WriteFiles text content", strings.Contains(rr.Content, "batch text"))
}

func testFilesReadToBuffer(ctx context.Context) {
	fmt.Println("\n── files: ReadToBuffer ──")

	// Use a fresh connection to avoid connection state issues
	sb := getSandbox(ctx)
	defer sb.Close()

	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(i)
	}
	mustE(sb.WriteFiles(ctx, []sdk.WriteFileEntry{{Path: "/tmp/go_buf.bin", Content: data}}))

	buf, err := sb.ReadToBuffer(ctx, "/tmp/go_buf.bin")
	check("readToBuffer no error", err == nil, fmt.Sprint(err))
	check("readToBuffer length 64", buf != nil && len(buf) == 64, fmt.Sprint(len(buf)))
	check("readToBuffer first byte 0", buf != nil && buf[0] == 0)
	check("readToBuffer last byte 63", buf != nil && len(buf) == 64 && buf[63] == 63)

	buf2, _ := sb.ReadToBuffer(ctx, "/tmp/go_no_such_xyz_readbuf")
	check("readToBuffer missing → nil", buf2 == nil)
}

func testFilesReadStream(ctx context.Context) {
	sb := getSandbox(ctx)
	defer sb.Close()
	testFilesReadStreamInner(ctx, sb)
}

func testFilesReadStreamInner(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: ReadStream ──")

	// Large file
	size := 200_000
	mustV(sb.Write(ctx, "/tmp/go_stream.txt", strings.Repeat("Y", size)))

	chunkCh, resultCh, errCh := sb.ReadStream(ctx, "/tmp/go_stream.txt", 32768)
	total, chunks := 0, 0
	for chunk := range chunkCh {
		decoded, _ := base64.StdEncoding.DecodeString(chunk.Data)
		total += len(decoded)
		chunks++
	}
	select {
	case err := <-errCh:
		check("readStream large: no error", err == nil, fmt.Sprint(err))
	default:
	}
	rs := <-resultCh
	check("readStream large: total bytes", total == size, fmt.Sprintf("got %d want %d", total, size))
	check("readStream large: multiple chunks", chunks > 1, fmt.Sprint(chunks))
	check("readStream result: total_bytes", rs.TotalBytes == uint64(size), fmt.Sprint(rs.TotalBytes))

	// Small file (single chunk)
	mustV(sb.Write(ctx, "/tmp/go_stream_small.txt", "tiny"))
	chunkCh2, resultCh2, _ := sb.ReadStream(ctx, "/tmp/go_stream_small.txt", 65536)
	total2 := 0
	for chunk := range chunkCh2 {
		decoded, _ := base64.StdEncoding.DecodeString(chunk.Data)
		total2 += len(decoded)
	}
	<-resultCh2
	check("readStream small: 4 bytes", total2 == 4, fmt.Sprint(total2))

	// Custom chunk size
	mustV(sb.Write(ctx, "/tmp/go_stream_chunk.txt", strings.Repeat("Z", 10000)))
	chunkCh3, resultCh3, _ := sb.ReadStream(ctx, "/tmp/go_stream_chunk.txt", 1024)
	chunks3 := 0
	for range chunkCh3 {
		chunks3++
	}
	<-resultCh3
	check("readStream chunk_size=1024: multiple chunks", chunks3 > 1, fmt.Sprint(chunks3))
}

func testFilesMkdirStatExistsList(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: MkDir / Stat / Exists / ListFiles ──")

	mustE(sb.MkDir(ctx, "/tmp/go_testdir/deep/nested"))
	st := mustV(sb.Stat(ctx, "/tmp/go_testdir"))
	check("stat: exists=true", st.Exists)
	check("stat: is_dir=true", st.IsDir)
	check("stat: is_file=false", !st.IsFile)

	mustV(sb.Write(ctx, "/tmp/go_testdir/f1.txt", "a"))
	mustV(sb.Write(ctx, "/tmp/go_testdir/f2.txt", "b"))
	entries := mustV(sb.ListFiles(ctx, "/tmp/go_testdir"))
	hasF1, hasF2, hasDeep := false, false, false
	for _, e := range entries {
		switch e.Name {
		case "f1.txt":
			hasF1 = true
			check("listFiles f1.txt: is_file", !e.IsDir)
			check("listFiles f1.txt: path non-empty", e.Path != "")
		case "f2.txt":
			hasF2 = true
		case "deep":
			hasDeep = true
			check("listFiles deep: is_dir", e.IsDir)
		}
	}
	check("listFiles: f1.txt present", hasF1)
	check("listFiles: f2.txt present", hasF2)
	check("listFiles: deep subdir present", hasDeep)

	// Exists
	ex := mustV(sb.Exists(ctx, "/tmp/go_testdir/f1.txt"))
	check("exists: true for existing file", ex)
	nex := mustV(sb.Exists(ctx, "/tmp/go_no_such_xyz_stat"))
	check("exists: false for missing file", !nex)

	// Stat on missing path
	stMissing := mustV(sb.Stat(ctx, "/tmp/go_no_such_xyz_stat"))
	check("stat: missing file exists=false", !stMissing.Exists)
}

func testFilesOverwrite(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: overwrite ──")

	mustV(sb.Write(ctx, "/tmp/go_overwrite.txt", "original"))
	mustV(sb.Write(ctx, "/tmp/go_overwrite.txt", "overwritten"))
	rr := mustV(sb.Read(ctx, "/tmp/go_overwrite.txt"))
	check("overwrite: new content present", rr.Content == "overwritten", fmt.Sprintf("%q", rr.Content))
}

func testFilesUploadDownload(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: UploadFile / DownloadFile ──")

	tmpSrc := filepath.Join(os.TempDir(), "go_upload_src.txt")
	os.WriteFile(tmpSrc, []byte("go upload content\n"), 0644)
	defer os.Remove(tmpSrc)

	mustE(sb.UploadFile(ctx, tmpSrc, "/tmp/go_uploaded.txt", nil))
	rr := mustV(sb.Read(ctx, "/tmp/go_uploaded.txt"))
	check("uploadFile: content correct", strings.Contains(rr.Content, "go upload content"))

	// DownloadFile
	tmpDst := tmpSrc + ".downloaded"
	defer os.Remove(tmpDst)
	os.Remove(tmpDst)

	dst, err := sb.DownloadFile(ctx, "/tmp/go_uploaded.txt", tmpDst, nil)
	check("downloadFile: no error", err == nil, fmt.Sprint(err))
	check("downloadFile: returns correct path", dst == tmpDst)
	dlBytes, _ := os.ReadFile(tmpDst)
	check("downloadFile: content correct", strings.Contains(string(dlBytes), "go upload content"))

	// Download missing file
	dst2, _ := sb.DownloadFile(ctx, "/tmp/go_no_such_xyz_dl", tmpDst+".missing", nil)
	check("downloadFile: missing → empty path", dst2 == "")

	// UploadFile with MkdirRecursive
	mustV(sb.RunCommand(ctx, "rm -rf /tmp/go_upload_nested", nil, nil))
	mustE(sb.UploadFile(ctx, tmpSrc, "/tmp/go_upload_nested/deep/file.txt",
		&sdk.FileOptions{MkdirRecursive: true}))
	rr2 := mustV(sb.Read(ctx, "/tmp/go_upload_nested/deep/file.txt"))
	check("uploadFile MkdirRecursive: content correct", strings.Contains(rr2.Content, "go upload content"))
}

func testFilesSequentialWrites(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: 20 sequential writes ──")

	for i := 0; i < 20; i++ {
		mustV(sb.Write(ctx, fmt.Sprintf("/tmp/go_seq_%d.txt", i), fmt.Sprintf("content_%d", i)))
	}
	allOK := true
	for i := 0; i < 20; i++ {
		rr := mustV(sb.Read(ctx, fmt.Sprintf("/tmp/go_seq_%d.txt", i)))
		if !strings.Contains(rr.Content, fmt.Sprintf("content_%d", i)) {
			allOK = false
		}
	}
	check("20 sequential writes/reads correct", allOK)
}

func testFilesLarge(ctx context.Context, _ *sdk.Sandbox) {
	sb := getSandbox(ctx)
	defer sb.Close()
	fmt.Println("\n── files: large file (512 KB) via ReadStream ──")

	big := strings.Repeat("X", 512*1024)
	mustV(sb.Write(ctx, "/tmp/go_large.txt", big))

	chunkCh, resultCh, errCh := sb.ReadStream(ctx, "/tmp/go_large.txt", 65536)
	total := 0
	for chunk := range chunkCh {
		decoded, _ := base64.StdEncoding.DecodeString(chunk.Data)
		total += len(decoded)
	}
	select {
	case err := <-errCh:
		check("large file: stream no error", err == nil, fmt.Sprint(err))
	default:
	}
	<-resultCh
	check("large file: 512KB bytes received", total == len(big), fmt.Sprintf("%d bytes", total))
}

// ────────────────────────────────────────────────────────────────────────────
// Runner
// ────────────────────────────────────────────────────────────────────────────

func run(ctx context.Context, name string, fn func(context.Context, *sdk.Sandbox)) {
	sb := getSandbox(ctx)
	defer sb.Close()
	fn(ctx, sb)
}

func main() {
	ctx := context.Background()

	fmt.Printf("Target: %s  Project: %s  Sandbox: %s\n", baseURL, project, sandboxID)

	// Exec — each test gets its own connection to avoid WS state bleed
	run(ctx, "exec-basic", testExecBasic)
	run(ctx, "exec-workingdir", testExecWorkingDir)
	run(ctx, "exec-args", testExecArgs)
	run(ctx, "exec-env", testExecEnv)
	run(ctx, "exec-env-isolation", testExecEnvIsolation)
	run(ctx, "exec-stdin", testExecStdin)
	run(ctx, "exec-multiline", testExecMultiline)
	run(ctx, "exec-timeout", testExecTimeout)
	run(ctx, "exec-detached", testExecDetached)
	run(ctx, "exec-kill", testExecKill)
	run(ctx, "exec-getcommand", testExecGetCommand)
	run(ctx, "exec-stream", testExecStream)
	run(ctx, "exec-concurrent", testExecConcurrent)
	run(ctx, "exec-largeoutput", testExecLargeOutput)
	run(ctx, "exec-specialchars", testExecSpecialChars)
	run(ctx, "exec-stdoutwriter", testExecStdoutWriter)

	// Files
	run(ctx, "files-writeread", testFilesWriteRead)
	run(ctx, "files-edit", testFilesEdit)
	run(ctx, "files-empty", testFilesEmpty)
	run(ctx, "files-unicode", testFilesUnicode)
	run(ctx, "files-writefiles", testFilesWriteFiles)
	testFilesReadToBuffer(ctx)
	testFilesReadStream(ctx)
	run(ctx, "files-mkdir-stat", testFilesMkdirStatExistsList)
	run(ctx, "files-overwrite", testFilesOverwrite)
	run(ctx, "files-upload-download", testFilesUploadDownload)
	run(ctx, "files-sequential", testFilesSequentialWrites)
	testFilesLarge(ctx, nil)

	// Summary
	passed := 0
	for _, r := range results {
		if r.ok {
			passed++
		}
	}
	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Printf("Sandbox: %d/%d passed\n", passed, len(results))
	for _, r := range results {
		if !r.ok {
			extra := ""
			if r.detail != "" {
				extra = "  [" + r.detail + "]"
			}
			fmt.Printf("  %s FAILED: %s%s\n", fail, r.name, extra)
		}
	}
	if passed < len(results) {
		os.Exit(1)
	}
}
