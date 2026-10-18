package logging

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
)

func TestLogWriterProcess(t *testing.T) {
	path := os.Getenv("CODEX_SWITCH_TEST_LOG")
	if path == "" {
		return
	}
	w, err := NewRotatingWriter(path, 128, 2)
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 100; i++ {
		if _, err = w.Write([]byte(fmt.Sprintf("synthetic entry %03d\n", i))); err != nil {
			t.Fatal(err)
		}
	}
}

func TestLogRetentionAcrossProcessesAndGenerations(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy.log")
	old, err := NewRotatingWriter(path, 128, 2)
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cmd := exec.Command(os.Args[0], "-test.run=^TestLogWriterProcess$")
			cmd.Env = append(os.Environ(), "CODEX_SWITCH_TEST_LOG="+path)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Errorf("writer: %v %s", err, output)
			}
		}()
	}
	wg.Wait()
	// A generation started before rotation must reopen the current log.
	if _, err := old.Write([]byte("last entry\n")); err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(path + "*")
	if err != nil {
		t.Fatal(err)
	}
	total := int64(0)
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0600 {
			t.Fatalf("unsafe permissions: %s", file)
		}
		if file == path+".lock" {
			continue
		}
		if info.Size() > 128 {
			t.Fatalf("unbounded log %s: %d bytes", file, info.Size())
		}
		total += info.Size()
		data, _ := os.ReadFile(file)
		if len(data) > 0 && data[len(data)-1] != '\n' {
			t.Fatal("split log record")
		}
	}
	if total > 3*128 {
		t.Fatalf("unbounded archives: %d", total)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.HasSuffix(data, []byte("last entry\n")) {
		t.Fatal("old generation wrote to retired inode", err)
	}
	if n, err := old.Write(bytes.Repeat([]byte("x"), 256)); err != nil || n != 256 {
		t.Fatalf("oversize record: %d %v", n, err)
	}
	info, _ := os.Stat(path)
	if info.Size() > 128 {
		t.Fatal("oversize record bypassed retention")
	}
}

func TestLogUpgradeBoundsExistingLargeFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy.log")
	if err := os.WriteFile(path, bytes.Repeat([]byte("old entry\n"), 100), 0644); err != nil {
		t.Fatal(err)
	}
	writer, err := NewRotatingWriter(path, 128, 2)
	if err != nil {
		t.Fatal(err)
	}
	if _, err = writer.Write([]byte("new entry\n")); err != nil {
		t.Fatal(err)
	}
	files, _ := filepath.Glob(path + "*")
	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil || info.Size() > 128 || info.Mode().Perm() != 0600 {
			t.Fatalf("unbounded or public old file: %s, %v", file, err)
		}
	}
}
