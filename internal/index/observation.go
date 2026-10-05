package index

import (
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 모음 기억(observation)의 낡음 판정 — 색인 때 · LLM 0 (자동쌓기설계 3-4).
//
// 판정 ① 근거 하나가 죽었다(덮임·무효·되돌려져 보류됨·없어짐) ② basis_hash 가 지금
// 근거와 다르다. ③ 「같은 무리에 새 구성원이 생김」은 무리 규칙을 다시 돌려야 알아
// 색인 때 안 본다 — `mem consolidate --plan` 이 「다시」로 잡는다.
// 낡았다고 스스로 다시 쓰지 않는다. 표시만 한다.

// 낡음 까닭 (memories.obs_stale 값). 사람 말은 search·review 가 붙인다.
const (
	ObsStaleMissing    = "missing"
	ObsStaleHeld       = "held"
	ObsStaleSuperseded = "superseded"
	ObsStaleChanged    = "changed"
)

// basisRow 는 근거 기억 하나의 색인 행에서 해시에 쓰는 칸이다.
type basisRow struct {
	part    model.BasisPart
	coverer string
	invalid bool
}

// refreshObservations 는 모음 기억마다 낡음 까닭을 다시 적는다. 모음 기억이 없는
// 저장소는 질의 한 번으로 끝난다 — 옛 저장소의 색인 값은 그대로다.
func (d *DB) refreshObservations() error {
	type pending struct{ id, hash, old string }
	rows, err := d.sql.Query("SELECT id, basis_hash, obs_stale FROM memories WHERE type = ?", model.TypeObservation)
	if err != nil {
		return err
	}
	list := []pending{}
	for rows.Next() {
		one := pending{}
		if err := rows.Scan(&one.id, &one.hash, &one.old); err != nil {
			rows.Close()
			return err
		}
		list = append(list, one)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	for _, one := range list {
		reason, err := d.observationStale(one.id, one.hash)
		if err != nil {
			return err
		}
		if reason == one.old {
			continue
		}
		if _, err := d.sql.Exec("UPDATE memories SET obs_stale = ? WHERE id = ?", reason, one.id); err != nil {
			return err
		}
	}
	return nil
}

// observationStale 은 모음 기억 하나의 낡음 까닭이다. 비면 안 낡았다.
func (d *DB) observationStale(id, written string) (string, error) {
	ids, err := d.memSources(id)
	if err != nil {
		return "", err
	}
	found, err := d.basisRows(ids)
	if err != nil {
		return "", err
	}
	inSet := map[string]bool{}
	for _, one := range ids {
		inSet[one] = true
	}
	parts := []model.BasisPart{}
	for _, one := range ids {
		if _, ok := found[one]; !ok {
			return ObsStaleMissing, nil
		}
	}
	for _, one := range ids {
		parts = append(parts, found[one].part)
	}
	if written != "" && model.BasisHash(parts) == written {
		return "", nil
	}
	reason := staleReason(ids, found, inSet)
	if written == "" && reason == ObsStaleChanged {
		// 해시 없이 손으로 쓴 옛 모음은 견줄 해시가 없다. 죽음·보류만 본다 —
		// 안 그러면 쓰자마자 영영 [낡음] 이다 (리뷰 2026-10-05).
		return "", nil
	}
	return reason, nil
}

// staleReason 은 해시가 어긋났을 때(또는 해시 없이 손으로 쓴 모음일 때) 까닭을
// 고른다. 보류가 먼저다 — 되돌리기(`mem auto undo`)가 낸 낡음이 가장 흔하다.
// 사슬 카드는 옛 구성원이 원래 죽어 있으니, 덮은 쪽이 **근거 밖**일 때만 덮임으로 친다.
func staleReason(ids []string, found map[string]basisRow, inSet map[string]bool) string {
	for _, one := range ids {
		if found[one].part.Held {
			return ObsStaleHeld
		}
	}
	for _, one := range ids {
		row := found[one]
		if row.part.Dead && (row.coverer == "" || !inSet[row.coverer]) {
			return ObsStaleSuperseded
		}
	}
	return ObsStaleChanged
}

// memSources 는 기억 하나의 `mem:` 근거를 적힌 차례로 준다.
func (d *DB) memSources(id string) ([]string, error) {
	rows, err := d.sql.Query("SELECT value FROM sources WHERE mem_id = ? AND kind = 'mem' ORDER BY rowid", id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []string{}
	for rows.Next() {
		value := ""
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, strings.TrimSpace(value))
	}
	return out, rows.Err()
}

// basisRows 는 id 들의 색인 행을 한 번에 읽는다. 없는 id 는 결과에 없다.
func (d *DB) basisRows(ids []string) (map[string]basisRow, error) {
	out := map[string]basisRow{}
	if len(ids) == 0 {
		return out, nil
	}
	holes := strings.TrimSuffix(strings.Repeat("?,", len(ids)), ",")
	args := make([]any, 0, len(ids))
	for _, one := range ids {
		args = append(args, one)
	}
	rows, err := d.sql.Query(`SELECT id, title, summary, body_hash, COALESCE(superseded_by, ''),
		invalid_at IS NOT NULL, review FROM memories WHERE id IN (`+holes+`)`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		row := basisRow{}
		held := 0
		if err := rows.Scan(&row.part.ID, &row.part.Title, &row.part.Summary, &row.part.BodyHash,
			&row.coverer, &row.invalid, &held); err != nil {
			return nil, err
		}
		row.part.Held = held == 1
		row.part.Dead = row.coverer != "" || row.invalid
		out[row.part.ID] = row
	}
	return out, rows.Err()
}

// BasisRef 는 검색 결과 아래 들여 쓴 근거 한 줄이다.
type BasisRef struct {
	ID    string `json:"id"`
	Title string `json:"title"`
}

// BasisOf 는 모음 기억 하나의 근거 id·제목이다. 색인에 없는 근거는 제목이 빈다.
// 보류된 근거(`auto undo` 등)는 숨긴다 — 검색·훅에 안 뜬다고 한 기억의 id·제목이
// 근거 줄로 새면 안 된다 (리뷰 2026-10-05). 덮인 근거는 그대로 보인다 — 사슬 카드의
// 옛 구성원이 바로 그 카드의 내용이다.
func (d *DB) BasisOf(id string) ([]BasisRef, error) {
	ids, err := d.memSources(id)
	if err != nil {
		return nil, err
	}
	found, err := d.basisRows(ids)
	if err != nil {
		return nil, err
	}
	out := make([]BasisRef, 0, len(ids))
	for _, one := range ids {
		row, ok := found[one]
		if ok && row.part.Held {
			continue
		}
		out = append(out, BasisRef{ID: one, Title: row.part.Title})
	}
	return out, nil
}

// HasObservations 는 모음 기억이 하나라도 색인에 있는지다.
func (d *DB) HasObservations() bool {
	found := 0
	err := d.sql.QueryRow("SELECT 1 FROM memories WHERE type = ? LIMIT 1", model.TypeObservation).Scan(&found)
	return err == nil
}

// AutoLinkPairs 는 색인이 찾아 둔 이웃(auto_links) 전부다. `mem consolidate` 의
// 링크 무리(②)가 사람이 적은 links 와 합쳐 쓴다.
func (d *DB) AutoLinkPairs() ([][2]string, error) {
	rows, err := d.sql.Query(`SELECT a.id, b.id FROM auto_links l
		JOIN memories a ON a.docid = l.src JOIN memories b ON b.docid = l.dst ORDER BY a.id, b.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := [][2]string{}
	for rows.Next() {
		pair := [2]string{}
		if err := rows.Scan(&pair[0], &pair[1]); err != nil {
			return nil, err
		}
		out = append(out, pair)
	}
	return out, rows.Err()
}
