// Package store handles the file layer: Maildir writes, store reads, retry
// wrappers and archives. It never opens the index database.
package store

import (
	"errors"
	"os"
	"path/filepath"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// Folder names inside one repository.
const (
	dirStore   = "store"
	dirInbox   = "inbox"
	dirTmp     = "tmp"
	dirNew     = "new"
	dirBad     = "bad"
	dirArchive = "archive"
	dirGolden  = "golden"
)

// Store is one opened repository seen as a file tree.
type Store struct {
	Dir      string
	ReadOnly bool
	// paths 는 되풀이되는 경로 검사를 한 번만 하게 해 준다 (pathcache.go).
	paths pathCache
}

// New wraps an opened repository.
func New(repository *config.Repository) *Store {
	return &Store{Dir: repository.Dir}
}

// Open builds a store straight from a folder, for callers that have no config yet.
func Open(dir string, readOnly bool) *Store {
	return &Store{Dir: dir, ReadOnly: readOnly}
}

func (s *Store) StoreDir() string    { return filepath.Join(s.Dir, dirStore) }
func (s *Store) InboxTmpDir() string { return filepath.Join(s.Dir, dirInbox, dirTmp) }
func (s *Store) InboxNewDir() string { return filepath.Join(s.Dir, dirInbox, dirNew) }
func (s *Store) InboxBadDir() string { return filepath.Join(s.Dir, dirInbox, dirBad) }
func (s *Store) ArchiveDir() string  { return filepath.Join(s.Dir, dirArchive) }
func (s *Store) GoldenDir() string   { return filepath.Join(s.Dir, dirGolden) }

// Mode of the skeleton. 저장소는 git 에 올라가므로 보통 권한을 쓴다.
const sharedDirMode = 0o755

// EnsureDirs creates the skeleton. Writing commands call it before their first write.
func (s *Store) EnsureDirs() error {
	if err := s.writable(); err != nil {
		return err
	}
	mode := os.FileMode(sharedDirMode)
	for _, dir := range []string{s.StoreDir(), s.InboxTmpDir(), s.InboxNewDir(), s.InboxBadDir(), s.ArchiveDir(), s.GoldenDir()} {
		if err := os.MkdirAll(dir, mode); err != nil {
			return err
		}
	}
	return nil
}

// ReadOnlyError is returned when a command tries to write to a read-only store.
type ReadOnlyError struct{}

func (e *ReadOnlyError) Error() string {
	return i18n.T(i18n.ReadOnlyStore)
}

func (s *Store) writable() error {
	if s.ReadOnly {
		return &ReadOnlyError{}
	}
	return nil
}

// IsReadOnly says whether an error came from writing to a read-only store.
func IsReadOnly(err error) bool {
	target := &ReadOnlyError{}
	return errors.As(err, &target)
}
