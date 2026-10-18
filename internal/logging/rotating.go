// Package logging owns the bounded diagnostic log shared by proxy generations.
package logging

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

type RotatingWriter struct {
	path     string
	limit    int64
	retained int
}

func NewRotatingWriter(path string, limit int64, retained int) (*RotatingWriter, error) {
	if limit < 1 || retained < 0 {
		return nil, fmt.Errorf("invalid log retention")
	}
	w := &RotatingWriter{path, limit, retained}
	if _, err := w.Write(nil); err != nil {
		return nil, err
	}
	return w, nil
}

// Write locks rotation across processes and opens the current inode each time.
// No generation retains an archived file descriptor. Oversized diagnostic
// records are truncated; request and response bytes never pass through here.
func (w *RotatingWriter) Write(data []byte) (int, error) {
	lock, err := os.OpenFile(w.path+".lock", os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return 0, err
	}
	defer lock.Close()
	for {
		err = unix.Flock(int(lock.Fd()), unix.LOCK_EX)
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return 0, err
	}
	defer unix.Flock(int(lock.Fd()), unix.LOCK_UN)
	if err = lock.Chmod(0600); err != nil {
		return 0, err
	}
	originalLength := len(data)
	if int64(len(data)) > w.limit {
		data = append([]byte(nil), data[:w.limit]...)
		data[len(data)-1] = '\n'
	}
	info, err := os.Stat(w.path)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	if info != nil && info.Size()+int64(len(data)) > w.limit {
		if w.retained == 0 {
			if err = os.Remove(w.path); err != nil {
				return 0, err
			}
		} else {
			for index := w.retained; index >= 1; index-- {
				source := w.path
				if index > 1 {
					source = fmt.Sprintf("%s.%d", w.path, index-1)
				}
				destination := fmt.Sprintf("%s.%d", w.path, index)
				if err = os.Rename(source, destination); err != nil && !os.IsNotExist(err) {
					return 0, err
				}
				// Harden logs created by earlier releases as they rotate.
				if err = trimArchive(destination, w.limit); err != nil && !os.IsNotExist(err) {
					return 0, err
				}
			}
		}
	}
	file, err := os.OpenFile(w.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0600)
	if err != nil {
		return 0, err
	}
	if err = file.Chmod(0600); err != nil {
		file.Close()
		return 0, err
	}
	n, writeErr := file.Write(data)
	closeErr := file.Close()
	if writeErr != nil {
		return n, writeErr
	}
	if closeErr != nil {
		return n, closeErr
	}
	return originalLength, nil
}

// Upgrading from an unbounded logger also bounds its existing archive. Keep
// the most recent complete records, using at most one log segment of memory.
func trimArchive(path string, limit int64) error {
	file, err := os.OpenFile(path, os.O_RDWR, 0600)
	if err != nil {
		return err
	}
	defer file.Close()
	if err = file.Chmod(0600); err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		return err
	}
	if info.Size() <= limit {
		return nil
	}
	data, err := io.ReadAll(io.NewSectionReader(file, info.Size()-limit, limit))
	if err != nil {
		return err
	}
	if newline := bytes.IndexByte(data, '\n'); newline >= 0 {
		data = data[newline+1:]
	} else {
		data = nil
	}
	if _, err = file.WriteAt(data, 0); err != nil {
		return err
	}
	return file.Truncate(int64(len(data)))
}
