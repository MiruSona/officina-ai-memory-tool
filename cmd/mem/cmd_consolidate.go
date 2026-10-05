package main

// mem consolidate --plan | --apply — 여러 기억을 모음 기억(observation) 카드로 묶는다
// (자동쌓기설계 3절 · B1). LLM 을 안 부른다. 묶음은 규칙이, 글은 코드가 정한다.
//
// 카드는 보류 없이 바로 쓰고 머리말에 origin: card 를 단다. 마음에 안 들면
// `mem auto undo --origin card --apply` 로 한꺼번에 보류로 돌린다.

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/consolidate"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var consolidateBools = []string{"plan", "apply", "json", "llm"}
var consolidateValues = []string{"rule", "scope", "max", "floor", "repo"}

// membersShown 은 계획 표에서 묶음마다 펼치는 구성원 수다.
const membersShown = 4

func init() {
	register(command{name: "consolidate", run: runConsolidate, bools: consolidateBools, values: consolidateValues})
}

// consolidateReport 는 --json 꼴이다.
type consolidateReport struct {
	Groups   []consolidate.Group `json:"groups"`
	New      int                 `json:"new"`
	Again    int                 `json:"again"`
	Same     int                 `json:"same"`
	TooBig   int                 `json:"too_big"`
	Dropped  int                 `json:"dropped"`
	NoVector bool                `json:"no_vector"`
	Millis   float64             `json:"ms"`
	Written  []string            `json:"written,omitempty"`
}

func runConsolidate(argv []string) int {
	parsed, err := parseOptions(argv, consolidateBools, consolidateValues)
	if err != nil {
		return fail(err.Error())
	}
	if len(parsed.rest) > 0 {
		return fail(i18n.T(i18n.ConsolidateUsage))
	}
	if parsed.flags["llm"] {
		// B2 자리만 둔다. 없는 기능을 조용히 무시하지 않고 말한다.
		fmt.Println(i18n.T(i18n.ConsolidateNoLLM))
		return exitOK
	}
	rule := parsed.text("rule")
	if rule != "" && rule != consolidate.RuleChain && rule != consolidate.RuleLink && rule != consolidate.RuleMeaning {
		return fail(i18n.T(i18n.ConsolidateBadRule, rule))
	}
	limit := 0
	if parsed.has("max") {
		limit, err = strconv.Atoi(parsed.text("max"))
		if err != nil || limit < 0 {
			return fail(i18n.T(i18n.ConsolidateUsage))
		}
	}
	floor := 0.0
	if parsed.has("floor") {
		floor, err = strconv.ParseFloat(parsed.text("floor"), 64)
		if err != nil || floor <= 0 || floor >= 1 {
			return fail(i18n.T(i18n.ConsolidateUsage))
		}
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	memories, err := autoMemories(opened)
	if err != nil {
		return fail(err.Error())
	}
	input := consolidate.Input{Memories: memories, Rule: rule, Scope: parsed.text("scope"), Floor: floor}
	input.AutoLinks = autoLinksOf(repository, parsed)
	// 중복 검사(quality)와 같이 만들어 둔 벡터만 본다. 모델은 안 싣는다.
	if vectors := vectorsFor(repository.Dir); vectors != nil && vectors.Len() > 0 {
		input.Vectors = vectors
	}
	started := time.Now()
	plan := consolidate.MakePlan(input)
	report := consolidateReport{Groups: plan.Groups, New: plan.New, Again: plan.Again, Same: plan.Same,
		TooBig: plan.TooBig, Dropped: plan.Dropped, NoVector: plan.NoVector,
		Millis: float64(time.Since(started).Microseconds()) / 1000}
	applied := exitOK
	if parsed.flags["apply"] {
		written, code := applyCards(repository, opened, plan, memories, limit)
		if code != exitOK && code != exitCheck {
			return code
		}
		report.Written, applied = written, code
	}
	if parsed.flags["json"] {
		if code := printJSON(report); code != exitOK {
			return code
		}
		return applied
	}
	printPlan(report, memories, parsed.flags["apply"])
	return applied
}

// autoLinksOf 는 색인의 auto_links 다. 색인이 없거나 못 열면 비운다 — 사람이 적은
// links 만으로도 ② 는 돈다.
func autoLinksOf(repository *config.Repository, parsed *options) [][2]string {
	database := openReady(repository, parsed)
	if database == nil {
		return nil
	}
	defer database.Close()
	pairs, err := database.AutoLinkPairs()
	if err != nil {
		return nil
	}
	return pairs
}

// applyCards 는 「새로」 묶음은 새 카드로, 「다시」 묶음은 그 카드의 머리말·본문을
// 고쳐 쓴다 (판 rev +1 · 옛 본문은 archive 로). 「그대로」 는 안 건드린다.
func applyCards(repository *config.Repository, opened *store.Store, plan consolidate.Plan,
	memories []*model.Memory, limit int) ([]string, int) {
	if err := opened.EnsureDirs(); err != nil {
		return nil, fail(err.Error())
	}
	byID := map[string]*model.Memory{}
	for _, one := range memories {
		byID[one.ID] = one
	}
	today := time.Now().Format("2006-01-02")
	names, written := []string{}, []string{}
	fresh, again := 0, 0
	for _, group := range plan.Groups {
		if group.Status == consolidate.StatusSame || (limit > 0 && len(written) >= limit) {
			continue
		}
		card := consolidate.Card(group, byID, today)
		if group.Status == consolidate.StatusNew {
			name, err := opened.WriteAdd(card)
			if err != nil {
				return nil, fail(err.Error())
			}
			names = append(names, name)
			written = append(written, name)
			fresh++
			continue
		}
		old := byID[group.Existing]
		rev := old.Rev + 1
		if old.Rev == 0 {
			rev = 2
		}
		patch, err := opened.WritePatch(old.ID, consolidate.Rewrite(card, rev))
		if err != nil {
			return nil, fail(err.Error())
		}
		amend, err := opened.WriteAmend(old.ID, card.Body)
		if err != nil {
			return nil, fail(err.Error())
		}
		names = append(names, patch, amend)
		written = append(written, old.ID)
		again++
	}
	if len(names) == 0 {
		return written, exitOK
	}
	promoted := promoteNow(repository, opened, names)
	// 새 카드는 승격 뒤에야 id 가 생긴다. 큐 이름을 id 로 바꿔 돌려준다.
	for at, name := range written {
		if outcome, ok := promoted.Outcomes[name]; ok && outcome.ID != "" {
			written[at] = outcome.ID
		}
	}
	noteLog(opened, store.LogCard, i18n.T(i18n.ConsolidateLog, fresh+again, fresh, again))
	fmt.Println(i18n.T(i18n.ConsolidateApplied, fresh+again, fresh, again))
	// 승격까지 못 간 큐가 있으면 0 아닌 코드다. 그대로 다시 돌리면 파일 목록에 그
	// 카드가 없어 「새로」 로 한 벌 더 큐에 넣는다 (리뷰 2026-10-05).
	if left := unpromoted(promoted, names); left > 0 {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.ConsolidateUnpromo, left))
		return written, exitCheck
	}
	return written, exitOK
}

// unpromoted 는 store 까지 못 간 큐 파일 수다 (락을 못 잡음 · 깨짐 · 오류).
func unpromoted(promoted *index.PromoteResult, names []string) int {
	count := 0
	for _, name := range names {
		switch promoted.Outcomes[name].State {
		case index.OutcomeNew, index.OutcomeDuplicate, index.OutcomeAppended, index.OutcomeDone,
			index.OutcomeGone: // gone 은 남이 먼저 승격한 것이다
		default:
			count++
		}
	}
	return count
}

// printPlan 은 사람이 읽는 계획 표다.
func printPlan(report consolidateReport, memories []*model.Memory, applied bool) {
	byID := map[string]*model.Memory{}
	for _, one := range memories {
		byID[one.ID] = one
	}
	counts := map[string]int{}
	for _, group := range report.Groups {
		counts[group.Rule]++
	}
	fmt.Println(i18n.T(i18n.ConsolidateHead, len(report.Groups), counts[consolidate.RuleChain],
		counts[consolidate.RuleLink], counts[consolidate.RuleMeaning], report.New, report.Again,
		report.Same, report.Millis))
	if report.NoVector {
		fmt.Println(i18n.T(i18n.ConsolidateNoVector))
	}
	if report.TooBig > 0 {
		fmt.Println(i18n.T(i18n.ConsolidateTooBig, report.TooBig))
	}
	if len(report.Groups) == 0 {
		fmt.Println(i18n.T(i18n.ConsolidateNone))
		return
	}
	for at, group := range report.Groups {
		tail := ""
		if group.Existing != "" {
			tail = " · " + group.Existing
			if group.Why != "" {
				tail += " (" + group.Why + ")"
			}
		}
		fmt.Println(i18n.T(i18n.ConsolidateGroup, at+1, group.Rule, group.Scope, len(group.Members),
			statusWord(group.Status), tail))
		for shown, id := range group.Members {
			if shown == membersShown {
				fmt.Println(i18n.T(i18n.ConsolidateMoreMem, len(group.Members)-membersShown))
				break
			}
			one := byID[id]
			fmt.Println(i18n.T(i18n.ConsolidateMember, id, one.Date, one.DisplayTitle()))
		}
	}
	if !applied {
		fmt.Println(i18n.T(i18n.ConsolidateDryRun))
	}
}

func statusWord(status string) string {
	switch status {
	case consolidate.StatusNew:
		return "새로"
	case consolidate.StatusAgain:
		return "다시"
	}
	return "그대로"
}
