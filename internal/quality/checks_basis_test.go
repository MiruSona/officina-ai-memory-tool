package quality

import (
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// C14 경계 열하나 (설계 `2026-08-30-무효화전파설계.md` 8절).
// 죽은 근거를 가진 기억만 큐에 오르고, 연쇄는 1단에서 멈춘다.

// basisMemory 는 C14 시험용 기억 하나다. 다른 규칙에 안 걸리게 갖춰 둔다.
func basisMemory(id, kind string) *model.Memory {
	return &model.Memory{ID: id, Type: kind, Date: "2026-08-22",
		Title:   "훅 주입 상한을 바이트로 잰다",
		Summary: "훅이 밀어 넣는 글은 글자 수가 아니라 UTF-8 바이트로 재야 한다. 한글은 한 글자가 세 바이트다",
		Tags:    []string{"hook", "korean"}, Scope: "aimemorytool", Spec: model.SpecV2,
		Body: "훅 상한은 바이트로 잰다.\n글자 수로 재면 한글에서 세 배로 틀린다.\n넘치면 우리가 먼저 자른다."}
}

// deadMemory 는 이미 죽은 기억이다. 덮은 새 기억 id 를 주면 덮인 꼴이 된다.
func deadMemory(id, newer string) *model.Memory {
	one := basisMemory(id, model.TypeDecision)
	one.InvalidAt = "2026-08-23"
	one.SupersededBy = newer
	return one
}

func basisFindings(t *testing.T, memories ...*model.Memory) map[string]bool {
	t.Helper()
	report := CheckRepo(memories, wave2dOptions(t))
	hit := map[string]bool{}
	for id, list := range report.ByID {
		if hasRule(list, RuleStaleBasis) {
			hit[id] = true
		}
	}
	return hit
}

func basisReasonOf(t *testing.T, id string, memories ...*model.Memory) string {
	t.Helper()
	report := CheckRepo(memories, wave2dOptions(t))
	for _, one := range report.ByID[id] {
		if one.Rule == RuleStaleBasis {
			return one.Reason
		}
	}
	return ""
}

// 1. 연쇄는 1단에서 멈춘다. Z 는 Y 를 가리켰을 뿐이라 안 걸린다.
func TestBasisStopsAtOneHop(t *testing.T) {
	x := deadMemory("20260822-aaaa0001", "20260830-bbbb0001")
	y := basisMemory("20260822-aaaa0002", model.TypeFact)
	y.Sources = []string{model.SourceMem + x.ID}
	z := basisMemory("20260822-aaaa0003", model.TypeHistory)
	z.Sources = []string{model.SourceMem + y.ID}
	hit := basisFindings(t, x, y, z)
	if !hit[y.ID] {
		t.Fatal("죽은 근거를 삼은 기억이 안 걸렸다")
	}
	if hit[z.ID] {
		t.Fatal("2단까지 번졌다. 1단만 봐야 한다")
	}
}

// 2. 자기가 자기를 가리킨 것은 판정할 것이 없다.
func TestBasisSkipsSelfLink(t *testing.T) {
	one := basisMemory("20260822-bbbb0001", model.TypeDecision)
	one.InvalidAt = "2026-08-23"
	one.Links = []string{one.ID}
	if basisFindings(t, one)[one.ID] {
		t.Fatal("자기참조가 걸렸다")
	}
}

// 3. 이미 무효인 기억은 안 부른다. 검색·훅에서 이미 빠진 기억이다.
func TestBasisSkipsDeadHolder(t *testing.T) {
	x := deadMemory("20260822-cccc0001", "20260830-cccc9999")
	expired := basisMemory("20260822-cccc0002", model.TypeFact)
	expired.Sources = []string{model.SourceMem + x.ID}
	expired.InvalidAt = "2026-08-21"
	covered := basisMemory("20260822-cccc0003", model.TypeFact)
	covered.Sources = []string{model.SourceMem + x.ID}
	covered.SupersededBy = "20260830-cccc8888"
	hit := basisFindings(t, x, expired, covered)
	if hit[expired.ID] || hit[covered.ID] {
		t.Fatalf("이미 무효인 기억이 걸렸다 : %v", hit)
	}
}

// 4. 덮은 쪽(superseded_by = X)은 근거가 아니다. 그 기억은 이미 죽었다.
func TestBasisSkipsSupersededBy(t *testing.T) {
	x := deadMemory("20260822-dddd0001", "20260830-dddd9999")
	y := basisMemory("20260822-dddd0002", model.TypeDecision)
	y.SupersededBy = x.ID
	if basisFindings(t, x, y)[y.ID] {
		t.Fatal("덮인 기억을 큐에 올렸다")
	}
}

// 5. `mem:` 만 근거다. file:·note: 는 id 처럼 생겼어도 안 본다.
func TestBasisOnlyMemPrefix(t *testing.T) {
	x := deadMemory("20260822-eeee0001", "20260830-eeee9999")
	y := basisMemory("20260822-eeee0002", model.TypeFact)
	y.Sources = []string{"file:" + x.ID, "note:mem:" + x.ID, "url:https://example.com/" + x.ID}
	if basisFindings(t, x, y)[y.ID] {
		t.Fatal("mem: 가 아닌 근거가 걸렸다")
	}
}

// 6. 근거가 살아 있으면 아무 말도 안 한다.
func TestBasisQuietWhenBasisLives(t *testing.T) {
	x := basisMemory("20260822-ffff0001", model.TypeDecision)
	y := basisMemory("20260822-ffff0002", model.TypeFact)
	y.Sources = []string{model.SourceMem + x.ID}
	if basisFindings(t, x, y)[y.ID] {
		t.Fatal("살아 있는 근거를 죽었다고 했다")
	}
}

// 7. 덮은 것 없이 invalid_at 만 지나도 근거가 죽은 것이다.
func TestBasisCountsInvalidWithoutSuperseder(t *testing.T) {
	x := basisMemory("20260822-1111aaaa", model.TypeDecision)
	x.InvalidAt = "2026-08-22"
	y := basisMemory("20260822-1111bbbb", model.TypeFact)
	y.Sources = []string{model.SourceMem + x.ID}
	if !basisFindings(t, x, y)[y.ID] {
		t.Fatal("덮은 것 없이 무효가 된 근거를 안 셌다")
	}
	if reason := basisReasonOf(t, y.ID, x, y); !strings.Contains(reason, "무효") {
		t.Fatalf("무효 문구가 아니다 : %q", reason)
	}
}

// 8. 스누즈 — stale_after 가 미래면 그날까지 큐에서 뺀다.
func TestBasisSnoozedByStaleAfter(t *testing.T) {
	x := deadMemory("20260822-2222aaaa", "20260830-2222dddd")
	snoozed := basisMemory("20260822-2222bbbb", model.TypeFact)
	snoozed.Sources = []string{model.SourceMem + x.ID}
	snoozed.StaleAfter = "2027-01-01"
	woken := basisMemory("20260822-2222cccc", model.TypeFact)
	woken.Sources = []string{model.SourceMem + x.ID}
	woken.StaleAfter = "2026-08-01"
	hit := basisFindings(t, x, snoozed, woken)
	if hit[snoozed.ID] {
		t.Fatal("미룬 기억이 큐에 다시 떴다")
	}
	if !hit[woken.ID] {
		t.Fatal("미룬 날이 지났는데 큐에 안 왔다")
	}
}

// 9. 링크로만 걸린 것은 문구가 다르다. links 는 근거보다 약하다.
func TestBasisLinkWording(t *testing.T) {
	x := deadMemory("20260822-3333aaaa", "20260830-3333dddd")
	y := basisMemory("20260822-3333bbbb", model.TypeHistory)
	y.Links = []string{x.ID}
	reason := basisReasonOf(t, y.ID, x, y)
	if reason == "" {
		t.Fatal("링크로 걸린 기억이 큐에 없다")
	}
	if !strings.Contains(reason, "links") || strings.HasPrefix(reason, "근거") {
		t.Fatalf("링크 문구가 아니다 : %q", reason)
	}
}

// 10. 없는 id 를 가리킨 것은 C14 가 아니라 D01 자리다.
func TestBasisIgnoresMissingID(t *testing.T) {
	y := basisMemory("20260822-4444bbbb", model.TypeFact)
	y.Sources = []string{model.SourceMem + "20260101-ffffffff"}
	if basisFindings(t, y)[y.ID] {
		t.Fatal("없는 기억을 죽은 근거로 셌다")
	}
}

// 11. 근거·링크가 다 걸려도 기억 하나에 한 줄이다. 근거 쪽 문구를 남긴다.
func TestBasisOneLinePerMemory(t *testing.T) {
	x := deadMemory("20260822-5555aaaa", "20260830-5555dddd")
	other := deadMemory("20260822-5555bbbb", "")
	other.SupersededBy = ""
	y := basisMemory("20260822-5555cccc", model.TypeFact)
	y.Sources = []string{model.SourceMem + x.ID}
	y.Links = []string{other.ID}
	report := CheckRepo([]*model.Memory{x, other, y}, wave2dOptions(t))
	count := 0
	for _, one := range report.ByID[y.ID] {
		if one.Rule == RuleStaleBasis {
			count++
			if !strings.HasPrefix(one.Reason, "근거") {
				t.Fatalf("근거 문구를 먼저 써야 한다 : %q", one.Reason)
			}
			if len(one.Related) < 2 {
				t.Fatalf("같이 볼 기억이 모자라다 : %v", one.Related)
			}
			if len(one.Next) == 0 {
				t.Fatal("다음에 칠 명령이 없다")
			}
		}
	}
	if count != 1 {
		t.Fatalf("한 기억에 %d줄이 났다", count)
	}
}

// 리뷰 #5 — 덮은 장본인은 자기 재검토 큐에 안 오른다.
// `--sources mem:A` 로 만든 B 를 `mem set A --by B` 로 이으면, B 가 제 손으로
// 죽인 A 때문에 제가 큐에 올라가던 자리다.
func TestBasisSkipsTheSuperseder(t *testing.T) {
	newer := basisMemory("20260830-bbbb0002", model.TypeDecision)
	old := deadMemory("20260822-aaaa0011", newer.ID)
	newer.Sources = []string{model.SourceMem + old.ID}
	other := basisMemory("20260822-aaaa0012", model.TypeFact)
	other.Sources = []string{model.SourceMem + old.ID}
	hit := basisFindings(t, old, newer, other)
	if hit[newer.ID] {
		t.Fatal("덮은 장본인이 자기 재검토 큐에 올랐다")
	}
	if !hit[other.ID] {
		t.Fatal("남이 삼은 죽은 근거는 그대로 걸려야 한다")
	}
}

// 서로 links 로 가리킨 두 기억(순환)이라도 검사가 멈추지 않는다 (시험 공백 보강).
func TestBasisHandlesLinkCycle(t *testing.T) {
	left := basisMemory("20260822-aaaa0021", model.TypeFact)
	right := basisMemory("20260822-aaaa0022", model.TypeHistory)
	left.Links = []string{right.ID}
	right.Links = []string{left.ID}
	dead := deadMemory("20260822-aaaa0023", "20260830-bbbb0003")
	left.Sources = []string{model.SourceMem + dead.ID}
	hit := basisFindings(t, left, right, dead)
	if !hit[left.ID] {
		t.Fatal("순환 links 가 있다고 죽은 근거를 놓쳤다")
	}
	if hit[right.ID] {
		t.Fatal("순환을 타고 2단까지 번졌다")
	}
}
