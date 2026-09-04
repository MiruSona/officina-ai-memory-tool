package embed

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
)

// FileName 은 표 파일의 이름이다.
const FileName = "ko.bin"

// DirName 은 저장소 안에서 표를 두는 자리다 — `Memory/model/ko.bin`.
const DirName = "model"

// Expand 는 앞머리 `~` 를 집 폴더로 바꾼다. mem.toml 에 손으로 적는 값이라
// 사람이 `~/.aimemory/model/ko.bin` 이라고 쓴다.
func Expand(path string) string {
	if path == "" || (path != "~" && !strings.HasPrefix(path, "~/") && !strings.HasPrefix(path, `~\`)) {
		return path
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return path
	}
	if path == "~" {
		return home
	}
	return filepath.Join(home, path[2:])
}

// Find 는 표 파일을 설정에 적힌 자리 → 저장소 안 자리 차례로 찾는다
// (설계 4-8). 없으면 빈 글이고, 그러면 검색은 낱말만으로 돈다.
func Find(configured, repoDir string) string {
	for _, candidate := range []string{Expand(configured), inRepo(repoDir)} {
		if candidate == "" {
			continue
		}
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}

func inRepo(repoDir string) string {
	if repoDir == "" {
		return ""
	}
	return filepath.Join(repoDir, DirName, FileName)
}

// 표는 프로세스 안에서 한 번만 읽는다. 사다리 일곱 칸이 칸마다 4MB 를 다시
// 읽으면 그것이 곧 느림이다.
var (
	cacheLock sync.Mutex
	cached    map[string]*Table
)

// Open 은 찾은 표를 준다. 파일이 없거나 못 읽으면 **nil 이다. 오류가 아니다** —
// 부르는 쪽은 nil 이면 임베딩 갈래를 통째로 건너뛴다 (설계 4-8 우아한 퇴화).
func Open(configured, repoDir string) *Table {
	path := Find(configured, repoDir)
	if path == "" {
		return nil
	}
	key := path
	if info, err := os.Stat(path); err == nil {
		key = path + "|" + info.ModTime().UTC().Format("20060102150405.000") +
			"|" + strconv.FormatInt(info.Size(), 10)
	}
	cacheLock.Lock()
	defer cacheLock.Unlock()
	if table, have := cached[key]; have {
		return table
	}
	table, err := Load(path)
	if err != nil {
		table = nil
	}
	if cached == nil {
		cached = map[string]*Table{}
	}
	cached[key] = table
	return table
}

// Forget 은 읽어 둔 표를 버린다. 시험이 파일을 바꿔 가며 부를 때 쓴다.
func Forget() {
	cacheLock.Lock()
	defer cacheLock.Unlock()
	cached = nil
}
