package quality

import (
	"fmt"
	"sort"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// C14 `stale-basis` — 근거로 삼은 기억이 죽었다 (설계 `2026-08-30-무효화전파설계.md`).
//
// X 가 죽으면(덮였거나 무효 날짜가 지났으면) X 를 근거로 삼은 Y 는 아직 맞는
// 말인지 사람이 한 번 봐야 한다. **판정은 안 한다** — 큐에 올리기까지다.
//
// 전파는 **1단만**이다. Y 를 가리킨 Z 는 안 본다. 사람이 Y 를 덮으면 그때 Z 가
// 1단 대상이 되어 연쇄가 한 칸씩 사람 판정을 거쳐 굴러간다.

// basisSource·basisLink 는 어떤 관계로 걸렸나다. `sources` 의 `mem:` 이 진짜
// 근거고, `links` 는 「이어지는 기억」이라 그보다 약하다 — 문구를 갈라 적는다.
const (
	basisSource = "source"
	basisLink   = "link"
)

// deadBasis 는 죽은 근거 하나다.
type deadBasis struct {
	id   string
	by   string
	dead *model.Memory
}

// staleBasis 는 C14 다. 죽은 기억을 근거·링크로 가리킨 살아 있는 기억을 찾는다.
func staleBasis(memories []*model.Memory, opt RepoOptions,
	add func(*model.Memory, string, string, ...string)) {
	dead := map[string]*model.Memory{}
	for _, one := range memories {
		if isDead(one, opt) {
			dead[one.ID] = one
		}
	}
	if len(dead) == 0 {
		return
	}
	for _, m := range memories {
		if isDead(m, opt) || model.IsFutureDate(m.StaleAfter, opt.Now) {
			continue
		}
		found := deadBasisOf(m, dead)
		if len(found) == 0 {
			continue
		}
		add(m, RuleStaleBasis, basisReason(found), basisRelated(found)...)
	}
}

// isDead 는 덮였거나 무효 날짜가 오늘까지 지난 기억인지다. 둘 중 하나면 된다 —
// 덮은 새 기억 없이 그냥 무효가 된 것도 근거가 죽은 것이다.
func isDead(m *model.Memory, opt RepoOptions) bool {
	if m.SupersededBy != "" {
		return true
	}
	return model.IsDate(m.InvalidAt) && !model.IsFutureDate(m.InvalidAt, opt.Now)
}

// deadBasisOf 는 이 기억이 가리킨 죽은 기억들이다. sources 의 `mem:` 이 먼저고,
// 같은 id 가 links 에도 있으면 근거 쪽만 남긴다.
//
// auto_links 는 안 본다 — 기계가 만든 파생 표라 사람이 적은 links 만 센다.
func deadBasisOf(m *model.Memory, dead map[string]*model.Memory) []deadBasis {
	seen := map[string]bool{}
	found := []deadBasis{}
	for _, source := range m.Sources {
		id := strings.TrimPrefix(source, model.SourceMem)
		if id == source || seen[id] || skipBasis(m, id, dead) {
			continue
		}
		seen[id] = true
		found = append(found, deadBasis{id: id, by: basisSource, dead: dead[id]})
	}
	for _, id := range m.Links {
		if seen[id] || skipBasis(m, id, dead) {
			continue
		}
		seen[id] = true
		found = append(found, deadBasis{id: id, by: basisLink, dead: dead[id]})
	}
	sort.SliceStable(found, func(a, b int) bool {
		if found[a].by != found[b].by {
			return found[a].by == basisSource
		}
		return found[a].id < found[b].id
	})
	return found
}

// BasisNext 는 C14 에서 사람이 그다음에 칠 명령이다. 큐(`mem review --kind basis`)와
// lint 안내가 **같은 한 벌**을 쓴다 — 두 벌이면 한쪽만 고쳐진다.
//
// **`--invalid-at` 만 주는 길은 안 적는다.** superseded_by 와 한 짝이라 혼자
// 주면 관문이 거절한다 (model.Validate).
func BasisNext(id string) []string {
	return []string{
		fmt.Sprintf("mem show %s   (아직 맞는 말인지 본다)", id),
		fmt.Sprintf("mem set %s --by <새id>   (덮였으면 — 새 기억을 먼저 넣는다)", id),
		fmt.Sprintf("mem set %s --stale-after <날짜>   (아직 맞다 — 그날 다시 본다)", id),
	}
}

// skipBasis 는 이 죽은 근거를 큐에 안 올릴 자리인지다.
//
// 자기 자신 말고도, **그 기억을 덮은 장본인이 나 자신이면 건너뛴다.**
// `--sources mem:A` 로 B 를 만들고 `mem set A --by B` 를 치면 B 가 제 손으로
// 죽인 A 때문에 제가 검토 큐에 올라간다 — 사람이 방금 판정을 끝낸 자리다.
func skipBasis(m *model.Memory, id string, dead map[string]*model.Memory) bool {
	if id == m.ID || dead[id] == nil {
		return true
	}
	return dead[id].SupersededBy == m.ID
}

// basisReason 은 사람이 읽을 한 줄이다. 근거로 걸린 것이 하나라도 있으면 그것을
// 말하고, 링크로만 걸렸으면 「근거로 쓴 것인지 본다」로 문구가 달라진다.
func basisReason(found []deadBasis) string {
	first := found[0]
	line := ""
	if first.by == basisSource {
		line = fmt.Sprintf("근거 `mem:%s` 가 %s", first.id, deadWords(first.dead))
	} else {
		line = fmt.Sprintf("이 기억이 links 로 가리킨 %s 가 %s. 근거로 쓴 것인지 본다",
			first.id, deadWords(first.dead))
	}
	if len(found) > 1 {
		line += fmt.Sprintf(" (죽은 근거가 %d건 더 있다)", len(found)-1)
	}
	return line
}

// deadWords 는 그 기억이 어떻게 죽었나다.
func deadWords(dead *model.Memory) string {
	if dead.SupersededBy == "" {
		return fmt.Sprintf("%s 부터 무효다", dead.InvalidAt)
	}
	if dead.InvalidAt == "" {
		return fmt.Sprintf("덮였다 (덮은 것은 %s)", dead.SupersededBy)
	}
	return fmt.Sprintf("%s 에 덮였다 (덮은 것은 %s)", dead.InvalidAt, dead.SupersededBy)
}

// basisRelated 는 같이 볼 기억이다. 죽은 근거와 그것을 덮은 기억을 같이 준다.
func basisRelated(found []deadBasis) []string {
	out := []string{}
	for _, one := range found {
		out = append(out, one.id)
		if one.dead.SupersededBy != "" {
			out = append(out, one.dead.SupersededBy)
		}
	}
	return out
}
