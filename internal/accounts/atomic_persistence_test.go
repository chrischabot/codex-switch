package accounts

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialPublicationSynchronizesDirectoryAfterRename(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "auth.json")
	os.WriteFile(path, []byte("old"), 0600)
	failure := errors.New("synthetic directory sync failure")
	called := false
	err := writeAtomicDataWithPublish(path, []byte("rotated"), os.Rename, func(parent string) error {
		called = true
		if parent != dir {
			t.Fatal("synced wrong directory")
		}
		saved, err := os.ReadFile(path)
		if err != nil || !bytes.Equal(saved, []byte("rotated")) {
			t.Fatal("directory synchronized before publication")
		}
		return failure
	})
	if !called || !errors.Is(err, failure) {
		t.Fatalf("publication lost directory sync failure: %v", err)
	}
	saved, _ := os.ReadFile(path)
	if string(saved) != "rotated" {
		t.Fatal("published rotating token was rolled back")
	}
}
