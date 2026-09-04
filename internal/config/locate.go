package config

import (
	"os"
	"path/filepath"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// DirName is the folder that holds one repository inside a project.
const DirName = "Memory"

// NoRepositoryError means no repository was found.
type NoRepositoryError struct{}

func (e *NoRepositoryError) Error() string {
	return i18n.T(i18n.NoRepository)
}

// ExitCode is the code the CLI returns for a missing repository.
func (e *NoRepositoryError) ExitCode() int {
	return 3
}

// Repository is one opened memory store.
type Repository struct {
	Dir    string
	Config Config
}

// Find walks up from start looking for Memory/mem.toml. It stops after the
// folder that holds .git so a submodule never picks up the outer repository.
func Find(start string) (string, bool) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", false
	}
	for {
		candidate := filepath.Join(dir, DirName, FileName)
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Join(dir, DirName), true
		}
		if isGitRoot(dir) {
			return "", false
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return "", false
		}
		dir = parent
	}
}

// isGitRoot also accepts a .git file, which is what a submodule has.
func isGitRoot(dir string) bool {
	_, err := os.Stat(filepath.Join(dir, ".git"))
	return err == nil
}

// Open reads the mem.toml of an already known repository folder.
func Open(dir string) (*Repository, error) {
	config, err := Load(filepath.Join(dir, FileName))
	if err != nil {
		return nil, err
	}
	return &Repository{Dir: dir, Config: config}, nil
}

// Resolve returns the project repository. 전역 저장소는 v0.2 에서 없앴다 (설계 6-3).
func Resolve(repoFlag, start string) (*Repository, error) {
	project, err := resolveProject(repoFlag, start)
	if err != nil {
		return nil, err
	}
	if project == nil {
		return nil, &NoRepositoryError{}
	}
	return project, nil
}

func resolveProject(repoFlag, start string) (*Repository, error) {
	if repoFlag == "" {
		dir, found := Find(start)
		if !found {
			return nil, nil
		}
		return Open(dir)
	}
	if _, err := os.Stat(filepath.Join(repoFlag, FileName)); err == nil {
		return Open(repoFlag)
	}
	inner := filepath.Join(repoFlag, DirName)
	if _, err := os.Stat(filepath.Join(inner, FileName)); err == nil {
		return Open(inner)
	}
	return nil, &NoRepositoryError{}
}
