package main

// mem review — 사람이 볼 검토 큐다 (설계 3-4 · 결정 28).
// 종류별 큐(review.Kinds)를 한 화면에 모으고, **판정은 안 한다.**
// 출력 꼴은 넷이다 — 기본 목록 · --table(scope 별 표) · --ids(AI 용 탭 줄) · --json.
// 큐를 만드는 것은 파도 G 의 internal/review 이고 여기서는 화면만 찍는다.

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/llm"
	"github.com/mirusona/officina-ai-memory-tool/internal/review"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var reviewBools = []string{"json", "table", "ids", "all", "nli"}
var reviewValues = []string{"kind", "limit", "repo", "promote", "reject", "scope", "nli-pairs"}

func init() {
	register(command{name: "review", run: runReview, bools: reviewBools, values: reviewValues})
}

// runReview 는 큐를 만들어 보여준다. 큐가 비어도 종료 코드는 0 이다 — 볼 것이
// 없는 것은 잘못이 아니다.
func runReview(argv []string) int {
	parsed, err := parseOptions(argv, reviewBools, reviewValues)
	if err != nil {
		return fail(err.Error())
	}
	// 승격은 큐를 만들기 전에 갈라진다. 화면을 찍는 명령과 기억을 고치는
	// 명령을 섞지 않는다 (결정 6 · cmd_review_promote.go).
	if parsed.has("promote") && parsed.has("reject") {
		return fail(i18n.T(i18n.ReviewRejectBoth))
	}
	if parsed.has("promote") {
		return promoteReview(parsed, parsed.text("promote"))
	}
	if parsed.has("reject") {
		return rejectReview(parsed, parsed.text("reject"))
	}
	kinds := parsed.list("kind")
	for _, kind := range kinds {
		if !review.KnownKind(kind) {
			return fail(i18n.T(i18n.UnknownOption, "--kind "+kind))
		}
	}
	pairs, ok := nliPairsOf(parsed)
	if !ok {
		return fail(i18n.T(i18n.UnknownOption, "--nli-pairs "+parsed.text("nli-pairs")))
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	options := review.Options{Store: opened, Config: repository.Config,
		Vocab: vocabOf(repository), Now: time.Now(), Kinds: kinds, Limit: limitOf(parsed),
		Near: nearOf(vectorsFor(repository.Dir)), Demoted: demotedOf(repository),
		Canon: canonOf(repository), Scopes: parsed.list("scope"), ByScope: parsed.flags["table"],
		All: parsed.flags["all"]}
	if parsed.flags["nli"] {
		options.NLI = reviewNLI(repository, pairs)
	}
	review.Prepare(&options)
	report, err := review.Run(options)
	if err != nil {
		return exitFor(err)
	}
	store.AppendHit(opened.Dir, "review", "")
	// 출력 꼴 깃발이 둘 이상이면 json → ids → table 차례로 하나만 쓴다.
	if parsed.flags["json"] {
		data, err := json.Marshal(report)
		if err != nil {
			return fail(err.Error())
		}
		fmt.Println(string(data))
		return exitOK
	}
	switch {
	case parsed.flags["ids"]:
		fmt.Print(review.IDs(report))
	case parsed.flags["table"]:
		fmt.Print(review.Table(report))
	default:
		fmt.Print(review.Markdown(report))
	}
	return exitOK
}

// reviewNLI 는 C19 의 NLI 단이다 (`--nli`). 판정 기록 폴더와 비밀 꼴 거절은 mem judge 와
// 같은 것을 쓰고, SemIf 는 안 부른다. nli_url 이 없으면 Judge 가 nil 이라 review 가
// 「못 본 것」에 한 줄 남긴다.
func reviewNLI(repository *config.Repository, pairs int) *review.NLIOptions {
	settings := loadLLM()
	scanner := scannerFor(repository.Config.Secret)
	refuse := func(text string) bool { return scanner.ScanText(text) != nil }
	return &review.NLIOptions{Judge: review.NLIJudge(llm.NewNLI(settings), judgeDir(repository), refuse),
		Health: review.HealthOf(settings.NLIURL), Pairs: pairs}
}

// nliPairsOf 는 --nli-pairs 다. 안 주면 0(기본값)이고, 수가 아니거나 1 아래면 거절한다.
func nliPairsOf(parsed *options) (int, bool) {
	if !parsed.has("nli-pairs") {
		return 0, true
	}
	value, err := strconv.Atoi(strings.TrimSpace(parsed.text("nli-pairs")))
	if err != nil || value < 1 {
		return 0, false
	}
	return value, true
}

// limitOf 는 --limit 이다. 안 주거나 못 읽으면 review 의 기본값을 쓴다.
func limitOf(parsed *options) int {
	value := 0
	if _, err := fmt.Sscanf(parsed.text("limit"), "%d", &value); err != nil || value < 0 {
		return 0
	}
	return value
}
