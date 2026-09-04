package search

import (
	"bytes"
	"math"
	"math/rand"
	"os"
	"path/filepath"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/link"
)

// embedDim 은 시험 표의 차원이다. 진짜 표는 128 이지만 시험은 방향만 보면 된다.
const embedDim = 8

// tableFile 은 시험용 표를 만들어 저장소 폴더 안에 놓는다. 조각마다 방향
// 하나를 주는데, `toward` 에 든 조각은 다 같은 방향을 본다 — 그래야 "이
// 문서가 질의와 뜻이 같다" 는 상황을 만들 수 있다.
func tableFile(t *testing.T, dir string, pieces []string, toward map[string]bool) {
	t.Helper()
	seed := rand.New(rand.NewSource(7))
	same := unitOf([]float64{1, 0, 0, 0, 0, 0, 0, 0})
	vectors := [][]float64{}
	weights := []float32{}
	for _, piece := range pieces {
		if toward[piece] {
			vectors = append(vectors, same)
		} else {
			raw := make([]float64, embedDim)
			// 첫 축은 비워 둔다. 안 그러면 아무 조각이나 같은 방향을 본다.
			for at := 1; at < embedDim; at++ {
				raw[at] = seed.NormFloat64()
			}
			vectors = append(vectors, unitOf(raw))
		}
		weights = append(weights, 1)
	}
	buffer := bytes.Buffer{}
	if err := embed.Write(&buffer, pieces, weights, vectors); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(filepath.Join(dir, embed.DirName), 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, embed.DirName, embed.FileName)
	if err := os.WriteFile(path, buffer.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
	embed.Forget()
	t.Cleanup(embed.Forget)
}

func unitOf(raw []float64) []float64 {
	total := 0.0
	for _, value := range raw {
		total += value * value
	}
	length := math.Sqrt(total)
	out := make([]float64, len(raw))
	for at, value := range raw {
		out[at] = value / length
	}
	return out
}

// corpusPieces 는 시험 말뭉치에 나오는 조각 전부다.
func corpusPieces() []string {
	seen := map[string]bool{}
	out := []string{}
	for _, item := range corpus {
		for _, text := range []string{item.summary, item.body} {
			for _, piece := range embed.Pieces(text) {
				if !seen[piece] {
					seen[piece] = true
					out = append(out, piece)
				}
			}
		}
	}
	return out
}

func askEmbed(t *testing.T, database *index.DB, dir, query string, settings config.EmbedConfig) *Result {
	t.Helper()
	result, err := Search(Options{
		Sources: []Source{{DB: database}}, Query: query, Limit: 10,
		Stopwords: config.DefaultStopwords(),
		Synonym:   map[string][]string{"기억": {"memory"}, "훅": {"hook"}, "색인": {"index"}},
		Embed:     settings, RepoDir: dir,
	})
	if err != nil {
		t.Fatalf("검색이 죽었다 : %v", err)
	}
	return result
}

func idsOf(result *Result) []string {
	out := []string{}
	for _, hit := range result.Hits {
		out = append(out, hit.ID)
	}
	return out
}

// 표가 없으면 켜 놔도 끈 것과 글자 하나까지 같아야 한다 (설계 4-8 우아한 퇴화).
func TestEmbedWithoutTableIsIdentical(t *testing.T) {
	embed.Forget()
	defer embed.Forget()
	database, _ := newRepo(t)
	empty := t.TempDir()
	for _, query := range []string{"성능 색인", "훅 예산", "바이그램 검색", "기억 저장소"} {
		off := askEmbed(t, database, empty, query, config.EmbedConfig{})
		on := askEmbed(t, database, empty, query, config.EmbedConfig{
			Enabled: true, Path: filepath.Join(empty, "없는표.bin"), Floor: 0.35, RRFWeight: 0.6})
		sameHits(t, query, off, on)
	}
}

// 껐을 때도 같아야 한다 — 표가 있어도 `enabled = false` 면 아무 일도 없다.
func TestEmbedDisabledIsIdentical(t *testing.T) {
	database, _ := newRepo(t)
	dir := t.TempDir()
	tableFile(t, dir, corpusPieces(), nil)
	for _, query := range []string{"성능 색인", "훅 예산", "결정 충돌"} {
		off := askEmbed(t, database, dir, query, config.EmbedConfig{})
		on := askEmbed(t, database, dir, query, config.EmbedConfig{
			Enabled: false, Floor: 0.35, RRFWeight: 0.6})
		sameHits(t, query, off, on)
	}
}

func sameHits(t *testing.T, query string, off, on *Result) {
	t.Helper()
	if len(off.Hits) != len(on.Hits) {
		t.Fatalf("%q : 건수가 %d → %d 로 바뀌었다", query, len(off.Hits), len(on.Hits))
	}
	for at := range off.Hits {
		// 점수에는 최근성 감쇠가 들어 있어 두 번 부르는 사이 시계가 흐른
		// 만큼 아주 조금 다르다. 여섯째 자리까지 같으면 같은 답이다 —
		// 1e-9 로 잡았더니 시계 드리프트만으로 깜빡였다 (뒷정리 확인).
		// 임베딩이 실제로 끼어들면 차이가 1e-2 대라 이 자로도 다 걸린다.
		if off.Hits[at].ID != on.Hits[at].ID ||
			math.Abs(off.Hits[at].Score-on.Hits[at].Score) > 1e-6 {
			t.Fatalf("%q : %d번째가 %s(%.6f) → %s(%.6f) 로 바뀌었다", query, at+1,
				off.Hits[at].ID, off.Hits[at].Score, on.Hits[at].ID, on.Hits[at].Score)
		}
	}
}

// **임베딩은 후보를 새로 데려오지 않는다.** 나온 기억의 집합이 그대로여야
// abstain 관문이 낱말 쪽 결과로만 판정된다 (설계 4-5 · G2).
func TestEmbedNeverAddsCandidates(t *testing.T) {
	database, _ := newRepo(t)
	dir := t.TempDir()
	tableFile(t, dir, corpusPieces(), nil)
	for _, query := range []string{"성능 색인", "훅 예산", "결정 충돌", "기억 저장소"} {
		off := askEmbed(t, database, dir, query, config.EmbedConfig{})
		on := askEmbed(t, database, dir, query, config.EmbedConfig{
			Enabled: true, Floor: 0, RRFWeight: 5})
		if len(idsOf(off)) != len(idsOf(on)) {
			t.Fatalf("%q : 후보가 %v → %v 로 바뀌었다", query, idsOf(off), idsOf(on))
		}
		have := map[string]bool{}
		for _, id := range idsOf(off) {
			have[id] = true
		}
		for _, id := range idsOf(on) {
			if !have[id] {
				t.Fatalf("%q : 임베딩이 없던 %s 를 데려왔다", query, id)
			}
		}
	}
}

// G2 — 저장소에 없는 것을 물으면 임베딩을 켜도 strict 답이 없어야 한다.
func TestEmbedKeepsAbstain(t *testing.T) {
	database, _ := newRepo(t)
	dir := t.TempDir()
	tableFile(t, dir, corpusPieces(), nil)
	for _, query := range []string{"물리엔진 중력", "결제모듈", "블렌더 리깅", "파티클 이펙트"} {
		result := askEmbed(t, database, dir, query, config.EmbedConfig{
			Enabled: true, Floor: 0, RRFWeight: 5})
		for _, hit := range result.Hits {
			if Strict(hit.Rung) {
				t.Fatalf("%q 에 strict 답이 났다 : %s (칸 %d)", query, hit.ID, hit.Rung)
			}
		}
	}
}

// 닮음 문턱을 넘길 수 없게 올리면 임베딩 표가 통째로 빈다 = 끈 것과 같다.
func TestEmbedFloorDropsEverything(t *testing.T) {
	database, _ := newRepo(t)
	dir := t.TempDir()
	tableFile(t, dir, corpusPieces(), nil)
	for _, query := range []string{"성능 색인", "훅 예산"} {
		off := askEmbed(t, database, dir, query, config.EmbedConfig{})
		on := askEmbed(t, database, dir, query, config.EmbedConfig{
			Enabled: true, Floor: 1.5, RRFWeight: 5})
		sameHits(t, query, off, on)
	}
}

// 뜻이 같다고 표가 말하면 차례가 올라간다. 표에서 「질의와 같은 방향」 으로
// 못박은 조각만 가진 기억이 위로 와야 임베딩이 제 일을 한 것이다.
func TestEmbedLiftsCloseMeaning(t *testing.T) {
	database, _ := newRepo(t)
	dir := t.TempDir()
	query := "성능 색인"
	off := askEmbed(t, database, dir, query, config.EmbedConfig{})
	if len(off.Hits) < 3 {
		t.Fatalf("고를 후보가 %d 건뿐이다", len(off.Hits))
	}
	// MMR 이 깎는 자리는 피한다. 「뜻이 가깝다」와 「앞에 선 것과 겹친다」는
	// 서로 반대로 미는 힘이라, 둘이 같은 기억에 걸리면 이 시험은 임베딩이 아니라
	// 그 줄다리기를 재게 된다 (결정 44 MMR · 2B).
	last := lastUnpenalized(t, off.Hits)
	toward := map[string]bool{}
	for _, piece := range embed.Pieces(query) {
		toward[piece] = true
	}
	for _, item := range corpus {
		if item.summary != last.Summary {
			continue
		}
		for _, piece := range embed.Pieces(item.summary + " " + item.body) {
			toward[piece] = true
		}
	}
	tableFile(t, dir, corpusPieces(), toward)
	on := askEmbed(t, database, dir, query, config.EmbedConfig{
		Enabled: true, Floor: 0.2, RRFWeight: 5})
	before, after := rankOfID(off, last.ID), rankOfID(on, last.ID)
	if after == 0 || after >= before {
		t.Fatalf("뜻이 같다고 했는데 차례가 %d → %d 다", before, after)
	}
}

// lastUnpenalized 는 MMR 벌점을 안 받는 답 중 가장 아래 것이다.
func lastUnpenalized(t *testing.T, hits []Hit) Hit {
	t.Helper()
	for at := len(hits) - 1; at >= 1; at-- {
		worst := 0.0
		for before := 0; before < at; before++ {
			if same := link.Similarity(docOf(hits[at]), docOf(hits[before])); same > worst {
				worst = same
			}
		}
		if worst < mmrFloor {
			return hits[at]
		}
	}
	t.Fatal("MMR 벌점을 안 받는 답이 없다. 시험 말뭉치를 고쳐야 한다")
	return Hit{}
}
