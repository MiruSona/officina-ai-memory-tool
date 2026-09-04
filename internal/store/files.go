package store

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// FileInfo is what the indexer needs to tell changed files from unchanged ones.
type FileInfo struct {
	Path    string
	Abs     string
	ModTime time.Time
	Size    int64
}

// MemoryFile is a parsed memory plus how its bytes were encoded on disk.
type MemoryFile struct {
	Path     string
	Encoding Encoding
	Memory   *model.Memory
}

var tempSequence atomic.Uint64

// ReservedNames 는 `store/` 안에서 기억 파일이 아닌 예약 이름이다. OKF 관례의
// 묶음 링크 목록이라 id 꼴과 안 맞는데, 그냥 두면 Validate 가 「id 와 이름이
// 안 맞다」로 거절해 실수와 구별이 안 된다 (Guide 기억파일규격 1절).
var ReservedNames = []string{"index.md"}

// IsReservedName 은 그 파일 이름이 예약 이름인지다. 대소문자는 안 가린다 —
// Windows 파일 이름이 대소문자를 안 가린다.
func IsReservedName(name string) bool {
	for _, reserved := range ReservedNames {
		if strings.EqualFold(name, reserved) {
			return true
		}
	}
	return false
}

// ListMemories walks store/**/*.md and returns paths with forward slashes.
func (s *Store) ListMemories() ([]FileInfo, error) {
	files := []FileInfo{}
	err := filepath.WalkDir(s.StoreDir(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || IsReservedName(entry.Name()) {
			return nil
		}
		// A symlink could point anywhere, so it is not part of the store.
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		relative, err := filepath.Rel(s.Dir, path)
		if err != nil {
			return err
		}
		files = append(files, FileInfo{Path: filepath.ToSlash(relative), Abs: path, ModTime: info.ModTime(), Size: info.Size()})
		return nil
	})
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(a, b int) bool { return files[a].Path < files[b].Path })
	return files, nil
}

// errFoundOne 은 첫 파일에서 걷기를 멈추는 표시다. 오류가 아니다.
var errFoundOne = errors.New("found one")

// HasMemory 는 store/ 에 md 가 한 장이라도 있는지다. 첫 장에서 멈추므로
// 2만 건 저장소에서도 싸다. 「색인이 0건인데 파일은 있다」 를 가르는 데 쓴다.
func (s *Store) HasMemory() bool {
	err := filepath.WalkDir(s.StoreDir(), func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".md") || IsReservedName(entry.Name()) {
			return nil
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			return nil
		}
		return errFoundOne
	})
	return errors.Is(err, errFoundOne)
}

// ReadMemory reads and parses one memory. A cp949 file is rewritten as UTF-8
// right away, so the next read is clean and lint only sees it once.
func (s *Store) ReadMemory(path string) (*MemoryFile, error) {
	abs, err := s.absFile(path)
	if err != nil {
		return nil, err
	}
	return s.readAt(abs, path)
}

// ReadListed 는 ListMemories 가 훑어 놓은 파일을 읽는다. 그 걸음에서 링크가
// 아닌 것과 있는 것을 이미 봤으니 마지막 이름을 다시 안 본다 — 파일 2만 개면
// Lstat 2만 번이 그대로 시간이다.
func (s *Store) ReadListed(file FileInfo) (*MemoryFile, error) {
	abs, err := s.absPath(file.Path)
	if err != nil {
		return nil, err
	}
	return s.readAt(abs, file.Path)
}

func (s *Store) readAt(abs, path string) (*MemoryFile, error) {
	raw, err := readFileRetry(abs)
	if err != nil {
		return nil, err
	}
	text, encoding, err := decode(raw, path)
	if err != nil {
		return nil, err
	}
	memory, err := model.Parse(text)
	if err != nil {
		return nil, err
	}
	if encoding == EncodingCP949 && !s.ReadOnly {
		if err := s.writeAtomic(abs, text); err != nil {
			return nil, err
		}
	}
	return &MemoryFile{Path: filepath.ToSlash(path), Encoding: encoding, Memory: memory}, nil
}

// WriteMemory writes store/YYYY/MM/<id>.md. Only the lock holder calls this
// (invariant 2), and it always goes through tmp plus rename.
func (s *Store) WriteMemory(memory *model.Memory) error {
	relative := model.StorePath(memory.ID)
	if relative == "" {
		return errors.New(i18n.T(i18n.BadID, memory.ID))
	}
	abs, err := s.Abs(relative)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return err
	}
	return s.writeAtomic(abs, model.Encode(memory))
}

// writeAtomic writes into inbox/tmp first so a reader never sees a partial file.
func (s *Store) writeAtomic(abs string, data []byte) error {
	if err := s.writable(); err != nil {
		return err
	}
	if err := regularFile(abs); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(s.InboxTmpDir(), 0o755); err != nil {
		return err
	}
	name := fmt.Sprintf("%d.%d.%d.tmp", time.Now().UnixNano(), os.Getpid(), tempSequence.Add(1))
	tmp := filepath.Join(s.InboxTmpDir(), name)
	if err := writeFileRetry(tmp, data); err != nil {
		return err
	}
	if err := renameRetry(tmp, abs); err != nil {
		os.Remove(tmp)
		return err
	}
	return nil
}

// Abs turns a repository relative path into an absolute one and refuses
// anything that would land outside the store (invariant 4).
func (s *Store) Abs(rel string) (string, error) {
	target, err := s.absPath(rel)
	if err != nil {
		return "", err
	}
	if err := plainLeaf(s.realRoot(), target); err != nil {
		return "", err
	}
	return target, nil
}

// absFile 은 Abs 와 같되 마지막 이름이 보통 파일인 것까지 한 번에 본다.
func (s *Store) absFile(rel string) (string, error) {
	target, err := s.absPath(rel)
	if err != nil {
		return "", err
	}
	if err := leafInfo(target); err != nil {
		return "", err
	}
	return target, nil
}

// absPath 는 Abs 에서 마지막 이름 검사만 뺀 것이다.
func (s *Store) absPath(rel string) (string, error) {
	if rel == "" || filepath.IsAbs(rel) || strings.ContainsRune(rel, ':') {
		return "", errors.New(i18n.T(i18n.OutsidePath, rel))
	}
	root, err := filepath.Abs(s.Dir)
	if err != nil {
		return "", err
	}
	target, err := filepath.Abs(filepath.Join(root, filepath.FromSlash(rel)))
	if err != nil {
		return "", err
	}
	if !inside(root, target) {
		return "", errors.New(i18n.T(i18n.OutsidePath, rel))
	}
	// 문자열만 봐서는 가운데 폴더가 링크로 밖을 가리키는 걸 못 잡는다 (조사E #9).
	// 뿌리와 가운데 폴더는 되풀이되니 결과를 재 쓴다 (pathcache.go).
	if err := s.checkDir(root, filepath.Dir(target)); err != nil {
		return "", err
	}
	return target, nil
}

// inside 는 target 이 root 아래에 있는지 본다.
func inside(root, target string) bool {
	relative, err := filepath.Rel(root, target)
	if err != nil {
		return false
	}
	if filepath.IsAbs(relative) {
		return false
	}
	return relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))
}

// realPath 는 링크를 푼 실제 경로다. 아직 없는 파일이면 있는 데까지만 풀고
// 나머지 이름은 그대로 붙인다.
func realPath(path string) string {
	tail := ""
	for current := path; ; {
		resolved, err := filepath.EvalSymlinks(current)
		if err == nil {
			return filepath.Join(resolved, tail)
		}
		parent := filepath.Dir(current)
		if parent == current {
			return path
		}
		tail = filepath.Join(filepath.Base(current), tail)
		current = parent
	}
}

// regularFile refuses symlinks and anything that is not a plain file. A missing
// file keeps the os.IsNotExist error so callers can tell the two apart.
func regularFile(abs string) error {
	info, err := os.Lstat(abs)
	if err != nil {
		return err
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return errors.New(i18n.T(i18n.NotRegularFile, abs))
	}
	return nil
}

// storeDepth 는 자리표가 훑는 깊이다. 기억은 store/YYYY/MM 에 있으므로 달 폴더
// 까지만 내려가고 그 안의 파일 목록은 안 읽는다 — 2만 개를 읽으면 싸지 않다.
const storeDepth = 2

// stampWindow 은 폴더 mtime 을 못 믿는 구간이다. 윈도의 시계 눈금은 15ms 라
// 폴더를 만든 바로 뒤에 넣은 파일이 폴더 mtime 을 안 바꾼 것처럼 보일 수 있다
// (indexer 의 coarseWindow 와 같은 조심이다).
const stampWindow = 2 * time.Second

// StoreDirsStamp 은 store/ 아래 폴더들의 고침 시각을 한 줄 값으로 요약한 것이다.
// 폴더 mtime 은 그 안에 파일이 늘거나 줄 때만 바뀌므로, 이 값이 지난번과 같으면
// 파일이 늘지도 줄지도 않았다는 뜻이다. 읽기 명령은 그때 파일 2만 개를 stat 하는
// 따라잡기를 통째로 건너뛴다.
//
// 방금 바뀐 폴더가 하나라도 있으면 빈 문자열이다 — 「모르겠다」 라는 뜻이고
// 부르는 쪽은 예전처럼 전부 훑는다. 있는 파일의 내용만 손으로 고친 것도 못
// 잡는다. 고침은 전부 inbox 를 거치고 `mem index` 는 늘 전체 증분을 하므로
// 그 길로 따라잡힌다.
func (s *Store) StoreDirsStamp() (string, error) {
	sum := sha256.New()
	fresh := false
	if err := stampDir(sum, s.StoreDir(), ".", storeDepth, time.Now(), &fresh); err != nil {
		return "", err
	}
	if fresh {
		return "", nil
	}
	return hex.EncodeToString(sum.Sum(nil)), nil
}

func stampDir(sum hash.Hash, abs, rel string, depth int, now time.Time, fresh *bool) error {
	info, err := os.Stat(abs)
	if err != nil {
		return err
	}
	if age := now.Sub(info.ModTime()); age < stampWindow && age > -stampWindow {
		*fresh = true
	}
	fmt.Fprintf(sum, "%s|%d\n", rel, info.ModTime().UnixNano())
	if depth == 0 {
		return nil
	}
	entries, err := os.ReadDir(abs)
	if err != nil {
		return err
	}
	for _, entry := range entries {
		if !entry.IsDir() {
			continue
		}
		next := filepath.Join(abs, entry.Name())
		if err := stampDir(sum, next, rel+"/"+entry.Name(), depth-1, now, fresh); err != nil {
			return err
		}
	}
	return nil
}
