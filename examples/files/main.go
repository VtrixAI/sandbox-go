// files: 文件读写与编辑
package main

import (
	"context"
	"fmt"
	"log"

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

	// 写文件
	wr, err := sb.Write(ctx, "/tmp/hello.txt", "Hello, Sandbox!\nLine 2\n")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Written %d bytes\n", wr.BytesWritten)

	// 读文件
	rr, err := sb.Read(ctx, "/tmp/hello.txt")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Content (truncated=%v):\n%s\n", rr.Truncated, rr.Content)

	// 编辑文件（精确替换）
	er, err := sb.Edit(ctx, "/tmp/hello.txt", "Hello, Sandbox!", "Hello, World!")
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Edit: %s\n", er.Message)

	// 验证
	rr2, _ := sb.Read(ctx, "/tmp/hello.txt")
	fmt.Printf("After edit:\n%s\n", rr2.Content)

	// 综合：写代码文件再执行
	code := `#!/usr/bin/env python3
print("Hello from Python inside sandbox!")
`
	sb.Write(ctx, "/tmp/script.py", code)
	result, err := sb.Execute(ctx, "python3 /tmp/script.py", nil)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Script output: %s\n", result.Output)
}
