package llm

import (
	"strings"
	"testing"
)

// 고정 10쌍 — 손으로 쓴 일반 문장이다 (mem 본문 아님). 설계 2절 「시험」 표.
func TestRuleJudgeFixedPairs(t *testing.T) {
	cases := []struct {
		name     string
		evidence string
		claim    string
		label    Label
		reason   string
	}{
		{"부정 한다", "새 기억을 넣을 때 중복 검사를 한다", "새 기억을 넣을 때 중복 검사를 안 한다",
			LetterContradict, "neg:한다→안 한다"},
		{"부정 기본 켬", "로그 파일은 기본 켬 상태로 시작", "로그 파일은 기본 끔 상태로 시작",
			LetterContradict, "neg:기본 켬→기본 끔"},
		{"숫자 초", "첫 응답은 평균 5초 안에 온다", "첫 응답은 평균 12초 안에 온다",
			LetterContradict, "num:5초≠12초"},
		{"숫자 단위 바꿈", "색인 크기는 2GB 까지 큰다", "색인 크기는 8000MB 까지 큰다",
			LetterContradict, "num:2GB≠8000MB"},
		{"무관 1", "고양이는 햇볕 아래에서 낮잠을 즐긴다", "서버 배포는 금요일 오후에 멈춘다",
			LetterUnrelated, "unrelated:"},
		{"무관 2", "Rust 컴파일러는 빌림 검사기를 갖췄다", "주말 등산 모임이 산 정상에서 끝났다",
			LetterUnrelated, "unrelated:"},
		{"반올림 지지", "응답은 평균 1500ms 걸린다", "응답은 평균 2초 걸린다", "", ""},
		{"단위 바꾼 지지", "모델 파일은 3GB 를 차지한다", "모델 파일은 3000MB 를 차지한다", "", ""},
		{"숫자 더한 지지", "검사는 매일 밤에 돌린다", "검사는 매일 밤에 돌리고 30분 걸린다", "", ""},
		{"일부만 받친 지지", "색인은 SQLite FTS5 로 만든다",
			"색인은 SQLite FTS5 로 만들고 검색 결과는 점수 순으로 보여 준다", "", ""},
	}
	for _, one := range cases {
		label, reason, ok := RuleJudge(one.evidence, one.claim)
		if one.label == "" {
			if ok {
				t.Errorf("%s : 안 걸려야 한다 : %s %s", one.name, label, reason)
			}
			continue
		}
		if !ok || label != one.label || !strings.HasPrefix(reason, one.reason) {
			t.Errorf("%s : %s %q 여야 한다 : %v %s %q", one.name, one.label, one.reason, ok, label, reason)
		}
	}
}

// 부정 짝 표 — 짝마다 양쪽(긍정→뒤집음 · 뒤집음→긍정)이 다 걸린다.
func TestRuleJudgeNegationTable(t *testing.T) {
	for _, pair := range negations {
		for _, plain := range pair.Plain {
			forward, _, ok := RuleJudge("설정 값을 "+plain, "설정 값을 "+pair.Flipped)
			if !ok || forward != LetterContradict {
				t.Errorf("%s→%s 가 안 걸렸다", plain, pair.Flipped)
			}
			_, reason, ok := RuleJudge("설정 값을 "+pair.Flipped, "설정 값을 "+plain)
			if !ok || reason != "neg:"+pair.Flipped+"→"+plain {
				t.Errorf("%s→%s 가 안 걸렸다 : %q", pair.Flipped, plain, reason)
			}
		}
	}
}

// 부정 짝이 있어도 자리가 다르거나, 주장에 긍정 꼴이 남아 있으면 안 걸린다.
func TestRuleJudgeNegationNeedsSamePlace(t *testing.T) {
	if label, reason, ok := RuleJudge("중복 검사를 한다. 저장도 빠르다", "중복 검사와 별개로 배포는 안 한다"); ok {
		t.Errorf("자리가 다르면 안 걸려야 한다 : %s %s", label, reason)
	}
	if label, reason, ok := RuleJudge("중복 검사를 한다", "중복 검사를 안 한다. 색인은 다시 한다"); ok {
		t.Errorf("주장에 긍정 꼴이 남았으면 안 걸려야 한다 : %s %s", label, reason)
	}
}

// 단위 표 — 단위마다 다른 값은 걸리고, 같은 무리 안에서 바꿔 맞춘 같은 값은 안 걸린다.
func TestRuleJudgeUnitTable(t *testing.T) {
	for _, one := range units {
		name := one.Name
		if name == "gb" || name == "mb" || name == "kb" {
			name = strings.ToUpper(name)
		}
		label, reason, ok := RuleJudge("값은 10"+name+" 이다", "값은 30"+name+" 이다")
		if !ok || label != LetterContradict || reason != "num:10"+name+"≠30"+name {
			t.Errorf("%s : 다른 값이 안 걸렸다 : %v %s %q", name, ok, label, reason)
		}
	}
	same := [][2]string{
		{"1초", "1000ms"}, {"2s", "2000ms"}, {"1분", "60초"}, {"1시간", "60분"},
		{"1GB", "1000MB"}, {"1MB", "1000KB"},
	}
	for _, pair := range same {
		if label, reason, ok := RuleJudge("값은 "+pair[0]+" 이다", "값은 "+pair[1]+" 이다"); ok {
			t.Errorf("%s = %s 인데 걸렸다 : %s %s", pair[0], pair[1], label, reason)
		}
	}
}

// 단위 없는 숫자 · 날짜·판·범위 꼴 숫자는 R-숫자에서 뺀다.
func TestRuleJudgeIgnoresPlainAndJoinedNumbers(t *testing.T) {
	for _, pair := range [][2]string{
		{"값은 10 이다", "값은 30 이다"},
		{"값은 4~5배 이다", "값은 7~8배 이다"},
		{"판은 1.2 이다", "판은 1.4 이다"},
		{"모델 e422148 을 쓴다", "모델 e999999 을 쓴다"},
	} {
		if label, reason, ok := RuleJudge(pair[0], pair[1]); ok {
			t.Errorf("%q / %q 가 걸렸다 : %s %s", pair[0], pair[1], label, reason)
		}
	}
}

// 같은 단위 숫자가 양쪽에 있으면 겹침이 없어도 무관으로 치지 않는다.
func TestRuleJudgeUnrelatedNeedsNoSharedUnit(t *testing.T) {
	if label, reason, ok := RuleJudge("고양이 사료는 3개 남았다", "서버 디스크 5개 교체 완료"); ok {
		t.Errorf("같은 단위 숫자가 있는데 걸렸다 : %s %s", label, reason)
	}
}

// 「같은 자리」는 앞 2낱말만 본다 — 뒤 낱말만 같은 숫자는 안 걸린다. 단위 「종」은 본다.
func TestRuleJudgePlaceLooksBeforeOnly(t *testing.T) {
	if label, reason, ok := RuleJudge("② 찍어 내기 — 6종 기억으로 바꿔 쓴다", "사실을 뽑아 12종 기억으로 찍어 낸다"); ok {
		t.Errorf("뒤 낱말만 같으면 안 걸려야 한다 : %s %s", label, reason)
	}
	label, reason, ok := RuleJudge("기본 7종은 지울 수 없다", "기본 14종은 지울 수 없다")
	if !ok || label != LetterContradict || reason != "num:7종≠14종" {
		t.Errorf("앞 낱말이 같은 「종」 숫자가 안 걸렸다 : %v %s %q", ok, label, reason)
	}
}

// 세 자리 쉼표 묶음은 한 숫자다 — 172 조각으로 읽지 않는다.
func TestRuleJudgeCommaGroupedNumbers(t *testing.T) {
	for _, claim := range []string{"한글은 11172자 라 크다", "한글은 11,200자 라 크다"} {
		if label, reason, ok := RuleJudge("한글은 11,172자 라 크다", claim); ok {
			t.Errorf("%q 는 같은 숫자인데 걸렸다 : %s %s", claim, label, reason)
		}
	}
	label, reason, ok := RuleJudge("한글은 11,172자 라 크다", "한글은 20,000자 라 크다")
	if !ok || reason != "num:11,172자≠20,000자" {
		t.Errorf("다른 숫자가 안 걸렸다 : %v %s %q", ok, label, reason)
	}
	if label, reason, ok := RuleJudge("항목 3,5,7개 를 돌렸다", "항목 9개 를 돌렸다"); ok {
		t.Errorf("쉼표 나열은 숫자로 안 읽는다 : %s %s", label, reason)
	}
}

// 붙여 쓴 부정 꼴(안한다·안된다)은 띄어 쓴 꼴과 같다. 낱말 안의 「안」(제안한다)은 부정이 아니다.
func TestRuleJudgeGluedNegation(t *testing.T) {
	for _, pair := range [][2]string{
		{"중복 검사를 안한다", "중복 검사를 안 한다"},
		{"중복 검사를 안 된다", "중복 검사를 안된다"},
		{"기능을 제안한다", "기능을 제안한다고 적었다"},
	} {
		if label, reason, ok := RuleJudge(pair[0], pair[1]); ok {
			t.Errorf("%q / %q 는 같은 뜻인데 걸렸다 : %s %s", pair[0], pair[1], label, reason)
		}
	}
	label, reason, ok := RuleJudge("중복 검사를 한다", "중복 검사를 안한다")
	if !ok || label != LetterContradict || reason != "neg:한다→안 한다" {
		t.Errorf("붙여 쓴 부정이 안 걸렸다 : %v %s %q", ok, label, reason)
	}
}

// 근거나 주장이 비면(바이그램 없음) 무관으로 잡지 않는다.
func TestRuleJudgeEmptySideIsNotUnrelated(t *testing.T) {
	for _, pair := range [][2]string{{"", "서버 배포는 금요일 오후에 멈춘다"}, {"서버 배포는 금요일 오후에 멈춘다", " "}} {
		if label, reason, ok := RuleJudge(pair[0], pair[1]); ok {
			t.Errorf("%q / %q 가 걸렸다 : %s %s", pair[0], pair[1], label, reason)
		}
	}
}

// 반올림 허용은 같은 자릿수 안에서 끝 유효숫자 자리까지만이다 — 둥근 수(100·10)가 반을 삼키지 않는다.
func TestSameNumberRounding(t *testing.T) {
	cases := []struct {
		a, b float64
		same bool
	}{
		{100, 50, false}, {10, 5, false}, {1000, 500, false}, {20, 25, false},
		{11172, 11200, true}, {1.46, 1.5, true}, {26.6, 27, true}, {1500, 2000, true},
		{1000, 1000, true}, {0, 5, false}, {95, 100, true},
	}
	for _, one := range cases {
		if got := sameNumber(one.a, one.b); got != one.same {
			t.Errorf("sameNumber(%v, %v) = %v, %v 여야 한다", one.a, one.b, got, one.same)
		}
		if got := sameNumber(one.b, one.a); got != one.same {
			t.Errorf("sameNumber(%v, %v) = %v, %v 여야 한다 (뒤집음)", one.b, one.a, got, one.same)
		}
	}
	label, reason, ok := RuleJudge("통과율은 100% 이다", "통과율은 50% 이다")
	if !ok || label != LetterContradict || reason != "num:100%≠50%" {
		t.Errorf("100%% ↔ 50%% 가 안 걸렸다 : %v %s %q", ok, label, reason)
	}
}
