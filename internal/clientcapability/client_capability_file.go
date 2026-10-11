package clientcapability

import (
	"crypto/rand"
	"encoding/base64"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

// capabilityFilename is the private file in the data directory that holds the
// client capability. Only the user who owns the data directory can read it.
const capabilityFilename = "client-capability"

const (
	capabilityBytes         = 32
	encodedCapabilityLength = 43
)

// Read returns the existing client capability from a private data directory.
func Read(dataDirectory string) (string, error) {
	if err := validatePrivateDataDirectory(dataDirectory); err != nil {
		return "", err
	}
	capabilityPath := filepath.Join(dataDirectory, capabilityFilename)
	info, err := os.Lstat(capabilityPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("client capability file is missing: %w", os.ErrNotExist)
		}
		return "", fmt.Errorf("inspect client capability file: %w", err)
	}
	if !info.Mode().IsRegular() {
		return "", errors.New("client capability must be a regular file")
	}
	if info.Mode().Perm() != 0o600 {
		return "", errors.New("client capability file must have mode 0600")
	}
	if !ownedByCurrentUser(info) {
		return "", errors.New("client capability file must belong to the current user")
	}
	encoded, err := os.ReadFile(capabilityPath)
	if err != nil {
		return "", fmt.Errorf("read client capability file: %w", err)
	}
	return validateEncodedCapability(encoded)
}

// Create returns the existing client capability or publishes a new random one.
func Create(dataDirectory string) (string, error) {
	if err := validatePrivateDataDirectory(dataDirectory); err != nil {
		return "", err
	}
	if existing, err := Read(dataDirectory); err == nil {
		return existing, nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return "", err
	}

	capability, err := generateCapability()
	if err != nil {
		return "", err
	}
	temporary, err := os.CreateTemp(dataDirectory, ".client-capability-*")
	if err != nil {
		return "", fmt.Errorf("create temporary client capability: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return "", fmt.Errorf("protect temporary client capability: %w", err)
	}
	if _, err := temporary.WriteString(capability); err != nil {
		temporary.Close()
		return "", fmt.Errorf("write temporary client capability: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return "", fmt.Errorf("sync temporary client capability: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return "", fmt.Errorf("close temporary client capability: %w", err)
	}

	capabilityPath := filepath.Join(dataDirectory, capabilityFilename)
	if err := os.Link(temporaryPath, capabilityPath); err != nil {
		if os.IsExist(err) {
			return Read(dataDirectory)
		}
		return "", fmt.Errorf("publish client capability: %w", err)
	}
	if err := syncDirectory(dataDirectory); err != nil {
		return "", err
	}
	return capability, nil
}

func generateCapability() (string, error) {
	raw := make([]byte, capabilityBytes)
	if _, err := rand.Read(raw); err != nil {
		return "", fmt.Errorf("generate client capability: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func validateEncodedCapability(encoded []byte) (string, error) {
	if len(encoded) != encodedCapabilityLength {
		return "", errors.New("client capability has an invalid encoding")
	}
	decoded, err := base64.RawURLEncoding.DecodeString(string(encoded))
	if err != nil || len(decoded) != capabilityBytes ||
		base64.RawURLEncoding.EncodeToString(decoded) != string(encoded) {
		return "", errors.New("client capability has an invalid encoding")
	}
	return string(encoded), nil
}

func validatePrivateDataDirectory(dataDirectory string) error {
	info, err := os.Lstat(dataDirectory)
	if err != nil {
		return fmt.Errorf("inspect client capability data directory: %w", err)
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("client capability data directory must be a directory")
	}
	if info.Mode().Perm() != 0o700 {
		return errors.New("client capability data directory must have mode 0700")
	}
	if !ownedByCurrentUser(info) {
		return errors.New("client capability data directory must belong to the current user")
	}
	return nil
}

func ownedByCurrentUser(info os.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)
	return ok && stat.Uid == uint32(os.Geteuid())
}

func syncDirectory(path string) error {
	directory, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open client capability data directory: %w", err)
	}
	defer directory.Close()
	if err := directory.Sync(); err != nil {
		return fmt.Errorf("sync client capability data directory: %w", err)
	}
	return nil
}
