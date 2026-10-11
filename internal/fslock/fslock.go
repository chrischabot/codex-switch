package fslock

import (
	"errors"
	"os"
	"path/filepath"
	"sync"
	"syscall"
)

var ErrHeld = errors.New("process lock is already held")

type Lock struct {
	mu   sync.Mutex
	file *os.File
}

// Acquire obtains a process-lifetime exclusive lock without waiting. It is
// used by the stable gateway so a second owner cannot remove or replace its
// Unix sockets.
func Acquire(path string) (*Lock, error) {
	return acquire(path, syscall.LOCK_EX|syscall.LOCK_NB)
}

func acquire(path string, operation int) (*Lock, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(file.Fd()), operation); err != nil {
		file.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, ErrHeld
		}
		return nil, err
	}
	return &Lock{file: file}, nil
}

func (l *Lock) Close() error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.file == nil {
		return nil
	}
	unlockErr := syscall.Flock(int(l.file.Fd()), syscall.LOCK_UN)
	closeErr := l.file.Close()
	l.file = nil
	return errors.Join(unlockErr, closeErr)
}

func With(path string, fn func() error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	return WithExistingDirectory(path, fn)
}

// WithExistingDirectory never recreates an account removed during a credential refresh.
func WithExistingDirectory(path string, fn func() error) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return fn()
}
