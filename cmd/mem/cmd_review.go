package main

// mem review — 사람이 볼 검토 큐다 (설계 3-4 · 결정 28).
// 만료 · 모순 후보 · 낡음 후보 · 차가운 것을 한 화면에 모으고, **판정은 안 한다.**
// 큐를 만드는 것은 파도 G 의 internal/review 이고 여기서는 화면만 찍는다.

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/review"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var reviewBools = []string{"json"}
var reviewValues = []string{"kind", "limit", "repo", "promote"}

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
	if parsed.has("promote") {
		return promoteReview(parsed, parsed.text("promote"))
	}
	kinds := parsed.list("kind")
	for _, kind := range kinds {
		if !review.KnownKind(kind) {
			return fail(i18n.T(i18n.UnknownOption, "--kind "+kind))
		}
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	options := review.Options{Store: opened, Config: repository.Config,
		Vocab: vocabOf(repository), Now: time.Now(), Kinds: kinds, Limit: limitOf(parsed),
		Near: nearOf(vectorsFor(repository.Dir)), Demoted: demotedOf(repository)}
	review.Prepare(&options)
	report, err := review.Run(options)
	if err != nil {
		return exitFor(err)
	}
	store.AppendHit(opened.Dir, "review", "")
	if parsed.flags["json"] {
		data, err := json.Marshal(report)
		if err != nil {
			return fail(err.Error())
		}
		fmt.Println(string(data))
		return exitOK
	}
	fmt.Print(review.Markdown(report))
	return exitOK
}

// limitOf 는 --limit 이다. 안 주거나 못 읽으면 review 의 기본값을 쓴다.
func limitOf(parsed *options) int {
	value := 0
	if _, err := fmt.Sscanf(parsed.text("limit"), "%d", &value); err != nil || value < 0 {
		return 0
	}
	return value
}
