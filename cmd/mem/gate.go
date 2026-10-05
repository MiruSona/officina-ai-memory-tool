package main

// add·set 이 같이 쓰는 품질 관문 배선이다. 규칙 자체는 internal/quality 가
// 들고 있고 여기서는 「무엇을 견줄지」 만 붙인다 (설계 3-1).

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
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
		Lookup: lookupOf(opened),
	}
}

// lookupOf 는 id 로 기억 파일 하나를 읽는 함수다. 파일 자리가 id 로 정해져 있어
// 저장소를 훑지 않는다. 없거나 못 읽으면 nil 이다.
func lookupOf(opened *store.Store) func(string) *model.Memory {
	return func(id string) *model.Memory {
		path := model.StorePath(id)
		if path == "" {
			return nil
		}
		file, err := opened.ReadMemory(path)
		if err != nil || file == nil {
			return nil
		}
		return file.Memory
	}
}

// fillBasisHash 는 손으로 넣는 모음 기억(observation)에 basis_hash 를 계산해 넣는다
// (카드와 같은 해시 함수). 해시가 없으면 근거가 고쳐져도 낡음을 못 잡는다.
// 근거 하나라도 못 읽으면 비워 둔다 — 그 기억은 죽음·보류만 본다.
func fillBasisHash(request *store.AddRequest, lookup func(string) *model.Memory) {
	if request.Type != model.TypeObservation || request.BasisHash != "" {
		return
	}
	ids := model.MemSources(request.Sources)
	if len(ids) == 0 {
		return
	}
	parts := make([]model.BasisPart, 0, len(ids))
	for _, id := range ids {
		found := lookup(id)
		if found == nil {
			return
		}
		parts = append(parts, model.PartOf(found))
	}
	request.BasisHash = model.BasisHash(parts)
}

// printVerdict 는 관문 결과를 사람에게 말한다. 거절이면 반드시 셋을 말한다 —
// 무슨 규칙에 걸렸나 · 어느 기억 때문인가 · 다음에 뭘 하면 되나 (설계 3-1).
func printVerdict(verdict quality.Verdict) {
	printVerdictAs(verdict, false)
}

// printVerdictAs 는 printVerdict 이되 auto 면 자동 기억이 못 쓰는 다음 수(`--new`·
// `--by`·`--by-new`)를 빼고 대신 맞는 한 줄을 찍는다 (A1 뒷정리).
func printVerdictAs(verdict quality.Verdict, auto bool) {
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
	printNextSteps(out, verdict, auto)
}

// humanOnlyStep 은 자동 기억에는 안 듣는 다음 수인지다. 자동 관문은 `--new` 로
// 닮음을 못 넘고, 덮기(`--by`)는 사람 몫이다 (R5 · 자동쌓기설계 2-3).
func humanOnlyStep(step string) bool {
	return step == quality.NewTopicStep || strings.Contains(step, "--by-new") ||
		strings.HasPrefix(step, "mem add … --by ")
}

func printNextSteps(out *os.File, verdict quality.Verdict, auto bool) {
	steps := []string{}
	seen := map[string]bool{}
	dropped := false
	for _, one := range verdict.Findings {
		for _, next := range one.Next {
			if auto && humanOnlyStep(next) {
				dropped = true
				continue
			}
			if !seen[next] {
				seen[next] = true
				steps = append(steps, next)
			}
		}
	}
	if dropped {
		fmt.Fprintln(out, i18n.T(i18n.AddAutoNoNew))
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
