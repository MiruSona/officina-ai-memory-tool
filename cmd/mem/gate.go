package main

// add·set 이 같이 쓰는 품질 관문 배선이다. 규칙 자체는 internal/quality 가
// 들고 있고 여기서는 「무엇을 견줄지」 만 붙인다 (설계 3-1).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// gateRepo 는 관문이 견줄 「이미 있는 기억」 을 저장소 파일에서 준다.
// 색인이 없어도 돌아야 해서 DB 가 아니라 md 를 읽는다 — 갓 init 한 저장소에서
// add 가 죽으면 안 된다.
type gateRepo struct {
	opened *store.Store
	cache  []*model.Memory
	read   bool
}

// Recent 는 새 기억부터 limit 건이다. 파일 이름이 `YYYYMMDD-…` 라 경로를
// 거꾸로 정렬하면 날짜 차례가 된다.
func (g *gateRepo) Recent(limit int) ([]*model.Memory, error) {
	if g.read {
		return capMemories(g.cache, limit), nil
	}
	g.read = true
	files, err := g.opened.ListMemories()
	if err != nil {
		return nil, err
	}
	sort.Slice(files, func(a, b int) bool { return files[a].Path > files[b].Path })
	room := limit
	if room <= 0 || room > len(files) {
		room = len(files)
	}
	for _, file := range files[:room] {
		one, err := g.opened.ReadListed(file)
		if err != nil {
			// 못 읽는 파일 하나 때문에 관문이 통째로 죽으면 안 된다. 그 파일은
			// lint·index 가 따로 알린다 (불변조건 I7).
			continue
		}
		g.cache = append(g.cache, one.Memory)
	}
	return capMemories(g.cache, limit), nil
}

func capMemories(list []*model.Memory, limit int) []*model.Memory {
	if limit <= 0 || limit > len(list) {
		return list
	}
	return list[:limit]
}

// vocabOf 는 저장소의 태그·scope 표준 목록이다. 파일이 없으면 기본 목록이다.
func vocabOf(repository *config.Repository) config.Vocab {
	vocab, err := config.LoadVocab(filepath.Join(repository.Dir, config.VocabFileName))
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
	}
	return vocab
}

// demotedOf 는 `mem eval --quality` 가 mem.toml 에 적어 둔 강등표다.
func demotedOf(repository *config.Repository) map[string]quality.Grade {
	text, err := os.ReadFile(filepath.Join(repository.Dir, config.FileName))
	if err != nil {
		return nil
	}
	return quality.ParseDemoted(string(text))
}

// gateOptions 는 관문 한 번의 조건이다.
func gateOptions(repository *config.Repository, opened *store.Store, parsed *options) quality.Options {
	return quality.Options{
		Config: repository.Config, Vocab: vocabOf(repository), Now: time.Now(),
		Repo: &gateRepo{opened: opened}, Demoted: demotedOf(repository),
		Near:           nearOf(vectorsFor(repository.Dir)),
		AllowDuplicate: parsed.flags["new"], SupersedeOf: parsed.text("by"),
	}
}

// printVerdict 는 관문 결과를 사람에게 말한다. 거절이면 반드시 셋을 말한다 —
// 무슨 규칙에 걸렸나 · 어느 기억 때문인가 · 다음에 뭘 하면 되나 (설계 3-1).
func printVerdict(verdict quality.Verdict) {
	out := os.Stdout
	if !verdict.Rejected() {
		out = os.Stderr
	}
	for _, line := range verdict.Reasons() {
		fmt.Fprintln(out, line)
	}
	for _, one := range verdict.Findings {
		for _, related := range one.Related {
			fmt.Fprintln(out, "  "+related)
		}
	}
	printNextSteps(out, verdict)
}

func printNextSteps(out *os.File, verdict quality.Verdict) {
	steps := []string{}
	seen := map[string]bool{}
	for _, one := range verdict.Findings {
		for _, next := range one.Next {
			if !seen[next] {
				seen[next] = true
				steps = append(steps, next)
			}
		}
	}
	if len(steps) == 0 {
		return
	}
	steps = newTopicFirst(steps)
	fmt.Fprintln(out, i18n.T(i18n.GateNextHead))
	for _, step := range steps {
		fmt.Fprintln(out, "  "+step)
	}
}

// newTopicFirst 는 「정말 다른 주제다(--new)」 줄을 맨 앞으로 올린다. 결정이
// 중복 관문과 결정 관문에 같이 걸리면 중복 관문의 `mem set … --by-new` 덮기가
// 첫 줄이 되어, 가장 쉬운 길이 남의 결정을 덮는 길이 된다 (리뷰 2026-09-23).
func newTopicFirst(steps []string) []string {
	for at, step := range steps {
		if step == quality.NewTopicStep && at > 0 {
			rest := append(append([]string{}, steps[:at]...), steps[at+1:]...)
			return append([]string{step}, rest...)
		}
	}
	return steps
}

// printVerdictJSON 은 --json 이 내는 판정 한 덩어리다.
func printVerdictJSON(verdict quality.Verdict) int {
	data, err := json.Marshal(verdict)
	if err != nil {
		return fail(err.Error())
	}
	fmt.Println(string(data))
	return verdict.ExitCode()
}

// fixNote 는 별칭 치환처럼 관문이 스스로 고친 것을 알린다.
func fixNote(verdict quality.Verdict) {
	for _, fix := range verdict.Fixes {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.GateFixed, fix.What, fix.From, fix.To))
	}
}
