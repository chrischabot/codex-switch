package accounts

import (
	"os"
	"path/filepath"
	"syscall"
)

// The login, accounts and remove commands hold pool.lock while they change the
// account pool, so two commands never change it at the same time. Accounts
// appear and disappear by directory rename, so readers without the lock, such
// as the proxy, see whole accounts only. auth.lock serialises every write to
// one account's auth.json.

func withPoolLock(dataDirectory string, change func() error) error {
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
		return err
	}
	return withFileLock(filepath.Join(dataDirectory, "pool.lock"), change)
}

// withAuthLock never recreates an account removed during a credential refresh.
func withAuthLock(accountDirectory string, change func() error) error {
	return withFileLock(filepath.Join(accountDirectory, "auth.lock"), change)
}

func withFileLock(path string, change func() error) error {
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		return err
	}
	defer syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	return change()
}
