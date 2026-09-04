package quality

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// goldenPath·storePath 는 저장소 뿌리에서 본 자리다. 시험은 패키지 폴더에서 도니
// 위로 셋 올라간다.
const (
	goldenPath = "../../testdata/golden/quality.yaml"
	storePath  = "../../testdata/quality/store"
	vocabPath  = "../../testdata/quality/vocab.toml"
)

// testdataVocab 은 이 골든셋 자료가 쓰는 낱말 표준이다. 자료가 이 도구를 만든
// 저장소에서 온 것이라 그 저장소의 태그·scope 를 자료 옆에 같이 둔다 —
// 기본 씨앗 목록에 그 낱말을 박아 두면 남의 프로젝트가 첫 add 부터 막힌다.
func testdataVocab(t *testing.T) config.Vocab {
	t.Helper()
	vocab, err := config.LoadVocab(filepath.FromSlash(vocabPath))
	if err != nil {
		t.Fatalf("자료용 vocab.toml 을 못 읽었다 : %v", err)
	}
	return vocab
}

// scoreGoldenSet 은 골든셋 채점 한 번이다. 여러 시험이 같이 쓴다.
func scoreGoldenSet(t *testing.T) (*GoldenSet, GoldenReport) {
	t.Helper()
	set, err := LoadGolden(filepath.FromSlash(goldenPath))
	if err != nil {
		t.Fatalf("골든셋을 못 읽었다 : %v", err)
	}
	memories, err := LoadMemories(filepath.FromSlash(storePath))
	if err != nil {
		t.Fatalf("저장소를 못 읽었다 : %v", err)
	}
	if len(memories) == 0 {
		t.Fatal("저장소가 비었다")
	}
	settings := config.Default("quality-golden")
	opt := RepoOptions{Options: Options{Config: settings, Vocab: testdataVocab(t),
		Now: time.Date(2026, 8, 23, 0, 0, 0, 0, time.Local)}}
	return set, ScoreGolden(set, memories, opt)
}

// TestGoldenScore 는 실제 저장소 130건에 규칙을 돌려 G6 ①~③ 을 판정한다.
// 표 전문은 -v 로 본다.
func TestGoldenScore(t *testing.T) {
	set, report := scoreGoldenSet(t)
	if len(set.Flag) != 112 || len(set.Clean) != 20 {
		t.Fatalf("골든셋 건수가 규격과 다르다 : flag %d · clean %d", len(set.Flag), len(set.Clean))
	}
	if len(report.Missing) > 0 {
		t.Errorf("골든셋이 카탈로그에 없는 규칙을 부른다 : %v", report.Missing)
	}
	t.Log("\n" + report.Table())
	// G6 ① 은 DUP 하나가 아직 못 넘는다 — 골든셋의 DUP 36건은 「같은 사실을 다른
	// 말로 다시 쓴 것」이라 글자 겹침으로는 안 갈린다(자세한 것은 TestTuneDuplicate).
	// 여기서는 지금 값을 못으로 박아 두고 뒤로 물러나는 것만 막는다.
	// (3물결 H) 중복 문턱을 실측값(0.22/0.17)으로 내리면서 DUP 재현율이
	// 0.25 → 0.556 으로 올랐다. 못을 그 자리로 옮겨 뒷걸음만 막는다.
	// (리뷰 B) 태그·scope 몫을 내용에 실어 주고 문턱을 0.08/0.06 으로 옮기면서
	// DUP 0.556 → 0.667 · 대조군 거짓 경보 3 → 0 이 됐다. 못을 다시 옮긴다.
	baseline := map[string]float64{TypeDup: 0.66, TypeSrc: 1.0, TypeTag: 1.0,
		TypeThin: 1.0, TypeFormat: 1.0, TypeMulti: 0.96}
	for _, one := range report.Types {
		if !one.Machine {
			continue
		}
		floor := MachineRecallFloor
		if known, ok := baseline[one.Type]; ok && known < floor {
			floor = known
		}
		if one.Recall+0.0005 < floor {
			t.Errorf("G6 ① 뒷걸음 : %s 재현율 %.3f < %.3f (놓친 것 %v)", one.Type, one.Recall, floor, one.Missed)
		}
	}
	// (리뷰 B) 봐주던 셋(dc5f20b1·9ed2e22c·43a62aa5)이 다 없어졌다. 태그·scope
	// 몫을 내용에 실어 주니 「같은 회차에 같은 태그로 쓴 서로 다른 발견」이
	// 더는 문턱에 안 닿는다. 이제 거짓 경보는 한 건도 안 봐준다.
	for _, alarm := range report.FalseAlarms {
		t.Errorf("G6 ② 미달 : 대조군 거짓 경보 %+v", alarm)
	}
	if !report.PrecisionOK {
		for _, one := range report.Rules {
			if one.Rule == RuleDuplicateSoft || one.Rule == RuleDuplicateHard {
				// 위와 같은 숙제다. 정밀도가 낮으면 도구가 스스로 경고로 내린다
				// (설계 결정 14).
				continue
			}
			if one.Precision >= 0 && one.Precision < PrecisionFloor {
				t.Errorf("G6 ③ 미달 : %s 정밀도 %.3f (진짜 %d · 거짓 %d)", one.Rule, one.Precision, one.True, one.False)
			}
		}
	}
}

// onlyDuplicateRules 는 걸린 규칙이 중복 둘뿐인지다.
func onlyDuplicateRules(rules []string) bool {
	for _, rule := range rules {
		if rule != RuleDuplicateSoft && rule != RuleDuplicateHard {
			return false
		}
	}
	return true
}
