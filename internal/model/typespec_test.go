package model

import (
	"strings"
	"testing"
)

// factMemory 는 규격을 다 지킨 fact 한 건이다.
func factMemory() Memory {
	return Memory{ID: "20260830-1a2b3c4d", Type: TypeFact, Date: "2026-08-30",
		Title: "집 인터넷 회선", Summary: strings.Repeat("가", 40),
		Tags: []string{"net", "tool"}, Scope: "officina", Author: "human:mirusona",
		Sources: []string{"note:KT 가입정보 08-30"}, Spec: SpecV2}
}

// fact 는 근거가 있어야 하고, severity·todo_status 는 못 붙인다.
func TestFactValidate(t *testing.T) {
	good := factMemory()
	if problems := Validate(&good); len(problems) > 0 {
		t.Fatalf("멀쩡한 fact 가 걸렸다 : %v", problems)
	}
	noSource := factMemory()
	noSource.Sources = nil
	if len(Validate(&noSource)) == 0 {
		t.Fatal("근거 없는 fact 가 통과했다")
	}
	withSeverity := factMemory()
	withSeverity.Severity = SeverityHigh
	if len(Validate(&withSeverity)) == 0 {
		t.Fatal("fact 에 severity 를 붙였는데 통과했다")
	}
	withStatus := factMemory()
	withStatus.TodoStatus = StatusOpen
	if len(Validate(&withStatus)) == 0 {
		t.Fatal("fact 에 todo_status 를 붙였는데 통과했다")
	}
}

// ValidateWith 는 표에 있는 종류만 받는다. 표를 늘리면 그 종류도 통과한다.
func TestValidateWithTable(t *testing.T) {
	one := factMemory()
	one.Type = "env"
	if len(Validate(&one)) == 0 {
		t.Fatal("기본표 밖 종류가 기본 자를 통과했다")
	}
	table := append(DefaultTypes(), TypeSpec{Name: "env", Label: "환경", Sources: SourcesRequired,
		Body: BodyFree, HalfLife: HalfLifeNone})
	if problems := ValidateWith(&one, one.Spec, table); len(problems) > 0 {
		t.Fatalf("표에 넣은 종류가 걸렸다 : %v", problems)
	}
}

// 회귀 못박기 — 기본표 7종의 값이 v0.4 가 코드에 박아 두었던 것 그대로다.
// 이 표가 바뀌면 이미 저장된 기억의 등급·감쇠·gc 가 통째로 달라진다.
func TestDefaultTypesFrozen(t *testing.T) {
	want := map[string]TypeSpec{
		TypeTodo: {Name: TypeTodo, Label: "할 일", Sources: SourcesNone, Body: BodyFree,
			HalfLife: HalfLifeNone, Hook: HookOpen, TodoStatus: true},
		TypeHistory: {Name: TypeHistory, Label: "기록", Sources: SourcesWarn, Body: BodyFree,
			HalfLife: HalfLifeHistory, Evidence: true, StaleDays: 180},
		TypeIssue: {Name: TypeIssue, Label: "이슈", Sources: SourcesRequired, Body: BodyIssueSections,
			HalfLife: HalfLifeNone, Hook: HookOpen, Severity: true, Evidence: true,
			GCKeep: true, SearchBonus: true},
		TypeCaution: {Name: TypeCaution, Label: "주의", Sources: SourcesRequired, Body: BodyFree,
			HalfLife: HalfLifeNone, Hook: HookOpen, Severity: true, GCKeep: true},
		TypeDecision: {Name: TypeDecision, Label: "결정", Sources: SourcesRequired, Body: BodyFree,
			HalfLife: HalfLifeDecision, Hook: HookDecision, Evidence: true, Conclusion: true,
			OneThing: true, Gate: true, GCKeep: true, SearchBonus: true, StaleDays: 1440},
		TypeHowto: {Name: TypeHowto, Label: "하는 법", Sources: SourcesNone, Body: BodyNumbered,
			HalfLife: HalfLifeDecision, Evidence: true, GCKeep: true, StaleDays: 1440},
		TypeFact: {Name: TypeFact, Label: "환경 사실", Sources: SourcesRequired, Body: BodyFree,
			HalfLife: HalfLifeNone, Hook: HookFact, OneThing: true, GCKeep: true, StaleDays: 360},
	}
	table := DefaultTypes()
	if len(table) != len(want) {
		t.Fatalf("기본표가 %d종이다 (7종이어야 한다)", len(table))
	}
	for _, spec := range table {
		if spec != want[spec.Name] {
			t.Errorf("%s 의 취급이 달라졌다 :\n지금 %+v\n원래 %+v", spec.Name, spec, want[spec.Name])
		}
	}
	if got := strings.Join(table.Names(), " "); got != "todo history issue caution decision howto fact" {
		t.Fatalf("종류 차례가 달라졌다 : %s", got)
	}
}

// 훅 절 차례는 최근 결정 · 열린 이슈·할 일 · 환경 이고, 표에만 있는 새 절은 뒤에 붙는다.
func TestHookSectionOrder(t *testing.T) {
	got := strings.Join(DefaultTypes().HookSections(), " / ")
	if got != HookDecision+" / "+HookOpen+" / "+HookFact {
		t.Fatalf("절 차례가 다르다 : %s", got)
	}
	table := append(DefaultTypes(), TypeSpec{Name: "env", Hook: "장비"})
	sections := table.HookSections()
	if sections[len(sections)-1] != "장비" {
		t.Fatalf("새 절이 맨 뒤가 아니다 : %v", sections)
	}
}
