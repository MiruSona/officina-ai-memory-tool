package main

// 임베딩을 꽂는 자리 (설계 결정 15).
//
// 쓰는 자리는 셋뿐이다 — 검색 재정렬 · 중복 후보 셋째 신호 · review 의 STALE
// 후보. **훅은 여기 안 온다.** 훅은 `mem hook` 이고 그 길에는 이 파일의 함수가
// 하나도 없다 (`wave3_embed_test.go` 가 못 박는다).

import (
	"fmt"
	"os"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/search"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// configModel 은 mem.toml `[embed] model` 값이다. 저장소를 열 때 채운다
// (wireNormalize 가 부른다) — 명령마다 따로 읽으면 값이 두 벌이 된다.
var configModel string

// wireEmbedModel 은 저장소가 정한 모델 이름을 알린다. 저장소가 없으면 비운다.
func wireEmbedModel(project *config.Repository) {
	if project == nil {
		configModel = ""
		return
	}
	configModel = project.Config.Embed.Model
}

// modelName 은 이번 판이 쓰는 모델이다. 센 것부터 : 환경변수 → mem.toml
// `[embed] model` → exe 가 아는 기본값. `mem install --model` 은 모델을 깔
// 뿐이고 어느 것을 쓸지는 안 정한다.
// 환경변수 MEM_EMBED_MODEL 은 **재기용 뒷문**이다 — T1' 이 같은 코드로 모델
// 둘을 태우려고 쓴다. 평소에는 아무도 안 준다.
func modelName() string {
	if given := os.Getenv("MEM_EMBED_MODEL"); given != "" {
		return given
	}
	if configModel != "" {
		return configModel
	}
	return embed.DefaultModel
}

// rerankerFor 는 검색에 꽂을 재정렬기다. 벡터 파일이 없으면 nil 이고, 그러면
// 검색은 낱말 모드로 답한다 (결정 16).
func rerankerFor(repoDir string, sources []search.Source) *embed.Reranker {
	if repoDir == "" || len(sources) == 0 {
		return nil
	}
	vectors := embed.OpenVectors(repoDir)
	if vectors == nil {
		return nil
	}
	database := sources[0].DB
	return embed.NewReranker(modelName(), vectors, func(docids []int64) map[int64]string {
		return namesOf(database, docids)
	})
}

// namesOf 는 docid 를 기억 id 로 바꾼다. 상위 200건뿐이라 한 번에 묻는다.
func namesOf(database *index.DB, docids []int64) map[int64]string {
	if database == nil || len(docids) == 0 {
		return nil
	}
	holes := make([]string, 0, len(docids))
	args := make([]any, 0, len(docids))
	for _, docid := range docids {
		holes = append(holes, "?")
		args = append(args, docid)
	}
	rows, err := database.SQL().Query(
		`SELECT docid, id FROM memories WHERE docid IN (`+strings.Join(holes, ",")+`)`, args...)
	if err != nil {
		return nil
	}
	defer rows.Close()
	out := make(map[int64]string, len(docids))
	for rows.Next() {
		docid := int64(0)
		id := ""
		if err := rows.Scan(&docid, &id); err == nil {
			out[docid] = id
		}
	}
	return out
}

// vectorsFor 는 중복·STALE 이 쓸 벡터다. 모델은 안 싣는다 — 이미 만들어 둔
// 벡터끼리만 견주면 된다.
func vectorsFor(repoDir string) *embed.Vectors { return embed.OpenVectors(repoDir) }

// nearOf 는 quality 에 넣을 값이다. **nil 인터페이스를 그대로 넣어야 한다** —
// 빈 포인터를 인터페이스에 담으면 quality 가 「있다」고 잘못 본다.
func nearOf(vectors *embed.Vectors) quality.Vectors {
	if vectors == nil || vectors.Len() == 0 {
		return nil
	}
	return vectors
}

// updateVectors 는 색인이 끝난 뒤 벡터를 최신으로 맞춘다. 열쇠가 같은 건은
// 그대로 쓴다 (결정 12·13).
//
// **모델이 없으면 저장소를 아예 안 읽는다** (리뷰 B · V1). 옛 판은 모델 유무를
// 보기 전에 `passagesOf` 로 기억 2만 개를 통째로 읽고 파싱했다. 색인이 스스로
// 잰 값은 0.15초인데 벽시계가 2.3~5.8초였던 것이 그 한 줄이다.
func updateVectors(repository *config.Repository, opened *store.Store,
	result *index.Result, quiet bool) {
	if repository == nil || opened == nil || result == nil {
		return
	}
	if !embed.Ready(modelName()) {
		if line := embedOffLine(repository); line != "" && !quiet {
			fmt.Println(line)
		}
		return
	}
	report, err := embed.Build(repository.Dir, modelName(), vectorItems(opened, result))
	if err != nil {
		if !quiet {
			fmt.Println(err.Error())
		}
		return
	}
	if quiet {
		return
	}
	if report.Skipped {
		fmt.Println(i18n.T(i18n.EmbedIndexSkipped))
		return
	}
	if report.Rebuilt {
		fmt.Println(i18n.T(i18n.EmbedIndexRebuilt))
	}
	fmt.Println(i18n.T(i18n.EmbedIndexMade, report.Made, report.Reused, report.Dropped,
		humanBytes(report.Bytes), report.Elapsed.Seconds()))
	if report.Failed > 0 {
		fmt.Println(i18n.T(i18n.EmbedIndexFailed, report.Failed))
	}
}

// embedOffLine 은 「의미 검색이 꺼져 있다」 한 줄이다. **설정이 끄기로 돼
// 있으면 아무 말도 안 한다** (스트레스 V12) — 사람이 안 쓰기로 정한 것을
// 명령마다 다시 권하지 않는다.
func embedOffLine(repository *config.Repository) string {
	if repository != nil && !repository.Config.Embed.Enabled {
		return ""
	}
	return i18n.T(i18n.EmbedIndexSkipped)
}

// vectorItems 는 색인이 담아 준 목록을 임베딩 항목으로 옮긴다. **글은 여기서
// 안 읽는다** — 정말 다시 계산할 건만 Load 가 그때 읽는다 (V1·V15).
func vectorItems(opened *store.Store, result *index.Result) []embed.Item {
	items := make([]embed.Item, 0, len(result.Vectors))
	for _, one := range result.Vectors {
		path := one.Path
		items = append(items, embed.Item{ID: one.ID, Hash: one.Hash,
			Load: func() string { return passageAt(opened, path) }})
	}
	return items
}

// passageAt 은 md 하나에서 임베딩이 읽을 글을 만든다. 못 읽으면 빈 글이고,
// 그러면 그 기억은 벡터 없이 지나간다.
func passageAt(opened *store.Store, path string) string {
	file, err := opened.ReadMemory(path)
	if err != nil || file == nil || file.Memory == nil {
		return ""
	}
	return passageText(file.Memory)
}

// passageText 는 기억마다 임베딩이 읽을 글이다. 제목·요약·본문 차례다 —
// 뜻은 앞머리에 몰려 있고 뒤는 잘려도 된다.
func passageText(one *model.Memory) string {
	parts := []string{one.Title, one.Summary, one.Body}
	kept := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			kept = append(kept, part)
		}
	}
	return strings.Join(kept, " ")
}

// evalVectors 는 `mem eval` 이 쓸 재정렬기다. 검색과 같은 길이라야 잰 값이
// 실제 검색과 맞는다.
func evalVectors(repos *opened) search.Vectors {
	near := rerankerFor(repos.dirOf(), repos.sources)
	if near == nil {
		return nil
	}
	repos.closers = append(repos.closers, near.Close)
	return near
}
