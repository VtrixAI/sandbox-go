// files: 文件读写、编辑、stat、列目录、upload/download、ReadStream
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	sandbox "github.com/VtrixAI/sandbox-go/src"
)

func main() {
	client := sandbox.NewClient(sandbox.ClientOptions{
		BaseURL:   "http://localhost:8080",
		Token:     "your-token",
		ServiceID: "seaclaw",
	})

	ctx := context.Background()

	sb, err := client.Create(ctx, sandbox.CreateOptions{UserID: "user-123"})
	if err != nil {
		log.Fatal(err)
	}
	defer sb.Close()

	fmt.Printf("Sandbox: %s\n", sb.Info.ID)

	// ── 写 / 读 / 编辑 ────────────────────────────────────
	wr, err := sb.Write(ctx, "/tmp/hello.txt", "Hello, Sandbox!\nLine 2\n")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Written %d bytes\n", wr.BytesWritten)

	rr, err := sb.Read(ctx, "/tmp/hello.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Read (truncated=%v):\n%s\n", rr.Truncated, rr.Content)

	er, err := sb.Edit(ctx, "/tmp/hello.txt", "Hello, Sandbox!", "Hello, World!")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Edit: %s\n", er.Message)

	// ── WriteFiles（批量 / 二进制 / 设权限）──────────────
	binData := make([]byte, 64)
	for i := range binData {
		binData[i] = byte(i)
	}
	err = sb.WriteFiles(ctx, []sandbox.WriteFileEntry{
		{Path: "/tmp/data.bin", Content: binData},
		{Path: "/tmp/run.sh", Content: []byte("#!/bin/bash\necho 'hello from script'\n"), Mode: 0o755},
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("WriteFiles done")

	// 验证可执行脚本
	scriptResult, err := sb.RunCommand(ctx, "/tmp/run.sh", nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Script output: %s\n", scriptResult.Output)

	// ── ReadToBuffer（读取原始字节）──────────────────────
	buf, err := sb.ReadToBuffer(ctx, "/tmp/data.bin")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("ReadToBuffer: %d bytes, first 4 = %v\n", len(buf), buf[:4])

	// 不存在的文件 → nil, nil
	missing, err := sb.ReadToBuffer(ctx, "/tmp/no_such_file")
	fmt.Printf("Missing file → %v (err=%v)\n", missing, err)

	// ── MkDir / Stat / Exists / ListFiles ────────────────
	if err := sb.MkDir(ctx, "/tmp/mydir/sub"); err != nil {
		log.Fatal(err)
	}
	fmt.Println("MkDir done")

	fi, err := sb.Stat(ctx, "/tmp/mydir")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Stat /tmp/mydir: exists=%v, is_dir=%v\n", fi.Exists, fi.IsDir)

	exists, err := sb.Exists(ctx, "/tmp/hello.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Exists /tmp/hello.txt: %v\n", exists)

	// 写几个文件到目录
	for _, name := range []string{"a.txt", "b.txt"} {
		sb.Write(ctx, "/tmp/mydir/"+name, "content of "+name)
	}
	entries, err := sb.ListFiles(ctx, "/tmp/mydir")
	if err != nil {
		log.Fatal(err)
	}
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name
	}
	fmt.Printf("ListFiles /tmp/mydir: %v\n", names)

	// ── ReadStream（大文件分块读取）──────────────────────
	sb.Write(ctx, "/tmp/big.txt", string(make([]byte, 100_000))) // 100 KB
	chunkCh, _, errCh := sb.ReadStream(ctx, "/tmp/big.txt", 32768)
	total := 0
	chunks := 0
	for chunk := range chunkCh {
		// chunk.Data is base64; decoded length is approx 3/4 of len
		total += len(chunk.Data)
		chunks++
	}
	if err := <-errCh; err != nil {
		log.Fatal(err)
	}
	fmt.Printf("ReadStream: %d raw chars in %d chunk(s)\n", total, chunks)

	// ── UploadFile / DownloadFile ─────────────────────────
	localSrc := "/tmp/sdk_upload_test.txt"
	if err := os.WriteFile(localSrc, []byte("uploaded content\n"), 0o644); err != nil {
		log.Fatal(err)
	}
	if err := sb.UploadFile(ctx, localSrc, "/tmp/uploaded.txt", nil); err != nil {
		log.Fatal(err)
	}
	fmt.Println("UploadFile done")

	localDst := "/tmp/sdk_download_test.txt"
	dst, err := sb.DownloadFile(ctx, "/tmp/uploaded.txt", localDst, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("DownloadFile → %s\n", dst)
	content, _ := os.ReadFile(dst)
	fmt.Printf("Downloaded content: %s\n", string(content))

	// 下载不存在的文件 → ""
	missingDst, err := sb.DownloadFile(ctx, "/tmp/nonexistent.txt", "/tmp/never.txt", nil)
	fmt.Printf("Download missing → %q (err=%v)\n", missingDst, err)

	// 综合：写代码文件再执行
	code := "#!/usr/bin/env python3\nprint('Hello from Python inside sandbox!')\n"
	sb.Write(ctx, "/tmp/script.py", code)
	runResult, err := sb.RunCommand(ctx, "python3 /tmp/script.py", nil, nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Script output: %s\n", runResult.Output)
}
