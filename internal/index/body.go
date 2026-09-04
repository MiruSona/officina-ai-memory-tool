package index

import (
	"path/filepath"
	"sync"

	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 본문은 DB 에 안 담는다 (파도 B 후속).
//
// md 가 정본이고 index.db 는 지워도 되는 파생물이다. 그런데 v0.1 은 본문을
// DB 에도 한 벌 더 담고 있었고 20k 에서 그것만 28MB — DB 전체의 3할이었다.
// 검색·랭킹은 본문 원문이 아니라 FTS 조각과 요약만 보므로, 본문이 정말
// 필요한 자리(구절 가산 · show)에서 **그때 상위 몇 건만** 파일에서 읽는다.
//
// 읽기는 store 를 지난다 — 경로 감옥(EvalSymlinks)·cp949 폴백·재시도가
// 거기 한 곳에 있다. 파일이 없어졌으면 빈 본문이고 Missing 이 참이다.

// bodySource 는 본문을 읽는 쪽이다. 늦게 한 번만 만든다.
type bodySource struct {
	once   sync.Once
	opened *store.Store
}

// SetBodySource 는 이미 열어 둔 저장소를 본문 읽기에 쓴다. 안 부르면 색인
// 파일 옆(Memory/)을 스스로 연다.
func (d *DB) SetBodySource(opened *store.Store) {
	d.body.once.Do(func() {})
	d.body.opened = opened
}

func (d *DB) bodyStore() *store.Store {
	d.body.once.Do(func() {
		if d.body.opened == nil {
			d.body.opened = store.Open(filepath.Dir(d.Path), true)
		}
	})
	return d.body.opened
}

// BodyOf 는 store 상대 경로 하나의 본문이다. 못 읽으면 빈 글이다.
func (d *DB) BodyOf(path string) string {
	if path == "" {
		return ""
	}
	file, err := d.bodyStore().ReadMemory(path)
	if err != nil || file == nil || file.Memory == nil {
		return ""
	}
	return file.Memory.Body
}

// BodiesOf 는 여러 건을 한 번에 읽는다. 상위 몇 건에만 쓰라고 만든 것이다 —
// 저장소 전체에 부르면 파일 2만 개를 연다.
func (d *DB) BodiesOf(paths []string) map[string]string {
	out := make(map[string]string, len(paths))
	for _, path := range paths {
		if _, done := out[path]; done {
			continue
		}
		out[path] = d.BodyOf(path)
	}
	return out
}
