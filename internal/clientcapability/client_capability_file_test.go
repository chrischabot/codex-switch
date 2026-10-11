package clientcapability

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestCreatePublishesStablePrivateCapability(t *testing.T) {
	dataDirectory := privateTestDirectory(t)
	created, err := Create(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if len(created) != encodedCapabilityLength || PathPrefix(created) != "/client/"+created {
		t.Fatalf("created capability contract is invalid")
	}
	reread, err := Read(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	again, err := Create(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if reread != created || again != created {
		t.Fatal("client capability changed after creation")
	}
	info, err := os.Lstat(filepath.Join(dataDirectory, capabilityFilename))
	if err != nil {
		t.Fatal(err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm() != 0o600 {
		t.Fatalf("client capability mode = %v", info.Mode())
	}
}

func TestReadRejectsMissingMalformedAndUnsafeCapabilityFiles(t *testing.T) {
	validCapability := strings.Repeat("A", encodedCapabilityLength)
	tests := []struct {
		name  string
		setup func(*testing.T, string)
	}{
		{name: "missing", setup: func(*testing.T, string) {}},
		{name: "short", setup: func(t *testing.T, directory string) {
			writeCapabilityFixture(t, directory, "short", 0o600)
		}},
		{name: "padded", setup: func(t *testing.T, directory string) {
			writeCapabilityFixture(t, directory, strings.Repeat("A", encodedCapabilityLength-1)+"=", 0o600)
		}},
		{name: "newline", setup: func(t *testing.T, directory string) {
			writeCapabilityFixture(t, directory, validCapability+"\n", 0o600)
		}},
		{name: "permissive", setup: func(t *testing.T, directory string) {
			writeCapabilityFixture(t, directory, validCapability, 0o644)
		}},
		{name: "directory", setup: func(t *testing.T, directory string) {
			if err := os.Mkdir(filepath.Join(directory, capabilityFilename), 0o600); err != nil {
				t.Fatal(err)
			}
		}},
		{name: "symlink", setup: func(t *testing.T, directory string) {
			target := filepath.Join(directory, "target")
			if err := os.WriteFile(target, []byte(validCapability), 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(target, filepath.Join(directory, capabilityFilename)); err != nil {
				t.Fatal(err)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dataDirectory := privateTestDirectory(t)
			test.setup(t, dataDirectory)
			if _, err := Read(dataDirectory); err == nil {
				t.Fatal("invalid client capability was accepted")
			}
		})
	}
}

func TestCreateRequiresExistingPrivateDataDirectory(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "missing")
	if _, err := Create(missing); err == nil {
		t.Fatal("missing data directory was accepted")
	}
	permissive := t.TempDir()
	if err := os.Chmod(permissive, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Create(permissive); err == nil {
		t.Fatal("permissive data directory was accepted")
	}
}

func TestConcurrentCreateReturnsOneCapability(t *testing.T) {
	dataDirectory := privateTestDirectory(t)
	const callers = 32
	values := make([]string, callers)
	errorsByCaller := make([]error, callers)
	ready := make(chan struct{})
	var wait sync.WaitGroup
	wait.Add(callers)
	for index := range callers {
		go func() {
			defer wait.Done()
			<-ready
			values[index], errorsByCaller[index] = Create(dataDirectory)
		}()
	}
	close(ready)
	wait.Wait()
	for index := range callers {
		if errorsByCaller[index] != nil {
			t.Fatalf("Create caller %d: %v", index, errorsByCaller[index])
		}
		if values[index] != values[0] {
			t.Fatal("concurrent creation returned different capabilities")
		}
	}
	stored, err := Read(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	if stored != values[0] {
		t.Fatal("stored capability differs from concurrent result")
	}
}

func TestErrorsDoNotContainCapabilityValue(t *testing.T) {
	dataDirectory := privateTestDirectory(t)
	secret := strings.Repeat("z", encodedCapabilityLength)
	writeCapabilityFixture(t, dataDirectory, secret, 0o644)
	_, err := Read(dataDirectory)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("error exposed client capability: %v", err)
	}
	if errors.Is(err, os.ErrNotExist) {
		t.Fatal("invalid capability was reported as missing")
	}
}

func privateTestDirectory(t *testing.T) string {
	t.Helper()
	directory := t.TempDir()
	if err := os.Chmod(directory, 0o700); err != nil {
		t.Fatal(err)
	}
	return directory
}

func writeCapabilityFixture(t *testing.T, directory, value string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(directory, capabilityFilename), []byte(value), mode); err != nil {
		t.Fatal(err)
	}
}
