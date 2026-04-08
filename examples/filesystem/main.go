// filesystem demonstrates file and directory operations.
//
// Run:
//
//	SANDBOX_API_KEY=your-key SANDBOX_BASE_URL=https://api.sandbox.vtrix.ai go run ./examples/filesystem
package main

import (
	"fmt"
	"log"
	"os"

	sandbox "github.com/VtrixAI/sandbox-go"
)

func main() {
	sb, err := sandbox.Create(sandbox.SandboxOpts{
		APIKey:  os.Getenv("SANDBOX_API_KEY"),
		BaseURL: os.Getenv("SANDBOX_BASE_URL"),
	})
	if err != nil {
		log.Fatalf("Create: %v", err)
	}
	defer sb.Kill() //nolint:errcheck

	// --- Write text / bytes ---
	if _, err := sb.Files.WriteText("/tmp/notes.txt", "line1\nline2\nline3"); err != nil {
		log.Fatalf("WriteText: %v", err)
	}
	if _, err := sb.Files.Write("/tmp/data.bin", []byte{0x00, 0x01, 0x02}); err != nil {
		log.Fatalf("Write bytes: %v", err)
	}

	// --- Read ---
	text, err := sb.Files.ReadText("/tmp/notes.txt")
	if err != nil {
		log.Fatalf("ReadText: %v", err)
	}
	fmt.Printf("notes.txt:\n%s\n", text)

	// Read — raw bytes
	raw, err := sb.Files.Read("/tmp/notes.txt")
	if err != nil {
		log.Fatalf("Read bytes: %v", err)
	}
	fmt.Printf("notes.txt bytes length: %d\n", len(raw))

	// --- Edit (find-and-replace) ---
	if err := sb.Files.Edit("/tmp/notes.txt", "line2", "LINE_TWO"); err != nil {
		log.Fatalf("Edit: %v", err)
	}
	updated, _ := sb.Files.ReadText("/tmp/notes.txt")
	fmt.Printf("after edit:\n%s\n", updated)

	// --- Batch write ---
	_, err = sb.Files.WriteFiles([]sandbox.WriteEntry{
		{Path: "/tmp/a.txt", Content: []byte("file A")},
		{Path: "/tmp/b.txt", Content: []byte("file B")},
	})
	if err != nil {
		log.Fatalf("WriteFiles: %v", err)
	}

	// --- Directory operations ---
	if _, err := sb.Files.MakeDir("/tmp/mydir"); err != nil {
		log.Fatalf("MakeDir: %v", err)
	}

	entries, err := sb.Files.List("/tmp")
	if err != nil {
		log.Fatalf("List: %v", err)
	}
	fmt.Printf("/tmp entries (%d):\n", len(entries))
	for _, e := range entries {
		fmt.Printf("  %s (%s, %d bytes)\n", e.Name, e.Type, e.Size)
	}

	// --- Exists / GetInfo / Rename / Remove ---
	exists, _ := sb.Files.Exists("/tmp/notes.txt")
	fmt.Printf("notes.txt exists: %v\n", exists)

	info, _ := sb.Files.GetInfo("/tmp/notes.txt")
	fmt.Printf("notes.txt size: %d\n", info.Size)

	if _, err := sb.Files.Rename("/tmp/notes.txt", "/tmp/notes_renamed.txt"); err != nil {
		log.Fatalf("Rename: %v", err)
	}

	if err := sb.Files.Remove("/tmp/notes_renamed.txt"); err != nil {
		log.Fatalf("Remove: %v", err)
	}

	fmt.Println("done.")
}
