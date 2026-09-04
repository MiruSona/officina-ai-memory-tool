package embed

// 문서 벡터 만들기 (결정 12·13).
//
// `index` 가 끝나면 여기로 온다. **바뀐 건만 다시 계산한다** — 2만 건을 매번
// 다시 추론하면 13분이 매번 든다. 모델이 바뀌면(머리말 불일치) 그때만 통째로
// 다시 만든다.
//
// **무엇이 바뀌었나는 색인 회차가 아니라 열쇠로 안다** (리뷰 B · V5). 열쇠는
// 기억 파일 해시 + 자르는 자다. `index --full` 이 `index.db` 를 통째로 버려도
// 벡터는 열쇠가 같으면 그대로 산다.
//
// **글은 정말 다시 계산할 건만 읽는다** (리뷰 B · V1·V15). 옛 판은 모델이
// 있는지 보기도 전에 기억 2만 개를 통째로 읽어 파싱했다 — 그것이 증분 색인
// 0.2초를 2.3~5.8초로, 최대 메모리를 1GB로 만들었다.
//
// 모델이 없으면 아무 일도 안 하고 조용히 돌아간다. 벡터 파일이 없으면 검색이
// 낱말 모드로 답할 뿐이다 (결정 16).

import (
	"hash/fnv"
	"os"
	"strconv"
	"strings"
	"time"
)

// envInt 는 시험용 뒷문이다. 값이 없거나 이상하면 기본값이다.
func envInt(name string, fallback int) int {
	value, err := strconv.Atoi(os.Getenv(name))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}

// Item 은 벡터를 만들 기억 하나다. Text 는 **정말 다시 계산할 때만** 읽는다.
type Item struct {
	ID string
	// Hash 는 기억 파일 내용 해시다 (`index` 의 files.hash). 비면 늘 다시 만든다.
	Hash string
	// Load 는 임베딩에 넣을 글을 읽어 오는 함수다. nil 이면 Text 를 쓴다.
	Load func() string
	Text string
}

// BuildReport 는 한 번 만든 결과다.
type BuildReport struct {
	// Skipped 가 참이면 모델이 없어 아무 일도 안 했다.
	Skipped bool
	// Rebuilt 가 참이면 머리말이 안 맞아 통째로 다시 만들었다.
	Rebuilt  bool
	Made     int
	Reused   int
	Dropped  int
	Total    int
	Bytes    int64
	Elapsed  time.Duration
	ModelKey string
	// Failed 는 토크나이저가 죽어 벡터 없이 지나간 건 수다 (H-1).
	Failed int
}

// MaxText 는 한 기억에서 볼 글자 수다. 기억 하나의 뜻은 앞머리에 다 있고,
// 길이가 두 배면 추론 시간도 두 배다.
//
// **(리뷰 B · V4) 600 그대로 둔다.** 320 으로 줄여 봤더니 20k 시간은 줄지만
// 실기억 골든셋 v2 80건 r@5 가 0.761 → 0.746 으로 내려갔다. 시간은 글자가
// 아니라 **토큰 상한**(maxTokens)에서 뗐다 — 그쪽은 128 로 내려도 0.761 이
// 그대로다 (History 코드리뷰-B 문서 V4 표).
// **재기용 뒷문**은 `MEM_EMBED_MAXTEXT` 다. 평소에는 아무도 안 준다 —
// 이 값을 재려고 벡터를 두 벌 만들 때만 쓴다 (MEM_DUP_BETA 와 같은 꼴).
var MaxText = envInt("MEM_EMBED_MAXTEXT", 600)

// buildChunk 는 한 번에 메모리에 올릴 글 수다. 2만 건 글을 통째로 들면 그것만
// 수백 MB 다 (V15). 배치(BatchSize)보다 충분히 커야 길이순 정렬이 값을 한다.
const buildChunk = 2048

// recipe 는 「어떻게 잘라 넣었나」다. 이 자가 바뀌면 옛 벡터를 못 쓴다.
func recipe() string {
	return strconv.Itoa(MaxText) + "/" + strconv.Itoa(maxTokens)
}

// keyOf 는 벡터 한 건의 열쇠다 — 기억 파일 해시 + 자르는 자.
func keyOf(hash string) uint64 {
	if hash == "" {
		return 0
	}
	sum := fnv.New64a()
	sum.Write([]byte(hash))
	sum.Write([]byte("|"))
	sum.Write([]byte(recipe()))
	return sum.Sum64()
}

// Build 는 벡터 파일을 최신으로 맞춘다. 열쇠가 같은 건은 그대로 쓴다.
func Build(repoDir, modelName string, items []Item) (BuildReport, error) {
	report := BuildReport{Total: len(items)}
	started := time.Now()
	path := VectorPath(repoDir)
	if path == "" {
		report.Skipped = true
		return report, nil
	}
	if modelName == "" {
		modelName = DefaultModel
	}
	if !Ready(modelName) {
		report.Skipped = true
		return report, nil
	}
	version := ModelVersion(modelName)
	report.ModelKey = modelName + "@" + version
	old := OpenVectors(repoDir)
	if !old.Matches(modelName, version, Dim) {
		old, report.Rebuilt = nil, old != nil
	}
	rows := make(map[string][]float32, len(items))
	keys := make(map[string]uint64, len(items))
	ids := make([]string, 0, len(items))
	todo := []int{}
	for at, item := range items {
		ids = append(ids, item.ID)
		key := keyOf(item.Hash)
		keys[item.ID] = key
		if key != 0 && old.Key(item.ID) == key {
			if kept := old.Get(item.ID); kept != nil {
				rows[item.ID] = kept
				report.Reused++
				continue
			}
		}
		todo = append(todo, at)
	}
	if old != nil {
		report.Dropped = old.Len() - report.Reused
		if report.Dropped < 0 {
			report.Dropped = 0
		}
	}
	if len(todo) == 0 && old != nil && report.Dropped == 0 && len(ids) == old.Len() {
		// 바뀐 것도 없고 사라진 것도 없다. 파일을 다시 쓸 이유가 없다.
		report.Elapsed = time.Since(started)
		report.Bytes = fileSize(path)
		return report, nil
	}
	if len(todo) > 0 {
		if err := makeVectors(modelName, items, todo, rows, &report); err != nil {
			return report, err
		}
	}
	if err := WriteVectors(path, modelName, version, Dim, ids, rows, keys); err != nil {
		return report, err
	}
	report.Bytes = fileSize(path)
	report.Elapsed = time.Since(started)
	return report, nil
}

// makeVectors 는 다시 계산할 건만 토막으로 나눠 추론한다. 글은 토막 안에서만
// 메모리에 있다.
func makeVectors(modelName string, items []Item, todo []int,
	rows map[string][]float32, report *BuildReport) error {
	model := OpenModel(modelName)
	if model == nil {
		report.Skipped = true
		return nil
	}
	defer model.Close()
	before := WarnCount()
	for from := 0; from < len(todo); from += buildChunk {
		to := from + buildChunk
		if to > len(todo) {
			to = len(todo)
		}
		texts := make([]string, 0, to-from)
		for _, at := range todo[from:to] {
			texts = append(texts, cut(textOf(items[at])))
		}
		made, err := model.Passages(texts)
		if err != nil {
			return err
		}
		for one, at := range todo[from:to] {
			if one < len(made) && made[one] != nil {
				rows[items[at].ID] = made[one]
				report.Made++
			}
		}
	}
	report.Failed = WarnCount() - before
	return nil
}

// textOf 는 임베딩에 넣을 글이다. 여기서 처음으로 파일을 읽는다.
func textOf(item Item) string {
	if item.Load != nil {
		return item.Load()
	}
	return item.Text
}

// cut 은 글을 앞에서 MaxText 글자만 남긴다. 글자(rune) 단위로 자른다.
func cut(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) <= MaxText {
		return text
	}
	return string(runes[:MaxText])
}

func fileSize(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
