package quality

import (
	"fmt"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// STALE 을 넷으로 잰다 (설계 결정 36).
//
//	① type 별 나이 임계        — 여기(C12)
//	② stale_after 명시 유효기간 — 이미 있다 (C07)
//	③ sources 참조 표류        — 여기서 갈고리만 준다 (D02·D03). 파일과 git 을
//	   아는 쪽(lint·review)이 붙인다. quality 는 파일 시스템을 모른다
//	④ 뜻은 가까운데 사실이 어긋나는 쌍 — 여기(C13). 1단은 2C 의 닮음 표를 그대로
//	   쓰고 2단만 뒤집는다 (겹치면 중복, 어긋나면 모순)
//
// 넷 다 **후보 등급**이다. 「낡았다」는 사람이 판정한다 — 되돌릴 수 없는 자동
// 조치는 안 만든다(불변조건 I5).

// staleAgeDays 는 종류마다 「이만큼 지났으면 한 번 다시 본다」는 날수다.
//
// 검색 감쇠 반감기(history 90 · decision·howto 720)의 두 배로 잡았다. 반감기는
// 「순위가 절반이 되는 때」고, 낡음 후보는 그보다 늦게 물어야 검토 큐가 안
// 넘친다. issue·caution·열린 todo 는 감쇠가 없는 종류라 여기서도 안 묻는다 —
// 안 고쳐진 문제는 오래됐다고 덜 중요해지지 않는다.
// (지금은 typespec 의 stale_days 칸이다 — issue·caution·열린 todo 는 0 이라 안 묻는다)

// staleAge 는 C12 다. 날짜를 못 읽으면 아무 말도 안 한다.
func staleAge(m *model.Memory, now time.Time, kind model.TypeSpec) string {
	days := kind.StaleDays
	if days <= 0 {
		return ""
	}
	written, err := time.ParseInLocation("2006-01-02", m.Date, time.Local)
	if err != nil {
		return ""
	}
	old := int(now.Sub(written).Hours() / 24)
	if old < days {
		return ""
	}
	return fmt.Sprintf("%s 기억인데 %d일이 지났다(임계 %d일). 아직 맞는 말인지 한 번 본다", m.Type, old, days)
}

// SourceMissing 은 근거 하나가 이제 없는지를 묻는 함수다 (설계 결정 36 ③).
// 돌려주는 글은 사람에게 보일 까닭이고, 빈 글이면 멀쩡한 것이다.
// 파일과 git 을 아는 쪽이 붙인다. nil 이면 D02·D03 을 건너뛴다.
type SourceMissing func(m *model.Memory, source string) (rule, reason string)

// staleSources 는 D02·D03 이다. 한 기억에서 규칙마다 한 번만 말한다 — 근거가
// 열 줄인 기억이 열 줄짜리 경고를 내면 아무도 안 읽는다.
func staleSources(m *model.Memory, ask SourceMissing,
	add func(*model.Memory, string, string, ...string)) {
	if ask == nil {
		return
	}
	said := map[string]bool{}
	for _, source := range m.Sources {
		rule, reason := ask(m, source)
		if rule == "" || said[rule] {
			continue
		}
		said[rule] = true
		add(m, rule, reason, source)
	}
}

// staleConflictCut 은 「뜻이 가깝다」로 보는 문장 정렬 값이다. 중복 1단의 자
// (AlignCut)보다 높다 — 중복은 「겹쳐 썼다」만 보면 되지만 모순은 **두 문장이
// 같은 것을 말한다**는 확신이 있어야 한다.
//
// 골든셋 130건 스윕 : 0.35 에서 잡음 23 · 대조군 0, 0.45 부터 STALE 을 놓치기
// 시작하고 0.55 위로는 아무것도 안 남는다. 대조군 0 을 지키는 가장 낮은 값이 0.35 다.
const staleConflictCut = 0.35

func staleConflictFloor() float64 { return staleConflictCut }

// staleConflictPairs 는 C13 이다. **어긋난 짝은 중복 판정과 같은 걸음에서**
// 이미 찾아 둔 것을 받는다 (repo.go duplicates) — 여기서 후보를 다시 뽑으면
// 문서마다 후보 뽑기를 두 번 하게 된다 (리뷰 B · V3).
func staleConflictPairs(found [][]string, memories []*model.Memory,
	add func(*model.Memory, string, string, ...string)) {
	for at, m := range memories {
		if len(found[at]) == 0 {
			continue
		}
		add(m, RuleStaleConflictPair,
			fmt.Sprintf("`%s` 과 같은 말을 하는데 숫자·이름이 어긋난다. 어느 쪽이 지금 맞는지 표시가 없다",
				strings.Join(found[at], "`·`")),
			found[at]...)
	}
}

// clashPairMax 는 한 기억이 댈 어긋난 짝 수다. 넘으면 그 기억이 아니라 저장소가
// 문제라 목록을 길게 내도 사람이 못 쓴다.
const clashPairMax = 3

func clashingIDs(doc *Doc, session *Session) []string {
	ids := []string{}
	for _, other := range session.Candidates(doc) {
		if other.ID == doc.ID || SameBody(doc, other) {
			continue
		}
		// 부르는 쪽이 보는 가장 낮은 자가 곧 문턱이다.
		info := alignOf(doc, other, staleConflictFloor(), staleConflictFloor())
		if info.Best < staleConflictFloor() || !factsClash(info.Left, info.Right) {
			continue
		}
		ids = append(ids, other.ID)
		if len(ids) >= clashPairMax {
			break
		}
	}
	return ids
}

// nextFor 는 저장소 전체 규칙에 붙는 「다음에 칠 명령」이다 (설계 결정 61 —
// 새 규칙 넷도 v0.2 관문의 세 규칙을 그대로 탄다 : ① 다음 명령을 손에 쥐여
// 주고 ② 거절이면 log.md 에 남고 ③ 정밀도 자동 강등을 탄다).
//
// **없는 옵션을 시키지 않는다.** 여기 적는 것은 전부 v0.2 부터 있는 명령이다.
func nextFor(rule string, m *model.Memory, related []string) []string {
	other := ""
	if len(related) > 0 {
		other = related[0]
	}
	switch rule {
	case RuleNotationDrift:
		return []string{
			fmt.Sprintf("mem show %s   (어느 표기가 정본인지 본다)", other),
			"mem set <id> --summary … --body …   (한쪽 표기로 맞춘다)",
		}
	case RuleStaleAge:
		return []string{
			fmt.Sprintf("mem show %s   (아직 맞는 말인지 본다)", m.ID),
			fmt.Sprintf("mem set %s --stale-after <날짜>   (다시 볼 날을 적는다)", m.ID),
		}
	case RuleStaleConflictPair:
		return []string{
			fmt.Sprintf("mem show %s   (어긋난 짝을 본다)", other),
			fmt.Sprintf("mem set %s --by %s   (그 기억이 이것을 덮는다고 적는다)", m.ID, other),
		}
	case RuleSupersedeMissing:
		// 결정 38 — 차례는 max(date) 로 코드가 매기지만, 파일에 쓰는 것은 사람이
		// 친 `mem set --by` 뿐이다. 자동으로 쓰면 살아 있는 결정이 말없이 무효가
		// 된다 (v0.2 결정 26 · 불변조건 I5).
		return []string{
			fmt.Sprintf("mem set %s --by %s   (이 기억이 덮였다고 적는다)", m.ID, other),
		}
	case RuleStaleBasis:
		return BasisNext(m.ID)
	case RuleDeadPath, RuleDeadCommit:
		return []string{
			fmt.Sprintf("mem set %s --sources …   (근거를 지금 있는 것으로 고친다)", m.ID),
		}
	}
	return nil
}

// SupersedeWinner 는 같은 자리에 살아 있는 결정 중 지금 맞는 것으로 볼 하나다
// (설계 결정 38). **차례만 정한다 — 파일은 안 고친다.**
// 날짜가 같으면 id 가 큰 쪽이다 (id 앞머리가 날짜라 그것이 들어온 차례다).
func SupersedeWinner(m *model.Memory, rivals []*model.Memory) string {
	return newestRival(m, rivals)
}
