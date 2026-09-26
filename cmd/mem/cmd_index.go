package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
	"github.com/mirusona/officina-ai-memory-tool/internal/gc"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/link"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var indexBools = []string{"full", "gc", "no-gc", "quiet", "verify", "clear-bad", "bad", "json"}
var indexValues = []string{"repo"}

func init() {
	register(command{name: "index", run: runIndex, bools: indexBools, values: indexValues})
}

// runIndex 는 inbox 를 승격하고 바뀐 파일을 색인한다. 락을 못 잡으면 조용히
// 건너뛴다 (설계 5-2).
func runIndex(argv []string) int {
	parsed, err := parseOptions(argv, indexBools, indexValues)
	if err != nil {
		return fail(err.Error())
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	// --bad 는 보기만 한다. 락도 안 잡고 파일도 안 건드리니 --clear-bad 와
	// 같이 줘도 이쪽이 먼저다 — 지우는 쪽으로 넘어가는 일이 없다.
	if parsed.flags["bad"] {
		return showBad(opened)
	}
	if parsed.flags["clear-bad"] {
		return clearBad(opened)
	}
	settings := repository.Config
	// --json 은 사람 글을 한 줄도 안 섞는다. 파서가 읽는 출력이다.
	quiet := parsed.flags["quiet"] || parsed.flags["json"]
	sayQueueWait(opened, quiet)
	result, err := index.Run(index.Options{
		Store: opened, GC: settings.GC, Secret: settings.Secret,
		Quiet: quiet, Verify: parsed.flags["verify"], Full: parsed.flags["full"],
		// 모델이 있을 때만 벡터 목록을 담는다 (리뷰 B · V1).
		WantVectors: embed.Ready(modelName()),
	})
	if err != nil {
		return exitFor(err)
	}
	// 색인이 끝난 뒤에 벡터를 맞춘다. 바뀐 건만 다시 계산한다 (결정 12).
	if !result.Locked {
		updateVectors(repository, opened, result, quiet)
	}
	if parsed.flags["json"] {
		return reportJSON(parsed, opened, settings.GC, result)
	}
	return reportRun(parsed, opened, settings.GC, result)
}

// clearBad 는 inbox/bad 에 쌓인 실패 파일을 보여주고 지운다. 지우는 명령이
// 없으면 한 번의 실패가 status·lint 에 영원히 남는다 (리뷰 C #14).
func clearBad(opened *store.Store) int {
	names, err := opened.ListBad()
	if err != nil {
		return fail(err.Error())
	}
	if len(names) == 0 {
		fmt.Println(i18n.T(i18n.IndexClearNone))
		return exitOK
	}
	// 이름·까닭은 남이 만든 글이라 `--bad` 와 같이 중화를 지난다 (불변조건 I3).
	for _, name := range names {
		shown := safe.Summary(name, badNameRoom)
		if reason := opened.BadReason(name); reason != "" {
			fmt.Println("  " + shown + " — " + safe.Summary(reason, badReasonRoom))
			continue
		}
		fmt.Println("  " + shown)
	}
	gone, err := opened.ClearBad(names)
	if err != nil {
		return fail(err.Error())
	}
	fmt.Println(i18n.T(i18n.IndexClearBad, gone))
	return exitOK
}

// badRowFormat 은 `--bad` 한 줄이다 (이름 · 종류 · 까닭 · 푸는 길). 한글이 없는
// 짜임새라 i18n 표에 안 둔다 (warnRowFormat 과 같은 규칙).
const badRowFormat = "- %s · %s · %s · %s"

// showBad 는 inbox/bad 에 쌓인 것을 보여주기만 한다. 옮기지도 지우지도 않고
// 락도 안 잡는다 (설계 2026-09-23 3-3). 이름·까닭은 남이 만든 글이라 줄마다
// 중화를 지난다 (불변조건 I3). 본문·값은 안 찍는다 — 까닭 파일도 규칙 이름·칸
// 이름뿐이다.
func showBad(opened *store.Store) int {
	names, err := opened.ListBad()
	if err != nil {
		return fail(err.Error())
	}
	if len(names) == 0 {
		fmt.Println(i18n.T(i18n.IndexBadNone))
		return exitOK
	}
	fmt.Println(i18n.T(i18n.IndexBadHead, len(names)))
	for _, name := range names {
		op := opened.BadOp(name)
		reason := opened.BadReason(name)
		if reason == "" {
			reason = i18n.T(i18n.IndexBadNoReason)
		}
		fmt.Printf(badRowFormat+"\n", safe.Summary(name, badNameRoom), badKind(op),
			safe.Summary(reason, badReasonRoom), badFix(op))
	}
	fmt.Println(i18n.T(i18n.IndexBadClearHint))
	return exitOK
}

// badNameRoom · badReasonRoom 은 한 줄에 싣는 룬 수다. 큐 이름은 60자 안쪽이고
// 까닭은 규칙 한 줄이라 넉넉하다. 누가 손으로 긴 이름을 넣어도 화면이 안 무너진다.
const (
	badNameRoom   = 80
	badReasonRoom = 160
)

// badKind 는 op 를 화면에 찍을 말로 바꾼다. 못 읽은 것은 「깨짐」이다.
func badKind(op string) string {
	if op == "" {
		return i18n.T(i18n.IndexBadBroken)
	}
	return op
}

// badFix 는 종류마다 푸는 길이다. add 는 값을 고쳐 다시 넣어야 하고, 나머지
// (patch · body · 깨짐)는 대상이 없어졌거나 손으로 쓴 것이라 치우면 끝이다
// (설계 3-1 표). bad 를 큐로 되돌리는 길은 일부러 없다 — 같은 까닭으로 또 걸린다.
func badFix(op string) string {
	if op == store.OpAdd {
		return i18n.T(i18n.IndexBadFixAdd)
	}
	return i18n.T(i18n.IndexBadFixDrop)
}

// reportRun 은 센 것을 한글로 찍는다. 색인 안 된 파일이 있으면 종료 코드 2 다.
func reportRun(parsed *options, opened *store.Store, settings config.GCConfig, result *index.Result) int {
	quiet := parsed.flags["quiet"]
	if result.Locked {
		if !quiet {
			fmt.Println(i18n.T(i18n.IndexLocked))
		}
		return exitOK
	}
	if !quiet {
		printRun(result)
	}
	// `--quiet` 도 `--json` 처럼 gc 조건을 봐야 한다. 예전에는 여기가 통째로
	// 안 돌아서 --quiet 로만 도는 훅 회차에서 gc 가 영영 안 걸렸다 (A8).
	reportGCQuiet(parsed, opened, settings, result, quiet)
	// 비밀정보로 막힌 것이 하나라도 있으면 보안 차단(4)이다. 규격 위반으로
	// 빠진 것(2)과 숫자를 갈라야 스크립트가 둘을 다르게 다룬다 (설계 8절).
	if result.Secret > 0 {
		return exitSecurity
	}
	if len(result.Unindexed) > 0 {
		return exitCheck
	}
	return exitOK
}

func printRun(result *index.Result) {
	if result.Rebuilt {
		fmt.Println(i18n.T(i18n.IndexRebuilt))
	}
	fmt.Println(i18n.T(i18n.IndexPromote, result.Added, result.Appended, result.Duplicated,
		result.Patched, result.Bad, result.Left))
	fmt.Println(i18n.T(i18n.IndexReport, result.Indexed, result.Skipped, result.Removed,
		result.Total, result.Elapsed.Seconds()))
	if result.LinkBlocked > 0 {
		fmt.Println(i18n.T(i18n.IndexLinkBlocked, result.LinkBlocked, result.LinkBiggest, link.BlockMax()))
	}
	if len(result.Unindexed) > 0 {
		fmt.Println(i18n.T(i18n.IndexUnindexed, len(result.Unindexed)))
	}
}

// queueNoticeMin 은 「이번 회차는 오래 걸린다」를 미리 말하는 큐 건수다.
// `migrate --apply` 는 저장소를 통째로 큐에 넣어서 20k 에서 첫 색인이 6분인데
// v0.4 까지는 아무 말이 없어 사람이 멈춘 줄 알았다 (리뷰 A R3).
const queueNoticeMin = 200

// sayQueueWait 는 큐가 많이 밀렸을 때 한 줄로 예고한다. 큐를 못 읽는 것은
// 여기서 알릴 일이 아니다 — 색인이 곧 같은 자리에서 제 오류를 낸다.
func sayQueueWait(opened *store.Store, quiet bool) {
	if quiet {
		return
	}
	names, err := opened.ListInbox()
	if err != nil || len(names) < queueNoticeMin {
		return
	}
	fmt.Println(safe.Text(i18n.T(i18n.QueueWait, len(names))))
}

// indexJSON 은 `mem index --json` 이 내는 꼴이다. 이름은 lint·search·add 와
// 같은 소문자 밑줄이다. 담는 것이 센 수와 파일 이름뿐이라 비밀정보 값이 실릴
// 자리가 없다 — 막힌 건수는 secret 으로만 알린다.
type indexJSON struct {
	Locked      bool       `json:"locked"`
	Rebuilt     bool       `json:"rebuilt"`
	Added       int        `json:"added"`
	Appended    int        `json:"appended"`
	Duplicated  int        `json:"duplicated"`
	Patched     int        `json:"patched"`
	Bad         int        `json:"bad"`
	Left        int        `json:"left"`
	Indexed     int        `json:"indexed"`
	Skipped     int        `json:"skipped"`
	Removed     int        `json:"removed"`
	Total       int        `json:"total"`
	Secret      int        `json:"secret"`
	AutoLinked  int        `json:"auto_linked"`
	LinkBlocked int        `json:"link_blocked"`
	LinkBiggest int        `json:"link_biggest"`
	Unindexed   []string   `json:"unindexed"`
	ElapsedMS   int64      `json:"elapsed_ms"`
	GCSkipped   bool       `json:"gc_skipped"`
	GC          *gc.Result `json:"gc,omitempty"`
}

// reportJSON 은 사람 글 대신 한 줄 JSON 을 찍는다. 종료 코드는 사람 화면과
// 똑같다 — 부르는 쪽이 화면 꼴에 따라 다른 코드를 받으면 안 된다.
func reportJSON(parsed *options, opened *store.Store, settings config.GCConfig, result *index.Result) int {
	payload := indexJSON{
		Locked: result.Locked, Rebuilt: result.Rebuilt,
		Added: result.Added, Appended: result.Appended, Duplicated: result.Duplicated,
		Patched: result.Patched, Bad: result.Bad, Left: result.Left,
		Indexed: result.Indexed, Skipped: result.Skipped, Removed: result.Removed,
		Total: result.Total, Secret: result.Secret, AutoLinked: result.AutoLinked,
		LinkBlocked: result.LinkBlocked, LinkBiggest: result.LinkBiggest,
		Unindexed: safeNames(result.Unindexed),
		ElapsedMS: result.Elapsed.Milliseconds(),
		GCSkipped: parsed.flags["no-gc"],
	}
	if !result.Locked {
		payload.GC = maybeGC(parsed, opened, settings, result)
	}
	data, err := json.Marshal(payload)
	if err != nil {
		return fail(err.Error())
	}
	fmt.Println(string(data))
	if result.Locked {
		return exitOK
	}
	if result.Secret > 0 {
		return exitSecurity
	}
	if len(result.Unindexed) > 0 {
		return exitCheck
	}
	return exitOK
}

// safeNames 는 색인에서 빠진 파일 이름을 화면에 낼 꼴로 바꾼다. 파일 이름도
// 남이 지은 글이라 중화를 지난다 (불변조건 3). 0건이면 빈 배열이다.
func safeNames(names []string) []string {
	out := make([]string, 0, len(names))
	for _, name := range names {
		out = append(out, safe.Neutralize(safe.OneLine(name)))
	}
	return out
}

// maybeGC 는 정리를 돌려야 하면 돌리고 결과를 돌려준다. 안 돌면 nil 이다.
// 조건은 사람 화면 쪽(reportGCQuiet)과 `--json` 쪽이 같은 것 하나만 쓴다.
func maybeGC(parsed *options, opened *store.Store, settings config.GCConfig, result *index.Result) *gc.Result {
	if parsed.flags["no-gc"] {
		return nil
	}
	if !parsed.flags["gc"] && !result.GCReady {
		return nil
	}
	done, err := gc.Run(gc.Options{Store: opened, GC: settings})
	if err != nil {
		fmt.Fprintln(os.Stderr, err.Error())
		return nil
	}
	return done
}

// reportGCQuiet 는 조건이 되면 이어서 정리한다. 24시간 쿨다운은 gc 안에 있다
// (설계 5-2 · 9-4). 훅은 이 길로 안 온다 — 훅은 index.Run 만 부른다.
// quiet 이면 정리는 그대로 돌되 글만 안 찍는다 — `--json` 이 이미 그렇게 한다 (A8).
func reportGCQuiet(parsed *options, opened *store.Store, settings config.GCConfig, result *index.Result, quiet bool) {
	if parsed.flags["no-gc"] {
		if !quiet {
			fmt.Println(i18n.T(i18n.GCIndexSkip))
		}
		return
	}
	done := maybeGC(parsed, opened, settings, result)
	if done == nil || quiet {
		return
	}
	printGC(done, i18n.T(i18n.GCIndexRan))
}
