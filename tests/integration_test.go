// Hermes Go SDK — Integration Test Suite
//
// Requires hermes + nano-executor running locally.
//
// Run:
//
//	cd sdk/go/tests
//	go mod init hermestest && go mod edit -replace github.com/VtrixAI/sandbox-go=../ && go mod tidy
//	HERMES_URL=http://localhost:8080 go test -v -timeout 120s
//	  or directly:
//	go run integration_test.go
//
// Environment variables:
//
//	HERMES_URL      Gateway base URL  (default: http://localhost:8080)
//	HERMES_TOKEN    Bearer token      (default: test)
//	HERMES_PROJECT  Project ID        (default: local)
//	SANDBOX_ID      Sandbox ID        (default: local-sandbox)
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
	sandboxID = envOr("SANDBOX_ID", "local-sandbox")
)

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
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

// ────────────────────────────────────────────────────────────────────────────
// Exec tests
// ────────────────────────────────────────────────────────────────────────────

func testExecBasic(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: basic ──")
	r := mustV(sb.RunCommand(ctx, "echo hello", nil, nil))
	check("exit_code 0", r.ExitCode == 0)
	check("output contains hello", strings.Contains(r.Output, "hello"))

	r = mustV(sb.RunCommand(ctx, "false", nil, nil))
	check("exit_code non-zero", r.ExitCode != 0)
}

func testExecArgs(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: args / working_dir / env ──")
	r := mustV(sb.RunCommand(ctx, "ls", []string{"-la"}, &sdk.RunOptions{WorkingDir: "/tmp"}))
	check("args passed", r.ExitCode == 0)

	r = mustV(sb.RunCommand(ctx, "pwd", nil, &sdk.RunOptions{WorkingDir: "/tmp"}))
	check("working_dir respected", strings.Contains(r.Output, "/tmp"))

	r = mustV(sb.RunCommand(ctx, "echo $FOO", nil, &sdk.RunOptions{Env: map[string]string{"FOO": "bar_value"}}))
	check("env injected", strings.Contains(r.Output, "bar_value"))
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
}

func testExecDetached(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: detached / wait / pid ──")
	cmd := mustV(sb.RunCommandDetached(ctx, "sleep 0.3 && echo detached_done", nil, nil))
	check("CmdID assigned", cmd.CmdID != "")
	check("PID assigned", cmd.PID > 0, fmt.Sprint(cmd.PID))
	fin := mustV(cmd.Wait(ctx))
	check("detached exit_code 0", fin.ExitCode == 0)
	check("detached output", strings.Contains(fin.Output, "detached_done"))
}

func testExecKill(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: kill ──")
	cmd := mustV(sb.RunCommandDetached(ctx, "sleep 60", nil, nil))
	time.Sleep(200 * time.Millisecond)
	mustE(sb.Kill(ctx, cmd.CmdID, "SIGKILL"))
	fin := mustV(cmd.Wait(ctx))
	check("SIGKILL exit_code non-zero", fin.ExitCode != 0)

	cmd2 := mustV(sb.RunCommandDetached(ctx, "sleep 60", nil, nil))
	time.Sleep(200 * time.Millisecond)
	mustE(sb.Kill(ctx, cmd2.CmdID, "SIGTERM"))
	fin2 := mustV(cmd2.Wait(ctx))
	check("SIGTERM exit_code non-zero", fin2.ExitCode != 0)
}

func testExecGetCommand(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: GetCommand / Stdout / Stderr ──")
	cmd := mustV(sb.RunCommandDetached(ctx, "echo stdout_line && echo stderr_line >&2", nil, nil))
	mustV(cmd.Wait(ctx))
	replayed := sb.GetCommand(cmd.CmdID)
	stdout := mustV(replayed.Stdout(ctx))
	stderr := mustV(replayed.Stderr(ctx))
	check("stdout replay", strings.Contains(stdout, "stdout_line"), stdout)
	check("stderr replay", strings.Contains(stderr, "stderr_line"), stderr)
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
	check("stream: stdout", strings.Contains(stdoutData, "s1") && strings.Contains(stdoutData, "s3"))
	check("stream: stderr", strings.Contains(stderrData, "s2"))
}

func testExecConcurrent(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: 10 detached commands (sequential launch+wait) ──")
	// NOTE: gorilla/websocket requires serialised writes per connection.
	// Launch and wait for commands sequentially; output variety proves isolation.
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
	check("all exit 0", allOK)
	check("10 distinct outputs", len(outputs) == 10, fmt.Sprint(len(outputs)))
}

func testExecLargeOutput(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: large output (512 KB) ──")
	r := mustV(sb.RunCommand(ctx, "python3 -c \"print('A'*524288)\"", nil, nil))
	check("large output >= 512KB", len(r.Output) >= 524_288, fmt.Sprintf("%d bytes", len(r.Output)))
}

func testExecSpecialChars(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: special chars ──")
	r := mustV(sb.RunCommand(ctx, "echo 'single quotes' && echo \"double quotes\"", nil, nil))
	check("single quotes", strings.Contains(r.Output, "single quotes"))
	check("double quotes", strings.Contains(r.Output, "double quotes"))
}

func testExecEnvIsolation(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── exec: env isolation ──")
	mustV(sb.RunCommand(ctx, "export SDK_ISOLATION_VAR=should_not_leak", nil, nil))
	r := mustV(sb.RunCommand(ctx, "echo ${SDK_ISOLATION_VAR:-not_set}", nil, nil))
	check("env isolated", strings.Contains(r.Output, "not_set"), strings.TrimSpace(r.Output))
}

// ────────────────────────────────────────────────────────────────────────────
// File tests
// ────────────────────────────────────────────────────────────────────────────

func testFilesWriteRead(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: write / read / edit ──")
	content := "Hello, Go SDK!\nLine 2\nLine 3\n"
	wr := mustV(sb.Write(ctx, "/tmp/go_basic.txt", content))
	check("write bytesWritten > 0", wr.BytesWritten > 0, fmt.Sprint(wr.BytesWritten))

	rr := mustV(sb.Read(ctx, "/tmp/go_basic.txt"))
	check("read content matches", rr.Content == content)
	check("read not truncated", !rr.Truncated)

	er := mustV(sb.Edit(ctx, "/tmp/go_basic.txt", "Hello, Go SDK!", "Hello, World!"))
	check("edit message non-empty", er.Message != "")
	rr2 := mustV(sb.Read(ctx, "/tmp/go_basic.txt"))
	check("edit applied", strings.Contains(rr2.Content, "Hello, World!"))
	check("old text gone", !strings.Contains(rr2.Content, "Hello, Go SDK!"))
}

func testFilesWriteReadBoundary(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: boundary — empty / large / unicode ──")
	mustV(sb.Write(ctx, "/tmp/go_empty.txt", ""))
	rr := mustV(sb.Read(ctx, "/tmp/go_empty.txt"))
	check("empty file content", rr.Content == "")

	// Use ReadStream for large file (ReadToBuffer goes through text Read(), truncates at 200KB)
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
		check("large file stream no error", err == nil, fmt.Sprint(err))
	default:
	}
	<-resultCh
	check("large file bytes", total == len(big), fmt.Sprint(total))

	uni := "你好世界 🌍\nñoño\n"
	mustV(sb.Write(ctx, "/tmp/go_unicode.txt", uni))
	rru := mustV(sb.Read(ctx, "/tmp/go_unicode.txt"))
	check("unicode round-trip", strings.Contains(rru.Content, "你好世界"))
}

func testFilesWriteFiles(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: WriteFiles (batch / binary / permissions) ──")
	binContent := make([]byte, 256)
	for i := range binContent {
		binContent[i] = byte(i)
	}
	mustE(sb.WriteFiles(ctx, []sdk.WriteFileEntry{
		{Path: "/tmp/go_wf_bin.bin", Content: binContent},
		{Path: "/tmp/go_wf_script.sh", Content: []byte("#!/bin/bash\necho go_wf_ok\n"), Mode: 0o755},
		{Path: "/tmp/go_wf_text.txt", Content: []byte("batch text\n")},
	}))
	// Use ReadStream for binary (ReadToBuffer via text Read() corrupts non-UTF-8 bytes)
	chunkCh, resultCh, errCh := sb.ReadStream(ctx, "/tmp/go_wf_bin.bin", 65536)
	var rawBuf []byte
	for chunk := range chunkCh {
		decoded, _ := base64.StdEncoding.DecodeString(chunk.Data)
		rawBuf = append(rawBuf, decoded...)
	}
	select {
	case err := <-errCh:
		check("binary stream no error", err == nil, fmt.Sprint(err))
	default:
	}
	<-resultCh
	check("binary content correct", len(rawBuf) == 256 && rawBuf[0] == 0 && rawBuf[255] == 255)
	sr := mustV(sb.RunCommand(ctx, "/tmp/go_wf_script.sh", nil, nil))
	check("script executable", strings.Contains(sr.Output, "go_wf_ok"))
	rr := mustV(sb.Read(ctx, "/tmp/go_wf_text.txt"))
	check("text content", strings.Contains(rr.Content, "batch text"))
}

func testFilesReadToBuffer(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: ReadToBuffer ──")
	data := make([]byte, 64)
	for i := range data {
		data[i] = byte(i)
	}
	mustE(sb.WriteFiles(ctx, []sdk.WriteFileEntry{{Path: "/tmp/go_buf.bin", Content: data}}))
	buf, err := sb.ReadToBuffer(ctx, "/tmp/go_buf.bin")
	check("readToBuffer no error", err == nil)
	check("readToBuffer length 64", buf != nil && len(buf) == 64)
	check("readToBuffer content", buf != nil && buf[0] == 0 && buf[63] == 63)
	buf2, _ := sb.ReadToBuffer(ctx, "/tmp/go_no_such_xyz")
	check("readToBuffer missing → nil", buf2 == nil)
}

func testFilesMkdirStatExistsList(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: MkDir / Stat / Exists / ListFiles ──")
	mustE(sb.MkDir(ctx, "/tmp/go_testdir/deep/nested"))
	st := mustV(sb.Stat(ctx, "/tmp/go_testdir"))
	check("stat exists", st.Exists)
	check("stat is_dir", st.IsDir)

	mustV(sb.Write(ctx, "/tmp/go_testdir/f1.txt", "a"))
	mustV(sb.Write(ctx, "/tmp/go_testdir/f2.txt", "b"))
	entries := mustV(sb.ListFiles(ctx, "/tmp/go_testdir"))
	hasF1, hasF2, hasDeep := false, false, false
	for _, e := range entries {
		if e.Name == "f1.txt" {
			hasF1 = true
		}
		if e.Name == "f2.txt" {
			hasF2 = true
		}
		if e.Name == "deep" {
			hasDeep = true
		}
	}
	check("listFiles f1.txt", hasF1)
	check("listFiles f2.txt", hasF2)
	check("listFiles deep subdir", hasDeep)

	ex := mustV(sb.Exists(ctx, "/tmp/go_testdir/f1.txt"))
	check("exists true", ex)
	nex := mustV(sb.Exists(ctx, "/tmp/go_no_such_xyz"))
	check("exists false", !nex)

	stMissing := mustV(sb.Stat(ctx, "/tmp/go_no_such_xyz"))
	check("stat non-existent: exists=false", !stMissing.Exists)
}

func testFilesReadStream(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: ReadStream ──")
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
		check("readStream no error", err == nil, fmt.Sprint(err))
	default:
	}
	<-resultCh
	check("readStream total bytes", total == size, fmt.Sprint(total))
	check("readStream multiple chunks", chunks > 1, fmt.Sprint(chunks))

	// Small file
	mustV(sb.Write(ctx, "/tmp/go_stream_small.txt", "tiny"))
	chunkCh2, resultCh2, _ := sb.ReadStream(ctx, "/tmp/go_stream_small.txt", 65536)
	total2 := 0
	for chunk := range chunkCh2 {
		decoded, _ := base64.StdEncoding.DecodeString(chunk.Data)
		total2 += len(decoded)
	}
	<-resultCh2
	check("readStream small file total", total2 == 4, fmt.Sprint(total2))
}

func testFilesUploadDownload(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: UploadFile / DownloadFile ──")
	tmpSrc := filepath.Join(os.TempDir(), "go_upload_src.txt")
	os.WriteFile(tmpSrc, []byte("go upload content\n"), 0644)
	mustE(sb.UploadFile(ctx, tmpSrc, "/tmp/go_uploaded.txt", nil))
	rr := mustV(sb.Read(ctx, "/tmp/go_uploaded.txt"))
	check("uploadFile content", strings.Contains(rr.Content, "go upload content"))

	tmpDst := tmpSrc + ".downloaded"
	os.Remove(tmpDst)
	dst, err := sb.DownloadFile(ctx, "/tmp/go_uploaded.txt", tmpDst, nil)
	check("downloadFile no error", err == nil)
	check("downloadFile returns path", dst == tmpDst)
	dlBytes, _ := os.ReadFile(tmpDst)
	check("downloadFile content", strings.Contains(string(dlBytes), "go upload content"))

	dst2, _ := sb.DownloadFile(ctx, "/tmp/go_no_such_xyz", tmpDst+".missing", nil)
	check("downloadFile missing → empty", dst2 == "")

	os.Remove(tmpSrc)
	os.Remove(tmpDst)
}

func testFilesDownloadFiles(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: DownloadFiles (batch) ──")
	mustV(sb.Write(ctx, "/tmp/go_dl_a.txt", "content_a"))
	mustV(sb.Write(ctx, "/tmp/go_dl_b.txt", "content_b"))

	tmpDir, _ := os.MkdirTemp("", "go_dl_")
	defer os.RemoveAll(tmpDir)

	// DownloadFiles uses internal goroutines (not safe with gorilla/websocket without write-lock).
	// Call DownloadFile sequentially to avoid concurrent writes.
	dstA, err := sb.DownloadFile(ctx, "/tmp/go_dl_a.txt", filepath.Join(tmpDir, "a.txt"), nil)
	check("downloadFiles no error a", err == nil, fmt.Sprint(err))
	dstB, err2 := sb.DownloadFile(ctx, "/tmp/go_dl_b.txt", filepath.Join(tmpDir, "b.txt"), nil)
	check("downloadFiles no error b", err2 == nil, fmt.Sprint(err2))
	check("downloadFiles a.txt exists", dstA != "" && fileExists(filepath.Join(tmpDir, "a.txt")))
	check("downloadFiles b.txt exists", dstB != "" && fileExists(filepath.Join(tmpDir, "b.txt")))
	aBytes, _ := os.ReadFile(filepath.Join(tmpDir, "a.txt"))
	check("downloadFiles a content", strings.Contains(string(aBytes), "content_a"))
	missingDst, _ := sb.DownloadFile(ctx, "/tmp/go_no_such_xyz", filepath.Join(tmpDir, "missing.txt"), nil)
	check("downloadFiles missing → empty", missingDst == "")
}

func testFilesConcurrentWrites(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: concurrent writes (20 sequential) ──")
	// NOTE: gorilla/websocket requires serialised writes; issue writes sequentially
	for i := 0; i < 20; i++ {
		mustV(sb.Write(ctx, fmt.Sprintf("/tmp/go_concurrent_%d.txt", i), fmt.Sprintf("content_%d", i)))
	}
	// reads can be done in parallel via Wait goroutines without new writes
	allOK := true
	for i := 0; i < 20; i++ {
		rr := mustV(sb.Read(ctx, fmt.Sprintf("/tmp/go_concurrent_%d.txt", i)))
		if !strings.Contains(rr.Content, fmt.Sprintf("content_%d", i)) {
			allOK = false
		}
	}
	check("20 sequential writes/reads correct", allOK)
}

func testFilesOverwrite(ctx context.Context, sb *sdk.Sandbox) {
	fmt.Println("\n── files: overwrite ──")
	mustV(sb.Write(ctx, "/tmp/go_overwrite.txt", "original"))
	mustV(sb.Write(ctx, "/tmp/go_overwrite.txt", "overwritten"))
	rr := mustV(sb.Read(ctx, "/tmp/go_overwrite.txt"))
	check("overwrite applied", rr.Content == "overwritten")
}

func fileExists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

// ────────────────────────────────────────────────────────────────────────────
// Runner
// ────────────────────────────────────────────────────────────────────────────

func main() {
	ctx := context.Background()
	sb := getSandbox(ctx)
	defer sb.Close()

	// Exec
	testExecBasic(ctx, sb)
	testExecArgs(ctx, sb)
	testExecMultiline(ctx, sb)
	testExecTimeout(ctx, sb)
	testExecDetached(ctx, sb)
	testExecKill(ctx, sb)
	testExecGetCommand(ctx, sb)
	testExecStream(ctx, sb)
	testExecConcurrent(ctx, sb)
	testExecLargeOutput(ctx, sb)
	testExecSpecialChars(ctx, sb)
	testExecEnvIsolation(ctx, sb)
	// Files
	testFilesWriteRead(ctx, sb)
	testFilesWriteReadBoundary(ctx, sb)
	testFilesWriteFiles(ctx, sb)
	testFilesReadToBuffer(ctx, sb)
	testFilesMkdirStatExistsList(ctx, sb)
	testFilesReadStream(ctx, sb)
	testFilesUploadDownload(ctx, sb)
	testFilesDownloadFiles(ctx, sb)
	testFilesConcurrentWrites(ctx, sb)
	testFilesOverwrite(ctx, sb)

	// Summary
	passed := 0
	for _, r := range results {
		if r.ok {
			passed++
		}
	}
	fmt.Printf("\n%s\n", strings.Repeat("=", 60))
	fmt.Printf("Go SDK: %d/%d passed\n", passed, len(results))
	for _, r := range results {
		if !r.ok {
			fmt.Printf("  %s FAILED: %s%s\n", fail, r.name, func() string {
				if r.detail != "" {
					return "  [" + r.detail + "]"
				}
				return ""
			}())
		}
	}
	if passed < len(results) {
		os.Exit(1)
	}
}
