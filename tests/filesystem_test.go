package tests

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	sandbox "github.com/VtrixAI/sandbox-go"
)

// ---------------------------------------------------------------------------
// Filesystem — basic read/write
// ---------------------------------------------------------------------------

func TestFilesystem_WriteAndReadText(t *testing.T) {
	const (
		path    = "/tmp/e2e_write_read.txt"
		content = "hello from go sdk e2e test"
	)

	_, err := sb.Files.WriteText(path, content)
	noErr(t, err, "WriteText")

	got, err := sb.Files.ReadText(path)
	noErr(t, err, "ReadText")

	if got != content {
		t.Fatalf("ReadText: got %q, want %q", got, content)
	}
}

func TestFilesystem_WriteBytesAndRead(t *testing.T) {
	data := []byte{0x00, 0x01, 0x7F, 0xFF, 0xFE}
	path := "/tmp/e2e_bytes.bin"

	_, err := sb.Files.Write(path, data)
	noErr(t, err, "Write bytes")

	got, err := sb.Files.Read(path)
	noErr(t, err, "Read bytes")

	if len(got) != len(data) {
		t.Fatalf("Read: got %d bytes, want %d", len(got), len(data))
	}
	for i := range data {
		if got[i] != data[i] {
			t.Fatalf("Read[%d]: got 0x%02X, want 0x%02X", i, got[i], data[i])
		}
	}
}

func TestFilesystem_WriteFiles_Batch(t *testing.T) {
	files := []sandbox.WriteEntry{
		{Path: "/tmp/e2e_batch_a.txt", Content: []byte("file A")},
		{Path: "/tmp/e2e_batch_b.txt", Content: []byte("file B")},
	}

	_, err := sb.Files.WriteFiles(files)
	noErr(t, err, "WriteFiles batch")

	for _, f := range files {
		got, err := sb.Files.ReadText(f.Path)
		noErr(t, err, "ReadText after batch write: "+f.Path)
		if got != string(f.Content) {
			t.Errorf("batch %s: got %q, want %q", f.Path, got, string(f.Content))
		}
	}
}

func TestFilesystem_WriteText_Overwrite(t *testing.T) {
	path := "/tmp/e2e_overwrite.txt"

	_, err := sb.Files.WriteText(path, "original content")
	noErr(t, err, "WriteText original")

	_, err = sb.Files.WriteText(path, "overwritten content")
	noErr(t, err, "WriteText overwrite")

	got, err := sb.Files.ReadText(path)
	noErr(t, err, "ReadText after overwrite")
	if got != "overwritten content" {
		t.Errorf("after overwrite: got %q, want %q", got, "overwritten content")
	}
}

// ---------------------------------------------------------------------------
// Filesystem — directory operations
// ---------------------------------------------------------------------------

func TestFilesystem_MakeDir(t *testing.T) {
	path := "/tmp/e2e_testdir"

	_, err := sb.Files.MakeDir(path)
	noErr(t, err, "MakeDir")

	exists, err := sb.Files.Exists(path)
	noErr(t, err, "Exists after MakeDir")
	if !exists {
		t.Fatal("directory should exist after MakeDir")
	}
}

func TestFilesystem_MakeDir_Idempotent(t *testing.T) {
	path := "/tmp/e2e_idempotent_dir"

	_, err := sb.Files.MakeDir(path)
	noErr(t, err, "MakeDir first call")

	// Second call on existing directory must not error.
	// The API response does not carry a "created" flag, so we only verify no error.
	_, err = sb.Files.MakeDir(path)
	noErr(t, err, "MakeDir second call (idempotent — must not error)")
}

func TestFilesystem_List(t *testing.T) {
	_, err := sb.Files.WriteText("/tmp/e2e_list_sentinel.txt", "list sentinel")
	noErr(t, err, "WriteText before List")

	entries, err := sb.Files.List("/tmp")
	noErr(t, err, "List /tmp")

	if len(entries) == 0 {
		t.Fatal("List /tmp returned 0 entries")
	}
	t.Logf("List /tmp: %d entries", len(entries))
}

func TestFilesystem_List_EntryFields(t *testing.T) {
	path := "/tmp/e2e_list_fields.txt"
	_, err := sb.Files.WriteText(path, "list entry fields")
	noErr(t, err, "WriteText")

	entries, err := sb.Files.List("/tmp")
	noErr(t, err, "List")

	var found *sandbox.EntryInfo
	for i := range entries {
		if entries[i].Name == "e2e_list_fields.txt" {
			found = &entries[i]
			break
		}
	}
	if found == nil {
		t.Fatal("written file not found in List result")
	}
	if found.Path == "" {
		t.Error("EntryInfo.Path should not be empty")
	}
	if found.Type == "" {
		t.Error("EntryInfo.Type should not be empty")
	}
	if found.Size <= 0 {
		t.Errorf("EntryInfo.Size: got %d, want > 0", found.Size)
	}
}

// ---------------------------------------------------------------------------
// Filesystem — exists / stat
// ---------------------------------------------------------------------------

func TestFilesystem_Exists_True(t *testing.T) {
	path := "/tmp/e2e_exists_true.txt"
	_, err := sb.Files.WriteText(path, "exists check")
	noErr(t, err, "WriteText")

	ok, err := sb.Files.Exists(path)
	noErr(t, err, "Exists")
	if !ok {
		t.Fatal("Exists should return true for written file")
	}
}

func TestFilesystem_Exists_False(t *testing.T) {
	ok, err := sb.Files.Exists("/tmp/definitely_no_such_file_xyz_9999")
	noErr(t, err, "Exists non-existent")
	if ok {
		t.Fatal("Exists should return false for missing path")
	}
}

func TestFilesystem_GetInfo(t *testing.T) {
	path := "/tmp/e2e_getinfo.txt"
	_, err := sb.Files.WriteText(path, "getinfo content")
	noErr(t, err, "WriteText before GetInfo")

	info, err := sb.Files.GetInfo(path)
	noErr(t, err, "GetInfo")

	if info.Name == "" {
		t.Fatal("GetInfo returned empty Name")
	}
	t.Logf("GetInfo: name=%s type=%s size=%d", info.Name, info.Type, info.Size)
}

func TestFilesystem_GetInfo_Fields(t *testing.T) {
	path := "/tmp/e2e_getinfo_fields.txt"
	content := "field check content"
	_, err := sb.Files.WriteText(path, content)
	noErr(t, err, "WriteText")

	info, err := sb.Files.GetInfo(path)
	noErr(t, err, "GetInfo")

	if info.Name != "e2e_getinfo_fields.txt" {
		t.Errorf("Name: got %q, want %q", info.Name, "e2e_getinfo_fields.txt")
	}
	if info.Path != path && !strings.HasSuffix(info.Path, strings.TrimPrefix(path, "/tmp")) {
		t.Errorf("Path: got %q, want suffix of %q", info.Path, path)
	}
	if info.Size != int64(len(content)) {
		t.Errorf("Size: got %d, want %d", info.Size, len(content))
	}
	if info.Type == "" {
		t.Error("Type should not be empty")
	}
	if info.ModifiedTime.IsZero() {
		t.Error("ModifiedTime should not be zero")
	}
}

// ---------------------------------------------------------------------------
// Filesystem — rename / remove
// ---------------------------------------------------------------------------

func TestFilesystem_Rename(t *testing.T) {
	src := "/tmp/e2e_rename_src.txt"
	dst := "/tmp/e2e_rename_dst.txt"

	_, err := sb.Files.WriteText(src, "rename me")
	noErr(t, err, "WriteText src")
	_ = sb.Files.Remove(dst) // clean up from previous run

	_, err = sb.Files.Rename(src, dst)
	noErr(t, err, "Rename")

	exists, err := sb.Files.Exists(dst)
	noErr(t, err, "Exists dst")
	if !exists {
		t.Fatal("destination should exist after Rename")
	}

	exists, err = sb.Files.Exists(src)
	noErr(t, err, "Exists src")
	if exists {
		t.Fatal("source should not exist after Rename")
	}
}

func TestFilesystem_Remove(t *testing.T) {
	path := "/tmp/e2e_remove.txt"
	_, err := sb.Files.WriteText(path, "delete me")
	noErr(t, err, "WriteText before Remove")

	err = sb.Files.Remove(path)
	noErr(t, err, "Remove")

	exists, err := sb.Files.Exists(path)
	noErr(t, err, "Exists after Remove")
	if exists {
		t.Fatal("file should not exist after Remove")
	}
}

func TestFilesystem_Remove_NonExistent(t *testing.T) {
	err := sb.Files.Remove("/tmp/e2e_remove_nonexistent_xyz_99999.txt")
	if err == nil {
		t.Fatal("Remove of non-existent path should return error")
	}
	t.Logf("Remove non-existent error (expected): %v", err)
}

func TestFilesystem_Remove_Directory(t *testing.T) {
	path := "/tmp/e2e_removedir"
	_, err := sb.Files.MakeDir(path)
	noErr(t, err, "MakeDir before Remove")

	err = sb.Files.Remove(path)
	noErr(t, err, "Remove directory")

	exists, err := sb.Files.Exists(path)
	noErr(t, err, "Exists after Remove directory")
	if exists {
		t.Fatal("directory should not exist after Remove")
	}
}

// ---------------------------------------------------------------------------
// Filesystem — error cases
// ---------------------------------------------------------------------------

func TestFilesystem_Read_NotFound(t *testing.T) {
	_, err := sb.Files.Read("/tmp/e2e_definitely_not_exist_xyz_99999.txt")
	if err == nil {
		t.Fatal("Read of non-existent file should return error")
	}
	t.Logf("Read non-existent error (expected): %v", err)
}

// ---------------------------------------------------------------------------
// Filesystem — edit
// ---------------------------------------------------------------------------

func TestFilesystem_Edit_Basic(t *testing.T) {
	path := "/tmp/e2e_edit_basic.txt"
	_, err := sb.Files.WriteText(path, "hello world")
	noErr(t, err, "WriteText before Edit")

	err = sb.Files.Edit(path, "world", "e2e")
	noErr(t, err, "Edit")

	got, err := sb.Files.ReadText(path)
	noErr(t, err, "ReadText after Edit")
	if got != "hello e2e" {
		t.Fatalf("after Edit: got %q, want %q", got, "hello e2e")
	}
}

func TestFilesystem_Edit_NotFound(t *testing.T) {
	path := "/tmp/e2e_edit_notfound.txt"
	_, err := sb.Files.WriteText(path, "some content")
	noErr(t, err, "WriteText")

	// oldText does not exist in file — server returns 422.
	err = sb.Files.Edit(path, "no_such_text_xyz", "replacement")
	if err == nil {
		t.Fatal("Edit with non-existent oldText should return error")
	}
	t.Logf("Edit not-found error (expected): %v", err)
}

func TestFilesystem_Edit_NotUnique(t *testing.T) {
	path := "/tmp/e2e_edit_notunique.txt"
	_, err := sb.Files.WriteText(path, "repeat repeat repeat")
	noErr(t, err, "WriteText")

	// "repeat" appears 3 times — server should reject (422).
	err = sb.Files.Edit(path, "repeat", "once")
	if err == nil {
		t.Fatal("Edit with non-unique oldText should return error")
	}
	t.Logf("Edit not-unique error (expected): %v", err)
}

func TestFilesystem_ReadStream(t *testing.T) {
	const path = "/tmp/e2e_readstream.txt"
	const content = "streaming content for readstream test"

	_, err := sb.Files.WriteText(path, content)
	noErr(t, err, "WriteText before ReadStream")

	rc, err := sb.Files.ReadStream(path)
	noErr(t, err, "ReadStream")
	defer rc.Close()

	got, err := io.ReadAll(rc)
	noErr(t, err, "ReadAll from ReadStream")

	if string(got) != content {
		t.Fatalf("ReadStream: got %q, want %q", string(got), content)
	}
}

func TestFilesystem_ReadStream_LargeFile(t *testing.T) {
	const path = "/tmp/e2e_readstream_large.bin"

	// Write 1 MiB of data.
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i % 251)
	}
	_, err := sb.Files.Write(path, data)
	noErr(t, err, "Write large file")

	rc, err := sb.Files.ReadStream(path)
	noErr(t, err, "ReadStream large file")
	defer rc.Close()

	got, err := io.ReadAll(rc)
	noErr(t, err, "ReadAll large")

	if len(got) != len(data) {
		t.Fatalf("ReadStream large: got %d bytes, want %d", len(got), len(data))
	}
}

func TestFilesystem_ReadStream_NotFound(t *testing.T) {
	rc, err := sb.Files.ReadStream("/tmp/e2e_readstream_notexist_xyz_99999.txt")
	if err == nil {
		rc.Close()
		t.Fatal("ReadStream of non-existent file should return error")
	}
	t.Logf("ReadStream non-existent error (expected): %v", err)
}


func TestFilesystem_WatchDir(t *testing.T) {
	watchPath := "/tmp/e2e_watch"
	_, err := sb.Files.MakeDir(watchPath)
	noErr(t, err, "MakeDir for WatchDir")

	events := make(chan sandbox.FilesystemEvent, 10)
	handle, err := sb.Files.WatchDir(watchPath, func(e sandbox.FilesystemEvent) {
		events <- e
	})
	noErr(t, err, "WatchDir")
	defer handle.Stop()

	// Give the watcher a moment to register.
	time.Sleep(200 * time.Millisecond)

	_, err = sb.Files.WriteText(watchPath+"/watched.txt", "trigger")
	noErr(t, err, "WriteText to trigger event")

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	select {
	case ev := <-events:
		t.Logf("WatchDir event: name=%s type=%s", ev.Name, ev.Type)
	case <-ctx.Done():
		t.Fatal("WatchDir: timed out waiting for filesystem event")
	}
}

func TestFilesystem_WatchDir_Stop(t *testing.T) {
	watchPath := "/tmp/e2e_watch_stop"
	_, err := sb.Files.MakeDir(watchPath)
	noErr(t, err, "MakeDir for WatchDir Stop test")

	fired := make(chan struct{}, 5)
	handle, err := sb.Files.WatchDir(watchPath, func(e sandbox.FilesystemEvent) {
		fired <- struct{}{}
	})
	noErr(t, err, "WatchDir")

	// Trigger an event to confirm watcher is live.
	time.Sleep(200 * time.Millisecond)
	_, err = sb.Files.WriteText(watchPath+"/before_stop.txt", "before")
	noErr(t, err, "WriteText before Stop")

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	select {
	case <-fired:
		// good — watcher was live
	case <-ctx.Done():
		t.Fatal("WatchDir: timed out waiting for pre-stop event")
	}

	// Now stop and drain the channel.
	handle.Stop()
	for len(fired) > 0 {
		<-fired
	}

	// Write after stop — callback must NOT fire.
	_, err = sb.Files.WriteText(watchPath+"/after_stop.txt", "after")
	noErr(t, err, "WriteText after Stop")

	time.Sleep(500 * time.Millisecond)
	if len(fired) > 0 {
		t.Error("WatchDir callback fired after Stop()")
	}
}
