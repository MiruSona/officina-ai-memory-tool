package store

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// pathCache 는 한 저장소에서 되풀이되는 경로 검사 결과다. 색인은 파일 2만 개를
// 여는데 뿌리와 가운데 폴더(store/YYYY/MM)는 스무 남짓뿐이라 한 번만 본다.
type pathCache struct {
	mutex sync.Mutex
	root  string
	dirs  map[string]error
}

// realRoot 는 링크를 푼 저장소 뿌리다. 한 번 풀고 계속 쓴다.
func (s *Store) realRoot() string {
	root, err := filepath.Abs(s.Dir)
	if err != nil {
		root = s.Dir
	}
	s.paths.mutex.Lock()
	defer s.paths.mutex.Unlock()
	return s.paths.realRootLocked(root)
}

func (c *pathCache) realRootLocked(root string) string {
	if c.root == "" {
		c.root = realPath(root)
	}
	return c.root
}

// checkDir 은 가운데 폴더가 뿌리 안의 보통 폴더인지 본다. 같은 폴더는 두 번
// 보지 않는다 — 색인 한 판 동안 락을 잡고 있어 폴더가 바뀌지 않는다.
func (s *Store) checkDir(root, dir string) error {
	s.paths.mutex.Lock()
	defer s.paths.mutex.Unlock()
	if s.paths.dirs == nil {
		s.paths.dirs = map[string]error{}
	}
	if err, seen := s.paths.dirs[dir]; seen {
		return err
	}
	err := checkPlainDir(root, s.paths.realRootLocked(root), dir)
	s.paths.dirs[dir] = err
	return err
}

// checkPlainDir 은 폴더 하나가 뿌리 밖을 가리키지 않는지 두 가지로 본다.
// 링크를 풀어 본 자리와, 사이 폴더가 전부 보통 폴더인지다.
func checkPlainDir(root, realRoot, dir string) error {
	if !inside(realRoot, realPath(dir)) {
		return errors.New(i18n.T(i18n.OutsidePath, dir))
	}
	return plainDirs(root, dir)
}

// plainDirs 는 root 와 dir 사이의 폴더가 전부 보통 폴더인지 본다.
// EvalSymlinks 가 윈도우 정션(mklink /J)은 안 풀어 주기 때문에 따로 본다.
func plainDirs(root, dir string) error {
	rest, err := filepath.Rel(root, dir)
	if err != nil {
		return errors.New(i18n.T(i18n.OutsidePath, dir))
	}
	current := root
	for _, name := range strings.Split(filepath.ToSlash(rest), "/") {
		if name == "." || name == "" {
			continue
		}
		current = filepath.Join(current, name)
		info, err := os.Lstat(current)
		if os.IsNotExist(err) {
			return nil
		}
		if err != nil {
			return err
		}
		if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			return errors.New(i18n.T(i18n.NotRegularFile, current))
		}
	}
	return nil
}

// leafInfo 는 마지막 이름을 한 번만 보고 보통 파일인지까지 답한다. 색인이
// 파일 2만 개를 여는 자리라 Lstat 을 두 번 하면 그대로 시간이 된다.
func leafInfo(target string) error {
	info, err := os.Lstat(target)
	if err != nil {
		return err
	}
	// 부모 폴더가 뿌리 안의 보통 폴더인 것은 checkDir 이 이미 봤다. 그러니
	// 마지막 이름이 링크가 아니면 이 파일은 뿌리 안에 있다.
	if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) != 0 || !info.Mode().IsRegular() {
		return errors.New(i18n.T(i18n.NotRegularFile, target))
	}
	return nil
}

// plainLeaf 는 마지막 이름만 본다. 부모가 뿌리 안의 보통 폴더인 것은 이미
// 봤으니, 여기서 밖으로 나갈 길은 마지막 이름이 링크인 것뿐이다.
func plainLeaf(realRoot, target string) error {
	info, err := os.Lstat(target)
	if err != nil {
		return nil
	}
	if info.Mode()&(fs.ModeSymlink|fs.ModeIrregular) == 0 {
		return nil
	}
	if !inside(realRoot, realPath(target)) {
		return errors.New(i18n.T(i18n.OutsidePath, target))
	}
	return nil
}
