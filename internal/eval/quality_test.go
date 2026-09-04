package eval

import (
	"strings"
	"testing"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
)

// goldenQuality 는 파도 F 가 만든 품질 골든셋 132건이다.
const goldenQuality = "../../testdata/golden/quality.yaml"

func qualityOptions() QualityOptions {
	return QualityOptions{Golden: goldenQuality, Store: "../../testdata/quality/store",
		Config: config.Default("aimemorytool"), Now: time.Date(2026, 8, 23, 12, 0, 0, 0, time.Local)}
}

// TestQualityGoldenRuns 는 골든셋 + 저장소를 실제로 채점한다. 표를 눈으로
// 보려고 로그로 찍는다 — 이 표가 G6 판정의 근거다 (설계 3-6).
func TestQualityGoldenRuns(t *testing.T) {
	report, err := RunQuality(qualityOptions())
	if err != nil {
		t.Fatal(err)
	}
	if report.Flag != 112 || report.Clean != 20 {
		t.Fatalf("골든셋 건수가 112/20 이 아니다 : %d/%d", report.Flag, report.Clean)
	}
	if report.Memories < 120 {
		t.Fatalf("저장소를 %d건밖에 못 읽었다", report.Memories)
	}
	t.Log("\n" + QualityMarkdown(report))
}

// TestQualityGatesSayUnmeasured 는 안 잰 합격선을 합격으로 안 찍는지 본다
// (설계 8-1 신뢰도 · 1절 「하나라도 미달이면 미달이라고 적는다」).
func TestQualityGatesSayUnmeasured(t *testing.T) {
	table := GateTable(AllGates(nil, nil))
	for _, key := range []string{"G1", "G2", "G3", "G4", "G5", "G6"} {
		if !strings.Contains(table, key) {
			t.Fatalf("%s 줄이 없다", key)
		}
	}
	if strings.Count(table, "안 잼") != 6 {
		t.Fatalf("아무것도 안 쟀는데 「안 잼」이 6줄이 아니다 :\n%s", table)
	}
}

// TestTuneDuplicateCurve 는 문턱 곡선이 실제로 그려지고 권장값이 나오는지 본다.
func TestTuneDuplicateCurve(t *testing.T) {
	report, err := RunTune(qualityOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Points) == 0 {
		t.Fatal("곡선이 비었다")
	}
	if report.Reject <= 0 || report.Warn <= 0 || report.Warn > report.Reject {
		t.Fatalf("권장값이 이상하다 : reject %.2f · warn %.2f", report.Reject, report.Warn)
	}
	markdown := TuneMarkdown(report)
	if n := strings.Count(markdown, "권장 :"); n != 1 {
		t.Fatalf("N3 : 권장 줄이 한 번이어야 하는데 %d번 찍혔다 :\n%s", n, markdown)
	}
	t.Log("\n" + markdown)
}

// TestLoadGoldenV2 는 파도 F 가 만든 80건 검색 골든셋이 새 칸(variant·
// expect_mode)째로 읽히는지 본다. 수치는 검색이 재고, 여기서는 자만 본다.
func TestLoadGoldenV2(t *testing.T) {
	set, err := Load("../../testdata/golden/goldenset-v2.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if len(set.Cases) != 80 {
		t.Fatalf("80건이 아니라 %d건이다", len(set.Cases))
	}
	if len(set.Warnings) > 0 {
		t.Fatalf("구성 경고가 남았다 : %v", set.Warnings)
	}
	multi, all, english := 0, 0, 0
	for _, item := range set.Cases {
		if item.Kind == KindMultisession {
			multi++
			if item.ExpectModeOf() == ExpectAll {
				all++
			}
		}
		if item.LangOf() == "en" {
			english++
		}
	}
	if multi != all {
		t.Fatalf("multisession %d건 중 %d건만 expect_mode: all 이다", multi, all)
	}
	if english < minEnglish {
		t.Fatalf("lang: en 이 %d건뿐이다 (최소 %d)", english, minEnglish)
	}
}
