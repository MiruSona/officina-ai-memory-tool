package quality

import (
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// TestSplitUnits 는 문장·표 줄 자르기다.
func TestSplitUnits(t *testing.T) {
	body := "## 증상\n색인용 토큰은 바이트로 자른다. 질의용은 낱말로 자른다.\n\n| 자리 | 값 |\n| --- | --- |\n| 색인 | 바이그램 |\n짧다\n"
	units := unitsOf("제목이 여기 있다 길게" + "\n" + "요약도 여기 있다 길게 쓴다" + "\n" + body)
	if len(units) == 0 {
		t.Fatal("조각이 하나도 안 나왔다")
	}
	for _, one := range units {
		if len([]rune(normalizeText(one.text))) < minUnitLen {
			t.Errorf("길이 하한을 못 지킨 조각 : %q", one.text)
		}
	}
	// `.` 뒤에 빈칸이 있으면 문장을 자른다.
	found := false
	for _, one := range units {
		if one.text == "색인용 토큰은 바이트로 자른다" {
			found = true
		}
	}
	if !found {
		t.Errorf("마침표에서 안 잘렸다 : %+v", texts(units))
	}
}

// TestSplitSentencesKeepsVersions 는 v0.2·3.14 처럼 붙어 있는 마침표를 안 자르는지다.
func TestSplitSentencesKeepsVersions(t *testing.T) {
	got := splitSentences("v0.2 는 32.5ms 였다. 그래서 고쳤다")
	if len(got) != 2 || got[0] != "v0.2 는 32.5ms 였다" {
		t.Fatalf("판 번호에서 잘렸다 : %q", got)
	}
}

// TestAlignMaxFindsSharedLine 은 긴 글 안에 박힌 같은 한 줄을 찾는지다.
func TestAlignMaxFindsSharedLine(t *testing.T) {
	left := NewDoc(&model.Memory{ID: "a", Summary: "결정 표를 한 자리에 모았다",
		Body: "시험은 홈 폴더를 격리해서 돌린다 늘 그렇게 한다\n다른 이야기가 여기 길게 이어진다"})
	right := NewDoc(&model.Memory{ID: "b", Summary: "시험 환경 정리",
		Body: "아주 다른 이야기가 여기 길게 이어져 나온다\n시험은 홈 폴더를 격리해서 돌린다 늘 그렇게 한다"})
	score, one, two := alignMax(left, right)
	if score < 0.9 {
		t.Fatalf("같은 줄을 못 찾았다 : %.3f (%q / %q)", score, one, two)
	}
	if Score(left, right) > score {
		t.Errorf("통째 점수가 정렬 값보다 높다 — 시험 자료가 부분 중복이 아니다")
	}
}

// TestFactsClash 는 뜻은 가깝고 사실이 다른 쌍을 거르는지다 (2단 · T11).
func TestFactsClash(t *testing.T) {
	cases := []struct {
		left, right string
		clash       bool
	}{
		{"검색이 32.5ms 에서 11.1ms 로 줄었다", "검색이 107.6ms 에서 11.7ms 로 줄었다", true},
		{"검색이 32.5ms 에서 11.1ms 로 줄었다", "검색이 32.5ms 에서 11.1ms 로 줄었다", false},
		{"gc 는 하루 200건까지 돈다", "gc 는 하루 200건까지 돈다고 정했다", false},
		{"파일은 internal/store/files.go 에 있다", "파일은 internal/index/body.go 에 있다", true},
		{"상한을 200 으로 잡았다", "상한을 500 으로 잡았다", true},
	}
	for _, one := range cases {
		if got := factsClash(one.left, one.right); got != one.clash {
			t.Errorf("factsClash(%q, %q) = %v, 바라는 값 %v", one.left, one.right, got, one.clash)
		}
	}
}

func texts(units []unit) []string {
	out := []string{}
	for _, one := range units {
		out = append(out, one.text)
	}
	return out
}
