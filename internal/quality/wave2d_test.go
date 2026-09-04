package quality

import (
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 물결 2 · 2D — 문서 품질 규칙. NOTATION·NOVALUE·STALE 넷·supersede 차례·
// decision-gate·summary-body-match 를 잰다.

func wave2dOptions(t *testing.T) RepoOptions {
	t.Helper()
	return RepoOptions{Options: Options{Config: config.Default("quality-golden"),
		Vocab: testdataVocab(t), Now: time.Date(2026, 8, 23, 0, 0, 0, 0, time.Local)}}
}

// TestNotationDriftHasID 는 C11 이 기억마다 나오는지 본다 (설계 결정 26).
// Wide(저장소 전체 이야기)로만 내면 id 가 없어 채점표에도 review 큐에도 안 뜬다 —
// 재현율 0.000 의 진짜 원인이었다.
func TestNotationDriftHasID(t *testing.T) {
	memories, err := LoadMemories(filepath.FromSlash(storePath))
	if err != nil {
		t.Fatal(err)
	}
	report := CheckRepo(memories, wave2dOptions(t))
	found := 0
	for id, list := range report.ByID {
		for _, one := range list {
			if one.Rule != RuleNotationDrift {
				continue
			}
			found++
			if id == "" || one.ID != id {
				t.Fatalf("C11 에 기억 id 가 없다 : %+v", one)
			}
			if one.Level != GradeWarn {
				t.Fatalf("C11 은 경고 등급이어야 한다 (결정 26) : %s", one.Level)
			}
			if len(one.Next) == 0 {
				t.Fatalf("C11 에 다음 명령이 없다 (결정 61) : %+v", one)
			}
		}
	}
	if found == 0 {
		t.Fatal("실기억 130건에서 C11 이 한 건도 안 켜졌다")
	}
	for _, one := range report.Wide {
		if one.Rule == RuleNotationDrift {
			t.Fatal("C11 이 아직 Wide 에 남아 있다 — 채점이 못 본다")
		}
	}
}

// TestUnfixedMarkerSameDate 는 B10 이 「같은 날짜」에서도 도는지 본다.
// 실기억 206건이 다 하루 안에 들어와 날짜만 보던 옛 자로는 한 번도 안 켜졌다.
func TestUnfixedMarkerSameDate(t *testing.T) {
	older := &model.Memory{ID: "20260822-aaaa1111", Type: model.TypeIssue, Date: "2026-08-22",
		Summary: "무언가 안 된다", Tags: []string{"bug", "cli"}, Body: "## 증상\n안 된다.\n\n## 해결\n안 고침."}
	newer := &model.Memory{ID: "20260822-bbbb2222", Type: model.TypeHistory, Date: "2026-08-22",
		Summary: "그것을 고쳤다", Tags: []string{"bug", "cli"}, Body: "고쳤다. 6.99초가 됐다."}
	report := CheckRepo([]*model.Memory{older, newer}, wave2dOptions(t))
	if !hasRule(report.ByID[older.ID], RuleUnfixedMarker) {
		t.Fatal("같은 날짜에 더 새 기억이 있는데 B10 이 안 켜졌다")
	}
	if hasRule(report.ByID[newer.ID], RuleUnfixedMarker) {
		t.Fatal("가장 새 기억에 B10 이 켜졌다")
	}
}

// TestUnfixedWordNotBarelyLater 는 맨 「나중에」로는 안 켜지는지 본다.
// 「나중에 다시 볼 때 아래를 먼저 본다」는 멀쩡한 글이다 (대조군 a08e73d4).
func TestUnfixedWordNotBarelyLater(t *testing.T) {
	if unfixedWord(&model.Memory{Body: "나중에 다시 볼 때 아래를 먼저 본다."}) != "" {
		t.Fatal("맨 「나중에」가 아직 미해결 표시로 잡힌다")
	}
	if unfixedWord(&model.Memory{Body: "이건 나중에 고친다."}) == "" {
		t.Fatal("「나중에 고친다」는 미해결 표시다")
	}
}

// TestNoValueBranches 는 B12 의 세 갈래를 하나씩 본다 (설계 결정 37).
func TestNoValueBranches(t *testing.T) {
	// ① 확인 가능한 조각이 없다.
	thin := &model.Memory{ID: "20260823-cccc3333", Type: model.TypeHistory, Date: "2026-08-23",
		Summary: "이것저것 손봤다", Body: "오늘은 여기저기 조금씩 손봤다.\n다음에 무엇을 할지는 아직 못 정했다."}
	if noValueReason(thin, model.DefaultTypes().Spec(thin.Type)) == "" {
		t.Fatal("숫자·경로·이름이 하나도 없는 history 를 B12 가 안 잡는다")
	}
	// caution 은 「함정은 이것이다」가 본체라 숫자가 없어도 쓸모가 있다.
	care := *thin
	care.Type = model.TypeCaution
	if noValueReason(&care, model.DefaultTypes().Spec(care.Type)) != "" {
		t.Fatal("caution 에까지 확인 가능한 조각을 요구한다")
	}
	// ② 결론 문장 꼴이 없다 (decision 만).
	nod := &model.Memory{ID: "20260823-dddd4444", Type: model.TypeDecision, Date: "2026-08-23",
		Summary: "gc 이야기를 적어 뒀다", Body: "gc 는 24시간에 한 번 돌았다.\n그때 200건을 옮겼다."}
	if noValueReason(nod, model.DefaultTypes().Spec(nod.Type)) == "" {
		t.Fatal("결론 문장이 없는 decision 을 B12 가 안 잡는다")
	}
	nod.Body += "\n결론 : 하루에 한 번만 돌린다."
	if noValueReason(nod, model.DefaultTypes().Spec(nod.Type)) != "" {
		t.Fatal("결론이 있는 decision 을 B12 가 아직 잡는다")
	}
	// ③ 본문이 제목·요약의 되풀이다.
	echo := &model.Memory{ID: "20260823-eeee5555", Type: model.TypeHowto, Date: "2026-08-23",
		Title: "색인을 v3 으로 올린다", Summary: "색인을 v3 으로 올린다",
		Body: "색인을 v3 으로 올린다."}
	if !echoesHead(echo) {
		t.Fatal("본문이 제목·요약 그대로인데 되풀이로 안 본다")
	}
}

// TestNoValueHasNext 는 B12 가 다음에 칠 명령을 손에 쥐여 주는지 본다 (결정 61).
func TestNoValueHasNext(t *testing.T) {
	m := &model.Memory{ID: "20260823-ffff6666", Type: model.TypeHistory, Date: "2026-08-23",
		Summary: "이것저것 손봤다", Body: "오늘은 여기저기 조금씩 손봤다."}
	found := checkValue(m, Options{}.normalized())
	if len(found) != 1 || len(found[0].Next) == 0 {
		t.Fatalf("B12 에 다음 명령이 없다 : %+v", found)
	}
	for _, step := range found[0].Next {
		if !strings.HasPrefix(step, "mem ") && !strings.Contains(step, "다시 넣는다") {
			t.Fatalf("없는 명령을 시킨다 : %s", step)
		}
	}
}

// TestStaleAgeByType 는 C12 가 종류마다 다른 나이 자를 쓰는지 본다 (결정 36 ①).
func TestStaleAgeByType(t *testing.T) {
	now := time.Date(2027, 8, 23, 0, 0, 0, 0, time.Local) // 366일 뒤
	old := &model.Memory{Type: model.TypeHistory, Date: "2026-08-22"}
	if staleAge(old, now, model.DefaultTypes().Spec(old.Type)) == "" {
		t.Fatal("한 해 지난 history 를 C12 가 안 잡는다")
	}
	old.Type = model.TypeDecision
	if staleAge(old, now, model.DefaultTypes().Spec(old.Type)) != "" {
		t.Fatal("decision 을 history 와 같은 자로 잰다")
	}
	// fact 는 360일 자다 — 366일 지났으니 잡힌다.
	old.Type = model.TypeFact
	if staleAge(old, now, model.DefaultTypes().Spec(old.Type)) == "" {
		t.Fatal("한 해 지난 fact 를 C12 가 안 잡는다")
	}
	// 안 고쳐진 문제는 오래됐다고 덜 중요해지지 않는다.
	old.Type = model.TypeIssue
	if staleAge(old, now, model.DefaultTypes().Spec(old.Type)) != "" {
		t.Fatal("issue 에 나이 자를 댄다")
	}
}

// TestStaleSourcesNeedsHook 은 D02·D03 이 갈고리 없이는 안 도는지 본다.
// quality 는 파일 시스템도 git 도 모른다 — 모르는 것을 「없다」고 말하면 안 된다.
func TestStaleSourcesNeedsHook(t *testing.T) {
	m := &model.Memory{ID: "20260823-7777aaaa", Sources: []string{"file:없는파일.md", "commit:deadbeef"}}
	said := 0
	staleSources(m, nil, func(*model.Memory, string, string, ...string) { said++ })
	if said != 0 {
		t.Fatal("갈고리가 없는데 근거가 없다고 말했다")
	}
	staleSources(m, func(_ *model.Memory, source string) (string, string) {
		if strings.HasPrefix(source, model.SourceCommit) {
			return RuleDeadCommit, "그 커밋이 없다"
		}
		return RuleDeadPath, "그 경로가 없다"
	}, func(*model.Memory, string, string, ...string) { said++ })
	if said != 2 {
		t.Fatalf("근거 둘에 규칙 둘이 아니라 %d 건이 나왔다", said)
	}
}

// TestGateRivalsNeedsTwoTags 는 C04 가 태그 하나로는 안 막는지 본다 (결정 39).
// scope 가 사실상 하나인 저장소에서 태그 하나만 겹쳐도 막히던 것이 v0.2 의
// 거짓 거절 원인이었다.
func TestGateRivalsNeedsTwoTags(t *testing.T) {
	mine := &model.Memory{ID: "20260823-8888bbbb", Type: model.TypeDecision, Date: "2026-08-23",
		Scope: "aimemorytool", Tags: []string{"gc", "design"},
		Summary: "도구가 스스로 기억을 지우는 길은 아예 만들지 않고 접어 두기만 한다"}
	oneTag := &model.Memory{ID: "20260823-9999cccc", Type: model.TypeDecision, Date: "2026-08-23",
		Scope: "aimemorytool", Tags: []string{"index", "design"},
		Summary: "색인 스키마를 v3 으로 올리고 마이그레이션은 한 번만 돌게 못 박는다"}
	if len(gateRivals(mine, []*model.Memory{oneTag})) != 0 {
		t.Fatal("태그가 하나만 겹치는데 C04 가 막는다")
	}
	twin := &model.Memory{ID: "20260823-aaaadddd", Type: model.TypeDecision, Date: "2026-08-23",
		Scope: "aimemorytool", Tags: []string{"gc", "design"},
		Summary: "도구가 스스로 기억을 지우는 길은 만들지 않고 접어 두기만 한다"}
	if len(gateRivals(mine, []*model.Memory{twin})) != 1 {
		t.Fatal("태그 둘이 겹치고 요약도 판박이인데 C04 가 안 막는다")
	}
	// C05(lint 후보)는 넓게 본다 — 좁히는 것은 C04 뿐이다 (결정 39).
	if len(LiveDecisionRivals(mine, []*model.Memory{oneTag}, time.Now(), model.DefaultTypes().Spec(mine.Type))) != 1 {
		t.Fatal("C05 까지 좁아졌다")
	}
}

// TestSupersedeOrderOnly 는 supersede 가 차례만 정하고 파일은 안 고치는지 본다
// (결정 38 · 불변조건 I5).
func TestSupersedeOrderOnly(t *testing.T) {
	mine := &model.Memory{ID: "20260822-1111aaaa", Type: model.TypeDecision, Date: "2026-08-22",
		Scope: "aimemorytool", Tags: []string{"gc"}, Summary: "가"}
	newer := &model.Memory{ID: "20260822-2222bbbb", Type: model.TypeDecision, Date: "2026-08-23",
		Scope: "aimemorytool", Tags: []string{"gc"}, Summary: "나"}
	if got := SupersedeWinner(mine, []*model.Memory{newer}); got != newer.ID {
		t.Fatalf("max(date) 가 이겨야 하는데 %q 가 나왔다", got)
	}
	CheckRepo([]*model.Memory{mine, newer}, wave2dOptions(t))
	if mine.SupersededBy != "" || newer.SupersededBy != "" {
		t.Fatal("도구가 superseded_by 를 스스로 적었다")
	}
}

// TestNounWordsKeepsNouns 는 이름씨 뽑기가 흔한 이름씨를 안 버리는지 본다 (B08).
func TestNounWordsKeepsNouns(t *testing.T) {
	for _, word := range []string{"이미지", "패키지", "메시지", "숫자", "글자", "문서", "순서", "경고"} {
		got := nounWords(word)
		if len(got) == 0 {
			t.Errorf("이름씨 %q 가 풀이말로 잘못 빠졌다", word)
		}
	}
	for _, word := range []string{"올렸다", "만든다", "되었다"} {
		if len(nounWords(word)) != 0 {
			t.Errorf("풀이말 %q 가 이름씨로 셌다", word)
		}
	}
	if len(nounWords("으로")) != 0 {
		t.Error("홀로 선 토씨 「으로」를 이름씨로 셌다")
	}
}

// TestT17NewRulesNoFalseAlarm 은 새 규칙마다 대조군 20건에 거짓 경보가 0인지
// 본다 (시험 계획 T17). 규칙을 더할 때마다 여기 이름을 더한다.
func TestT17NewRulesNoFalseAlarm(t *testing.T) {
	set, report := scoreGoldenSet(t)
	if len(set.Clean) != 20 {
		t.Fatalf("대조군이 20건이 아니라 %d건이다", len(set.Clean))
	}
	fresh := map[string]bool{RuleNoValue: true, RuleNotationDrift: true,
		RuleStaleAge: true, RuleStaleConflictPair: true}
	for _, alarm := range report.FalseAlarms {
		for _, rule := range alarm.Rules {
			if fresh[rule] {
				t.Errorf("T17 미달 : 새 규칙 %s 가 대조군 %s 를 잡았다", rule, alarm.ID)
			}
		}
	}
	// 정밀도 자동 강등도 그대로 탄다 (결정 61 ③).
	for _, one := range report.Rules {
		if !fresh[one.Rule] {
			continue
		}
		meta, _ := Lookup(one.Rule)
		if !meta.Demotable {
			t.Errorf("새 규칙 %s 가 자동 강등을 안 탄다", one.Rule)
		}
	}
}

// TestCatalogCodesUnique 는 규칙 코드가 겹치지 않는지 본다. 새 규칙을 더할 때
// 코드를 베껴 쓰면 강등표와 보고 표가 조용히 어긋난다.
func TestCatalogCodesUnique(t *testing.T) {
	seen := map[string]string{}
	for _, rule := range Catalog {
		if before, ok := seen[rule.Code]; ok {
			t.Errorf("코드 %s 를 %s 와 %s 가 같이 쓴다", rule.Code, before, rule.Name)
		}
		seen[rule.Code] = rule.Name
	}
	if len(Catalog) != wave2dRuleCount {
		t.Fatalf("규칙이 %d개다. 설계·도움말·골든셋 머리말의 수를 같이 고친다", len(Catalog))
	}
}

// wave2dRuleCount 는 지금 규칙 수다. v0.2 57 + 2B 의 LINK 1 + 2D 의 셋 + C14(근거가 죽음).
const wave2dRuleCount = 63
