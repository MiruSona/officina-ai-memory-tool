package store

import (
	"errors"
	"os"
	"syscall"
	"time"
)

// Windows returns these when an antivirus or another process holds the file.
// The numbers are used directly so golang.org/x/sys stays out of the module.
// ERROR_ACCESS_DENIED shows up the same way when a scanner has the file open,
// or when the target of a rename is still open somewhere else.
const (
	errAccessDenied     = syscall.Errno(5)
	errSharingViolation = syscall.Errno(32)
	errLockViolation    = syscall.Errno(33)
)

const (
	retryAttempts = 10
	retryDelay    = 50 * time.Millisecond
)

// withRetry repeats an action while Windows says the file is momentarily held.
func withRetry(action func() error) error {
	err := action()
	for attempt := 1; attempt < retryAttempts && isHeldByOther(err); attempt++ {
		time.Sleep(retryDelay)
		err = action()
	}
	return err
}

func isHeldByOther(err error) bool {
	if err == nil {
		return false
	}
	errno := syscall.Errno(0)
	if !errors.As(err, &errno) {
		return false
	}
	return errno == errAccessDenied || errno == errSharingViolation || errno == errLockViolation
}

func readFileRetry(path string) ([]byte, error) {
	data := []byte(nil)
	err := withRetry(func() error {
		read, err := os.ReadFile(path)
		data = read
		return err
	})
	return data, err
}

func renameRetry(from, to string) error {
	return withRetry(func() error {
		return os.Rename(from, to)
	})
}
