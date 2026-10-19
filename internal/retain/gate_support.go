package retain

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// R3 를 못 받은 갈래. retain:* 의 R3 거절 까닭 글은 이 낱말로 시작한다 —
// `mem retain` 이 자식 결과를 읽어 서버가 끊겼는지 가른다 (A2 설계 1절).
const (
	SupportUnreachable = "unreachable"
	SupportTimeout     = "timeout"
	SupportUnsure      = "unsure"
	SupportSkipped     = "skipped"
	SupportRefused     = "refused"
	SupportBad         = "bad"
)

// 세 칸 확률 글자는 llm.LetterSupport 등과 같다. TitleSureDefault 는 Context.TitleSure 가 비었을 때
// 쓰는 「반대」 문턱이다 (llm.toml nli_sure 기본).
const (
	letterSupport, letterContradict, letterUnrelated = "A", "B", "C"
	TitleSureDefault                                 = 0.50
)

// serverError 는 판정 서버 쪽 고장(5xx)이다. 그 판 동안 다시 물어도 같다.
var serverError = regexp.MustCompile(`http-5\d\d`)

// supportKind 는 판정자가 말한 까닭(llm.Judge.Problem)을 갈래로 접는다. 판을 멈추는
// unreachable 은 서버가 안 닿음 · 5xx 뿐이다. 비밀 꼴로 안 보냄은 refused, 이상한 답 ·
// 4xx · 까닭 없음은 bad 다 — 둘 다 그 후보만 거절한다.
func supportKind(skipped bool, problem string) string {
	switch {
	case skipped:
		return SupportSkipped
	case strings.Contains(problem, SupportTimeout):
		return SupportTimeout
	case strings.HasPrefix(problem, SupportUnsure):
		return SupportUnsure
	case strings.Contains(problem, SupportUnreachable) || serverError.MatchString(problem):
		return SupportUnreachable
	case strings.Contains(problem, "secret-shape"):
		return SupportRefused
	}
	return SupportBad
}

// supportMissing 은 retain:* 가 판정을 못 받았을 때의 R3 거절 까닭 글이다.
func supportMissing(skipped bool, problem string) string {
	kind := supportKind(skipped, problem)
	detail := kind
	if problem != "" {
		detail = problem
	}
	return kind + " — " + i18n.T(i18n.AutoJudgeRequired, detail)
}

// SupportDown 은 R3 까닭 글이 「판정 서버 끊김」(unreachable·timeout)인지다 — mem retain 이 판을 멈춘다.
func SupportDown(reason string) bool {
	return strings.HasPrefix(reason, SupportUnreachable) || strings.HasPrefix(reason, SupportTimeout)
}

// problemer 는 판정을 못 받은 까닭을 말해 주는 판정자다 (llm.Judge). 없어도 된다.
type problemer interface{ Problem() string }

// skipper 는 물을 단이 하나도 없었는지 말해 주는 판정자다 (llm.Judge). 그때는 경고 없이 넘긴다.
type skipper interface{ Skipped() bool }

// prober 는 마지막 판정의 세 칸 확률을 주는 판정자다 (llm.Judge). 까닭 글과 제목 검사가 쓴다.
type prober interface{ Probs() map[string]float64 }

// supportAsk 는 판정 한 번의 결과다.
type supportAsk struct {
	supported, ok, skipped bool
	problem                string
	probs                  map[string]float64
}

func askJudge(judge Judge, evidence, claim string) supportAsk {
	answer := supportAsk{}
	answer.supported, answer.ok = judge.Supports(evidence, claim)
	if quiet, has := judge.(skipper); has {
		answer.skipped = quiet.Skipped()
	}
	if why, has := judge.(problemer); has {
		answer.problem = why.Problem()
	}
	if probs, has := judge.(prober); has {
		answer.probs = probs.Probs()
	}
	return answer
}

// halts 는 retain:* 에서 남은 물음을 그만둘 답인지다 — 서버 끊김 · 물을 단 없음.
func (a supportAsk) halts() bool {
	if a.ok {
		return false
	}
	kind := supportKind(a.skipped, a.problem)
	return kind == SupportUnreachable || kind == SupportTimeout || kind == SupportSkipped
}

// reason 은 지지를 못 받은 까닭 글이다. 세 칸 확률이 있으면 늘 붙인다.
func (a supportAsk) reason(head string) string {
	if !a.ok {
		head = supportMissing(a.skipped, a.problem)
	}
	return head + probsText(a.probs)
}

// probsText 는 「 (a/b/c 0.34/0.04/0.62)」 꼴이다. 확률이 없으면 빈 글이다.
func probsText(probs map[string]float64) string {
	if len(probs) == 0 {
		return ""
	}
	return fmt.Sprintf(" (a/b/c %.2f/%.2f/%.2f)", probs[letterSupport], probs[letterContradict], probs[letterUnrelated])
}

// checkSupportStrict 는 retain:* 의 R3 다. 인용마다 묻고, 하나라도 못 넘으면 인용 전부를 한 줄로
// 이어 한 번 더 묻는다(호출은 인용 수 + 1 까지). 지지를 받으면 제목이 근거와 어긋나는지 본다.
// 판정을 못 받으면 거절이다 — 사람 눈이 없는 길이다 (A2 설계 5절 · 첫 실측 5절).
func checkSupportStrict(result *Result, candidate Candidate, context Context) {
	summary := candidate.Memory.Summary
	notes := []string{}
	for _, quote := range candidate.Quotes {
		answer := askJudge(context.Judge, quote, summary)
		if answer.halts() {
			result.reject(RuleSupport, answer.reason(""))
			return
		}
		if !answer.ok || !answer.supported {
			notes = append(notes, answer.reason(i18n.T(i18n.AutoNotSupported, shortText(quote))))
		}
	}
	if len(notes) > 0 && len(candidate.Quotes) > 1 {
		answer := askJudge(context.Judge, strings.Join(candidate.Quotes, " "), summary)
		if answer.halts() {
			result.reject(RuleSupport, answer.reason(""))
			return
		}
		if answer.ok && answer.supported {
			notes = nil
		} else {
			notes = append([]string{answer.reason(i18n.T(i18n.AutoBundleNotSupp))}, notes...)
		}
	}
	if len(notes) > 0 {
		result.reject(RuleSupport, strings.Join(notes, " · "))
		return
	}
	checkTitle(result, candidate, context)
}

// checkTitle 은 같은 근거(인용 묶음)에 제목을 물어 「반대」가 문턱 이상이면 거절한다. 제목은
// 문장이 아니라 지지는 요구하지 않는다 — 요약은 맞고 제목만 틀린 후보를 막는다 (첫 실측 함정 1).
func checkTitle(result *Result, candidate Candidate, context Context) {
	title := strings.TrimSpace(candidate.Memory.Title)
	if title == "" {
		return
	}
	answer := askJudge(context.Judge, strings.Join(candidate.Quotes, " "), title)
	if answer.halts() && !answer.skipped {
		result.reject(RuleSupport, answer.reason(""))
		return
	}
	sure := context.TitleSure
	if sure <= 0 {
		sure = TitleSureDefault
	}
	if against := answer.probs[letterContradict]; against >= sure {
		result.reject(RuleSupport, i18n.T(i18n.AutoTitleContradict, against, sure)+probsText(answer.probs))
	}
}

// checkSupportLoose 는 (가) stop 의 R3 다. 근거마다 지지를 받아야 하고, 판정을 못 받으면
// 경고로 흘린다 (사용자 결정 2026-10-08).
func checkSupportLoose(result *Result, candidate Candidate, context Context) {
	for _, quote := range candidate.Quotes {
		answer := askJudge(context.Judge, quote, candidate.Memory.Summary)
		if answer.ok {
			if !answer.supported {
				result.reject(RuleSupport, i18n.T(i18n.AutoNotSupported, shortText(quote))+probsText(answer.probs))
			}
			continue
		}
		// 이 근거만 물을 단이 없었다 — 남은 근거는 규칙 B 에 걸릴 수 있어 계속 본다.
		if answer.skipped {
			continue
		}
		warning := i18n.T(i18n.AutoJudgeSkipped)
		if answer.problem != "" {
			warning = i18n.T(i18n.AutoJudgeSkippedWhy, answer.problem)
		}
		result.Warnings = append(result.Warnings, warning)
		return
	}
}
