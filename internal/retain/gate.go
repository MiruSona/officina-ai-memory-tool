package retain

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// 자동 관문 규칙 이름 (자동쌓기설계 2-3). log.md 와 화면에 이 이름이 찍힌다.
const (
	RuleQuote     = "R1-quote"
	RuleFragment  = "R2-fragment"
	RuleSupport   = "R3-support"
	RuleNoMerge   = "R4-no-merge"
	RuleNoBy      = "R5-no-supersede"
	RuleCap       = "R6-cap"
	RuleType      = "R7-type"
	RuleSources   = "R8-sources"
	OriginStop    = "stop"
	retainPrefix  = "retain:"
	quoteMinRunes = 15
	quoteMax      = 3
)

// dupRules 는 R4 가 보는 기존 관문 규칙이다. 경고 등급이어도 자동 기억에는 거절이다.
var dupRules = map[string]bool{"duplicate-hard": true, "duplicate-soft": true, "same-body": true}

// Judge 는 R3(근거 지지 판정)을 맡는 바깥 LLM 자리다 (자동쌓기설계 2-3 · 5절 K).
// llm.toml 이 켜져 있으면 llm.Judge 가 들어오고, 없으면 nil 이라 R3 를 건너뛴다.
type Judge interface {
	// Supports 는 근거 문장이 요약을 뒷받침하는지다. ok 가 거짓이면 판정을
	// 못 받은 것이다 (서버가 안 닿음 등).
	Supports(quote, summary string) (supported bool, ok bool)
}

// Candidate 는 자동 관문에 거는 기억 한 건이다.
type Candidate struct {
	Memory     *model.Memory
	Origin     string
	Quotes     []string
	Supersedes string
	// GateRules 는 기존 add 관문이 낸 규칙 이름이다 (R4 가 본다).
	GateRules []string
}

// Context 는 관문이 대조할 자료다. Record 가 nil 이면 세션 기록을 못 찾은 것이다.
type Context struct {
	Settings    config.RetainConfig
	Root        string
	StoreDir    string
	Record      *Tally
	SessionAdds int
	DayAdds     int
	Scopes      []string
	Judge       Judge
}

// Finding 은 걸린 것 하나다.
type Finding struct {
	Rule   string `json:"rule"`
	Reason string `json:"reason"`
}

// Result 는 관문 한 번이다. Warnings 는 저장을 막지 않는 알림이다.
type Result struct {
	Rejects  []Finding `json:"rejects"`
	Warnings []string  `json:"warnings"`
}

// Rejected 는 저장을 막았는지다.
func (r Result) Rejected() bool { return len(r.Rejects) > 0 }

// IsRetain 은 로컬 모델 길((나))에서 온 기억인지다. 규칙이 더 엄하다.
func IsRetain(origin string) bool { return strings.HasPrefix(origin, retainPrefix) }

// Check 는 R1~R8 을 돌린다(R3 는 판정자가 있을 때만). 하나라도 걸리면 저장하지 않는다.
func Check(candidate Candidate, context Context) Result {
	result := Result{}
	strict := IsRetain(candidate.Origin)
	talk, all := "", ""
	if context.Record != nil {
		talk, all = Fold(context.Record.Talk.String()), Fold(context.Record.All.String())
	} else {
		if strict {
			result.reject(RuleQuote, i18n.T(i18n.AutoNoRecord))
		} else {
			result.Warnings = append(result.Warnings, i18n.T(i18n.AutoNoRecordWarn))
		}
	}
	checkQuotes(&result, candidate, context.Record != nil, strict, talk)
	checkFragments(&result, candidate.Memory, context, all)
	for _, rule := range candidate.GateRules {
		if dupRules[rule] {
			result.reject(RuleNoMerge, i18n.T(i18n.AutoDuplicate, rule))
			break
		}
	}
	if candidate.Supersedes != "" {
		result.reject(RuleNoBy, i18n.T(i18n.AutoNoBy))
	}
	checkCaps(&result, context)
	if strict && candidate.Memory.Type == model.TypeDecision {
		result.reject(RuleType, i18n.T(i18n.AutoNoDecision))
	}
	checkSources(&result, candidate, context, strict)
	// R3 는 맨 끝이다 — 규칙 판에 이미 걸렸으면 바깥 서버에 묻지 않는다.
	checkSupport(&result, candidate, context)
	return result
}

func (r *Result) reject(rule, reason string) {
	r.Rejects = append(r.Rejects, Finding{Rule: rule, Reason: reason})
}

// checkQuotes 는 R1 이다. (가)는 주면 보고, (나)는 1~3개가 꼭 있어야 한다.
func checkQuotes(result *Result, candidate Candidate, haveRecord, strict bool, talk string) {
	quotes := candidate.Quotes
	if len(quotes) == 0 {
		if strict {
			result.reject(RuleQuote, i18n.T(i18n.AutoQuoteNeeded))
		}
		return
	}
	if len(quotes) > quoteMax {
		result.reject(RuleQuote, i18n.T(i18n.AutoQuoteTooMany, len(quotes), quoteMax))
		return
	}
	for _, quote := range quotes {
		folded := Fold(quote)
		if utf8.RuneCountInString(folded) < quoteMinRunes {
			result.reject(RuleQuote, i18n.T(i18n.AutoQuoteShort, shortText(quote), quoteMinRunes))
			continue
		}
		if haveRecord && !strings.Contains(talk, folded) {
			result.reject(RuleQuote, i18n.T(i18n.AutoQuoteMissing, shortText(quote)))
		}
	}
}

// checkFragments 는 R2 다. 숫자·경로·날짜·id 조각이 대화 기록이나 저장소에 있어야 한다.
// 기록이 없으면 경로·id 만 저장소로 대조하고, 못 본 조각 수를 알린다.
func checkFragments(result *Result, memory *model.Memory, context Context, all string) {
	text := memory.Title + "\n" + memory.Summary + "\n" + memory.Body
	unchecked := 0
	for _, piece := range Fragments(text) {
		if context.Record != nil && strings.Contains(all, Fold(piece.Text)) {
			continue
		}
		if piece.Kind == KindPath && pathExists(context.Root, piece.Text) {
			continue
		}
		if piece.Kind == KindID && memoryExists(context.StoreDir, piece.Text) {
			continue
		}
		if context.Record == nil && (piece.Kind == KindNumber || piece.Kind == KindDate || piece.Kind == KindURL) {
			unchecked++
			continue
		}
		result.reject(RuleFragment, i18n.T(i18n.AutoFragmentMissing, shortText(piece.Text)))
	}
	if unchecked > 0 {
		result.Warnings = append(result.Warnings, i18n.T(i18n.AutoFragmentUnchecked, unchecked))
	}
}

// problemer 는 판정을 못 받은 까닭을 말해 주는 판정자다 (llm.Judge). 없어도 된다.
type problemer interface{ Problem() string }

// checkSupport 는 R3 다. 판정자가 없으면 조용히 건너뛴다 — LLM 주소가 없는
// 기계에서는 규칙 관문만 돈다 (U5). 앞 규칙에 이미 걸렸으면 묻지 않는다 — 어차피
// 거절이라 바깥 서버에 한 판(1초대)을 쓸 까닭이 없다. 판정을 한 번 못 받으면
// 남은 근거도 묻지 않고 경고 한 줄로 끝낸다 (재시도 없음).
func checkSupport(result *Result, candidate Candidate, context Context) {
	if context.Judge == nil || result.Rejected() {
		return
	}
	for _, quote := range candidate.Quotes {
		supported, ok := context.Judge.Supports(quote, candidate.Memory.Summary)
		if !ok {
			warning := i18n.T(i18n.AutoJudgeSkipped)
			if why, has := context.Judge.(problemer); has && why.Problem() != "" {
				warning = i18n.T(i18n.AutoJudgeSkippedWhy, why.Problem())
			}
			result.Warnings = append(result.Warnings, warning)
			return
		}
		if !supported {
			result.reject(RuleSupport, i18n.T(i18n.AutoNotSupported, shortText(quote)))
		}
	}
}

// checkCaps 는 R6 이다. 0 이하로 적으면 그 상한은 끈 것으로 본다.
func checkCaps(result *Result, context Context) {
	settings := context.Settings
	if settings.PerSession > 0 && context.SessionAdds >= settings.PerSession {
		result.reject(RuleCap, i18n.T(i18n.AutoCapSession, settings.PerSession))
	}
	if settings.PerDay > 0 && context.DayAdds >= settings.PerDay {
		result.reject(RuleCap, i18n.T(i18n.AutoCapDay, settings.PerDay))
	}
}

// checkSources 는 R8 이다. 부르는 쪽이 준 근거가 하나 이상이고, `file:` 은 실제로
// 있어야 한다. (나)는 scope 도 그 저장소 vocab 목록 안이어야 한다.
func checkSources(result *Result, candidate Candidate, context Context, strict bool) {
	sources := candidate.Memory.Sources
	if len(sources) == 0 {
		result.reject(RuleSources, i18n.T(i18n.AutoSourcesNeeded))
	}
	for _, source := range sources {
		if !strings.HasPrefix(source, model.SourceFile) {
			continue
		}
		target := strings.TrimSpace(strings.TrimPrefix(source, model.SourceFile))
		if !pathExists(context.Root, target) {
			result.reject(RuleSources, i18n.T(i18n.AutoSourceDead, shortText(target)))
		}
	}
	if strict && len(context.Scopes) > 0 && !contains(context.Scopes, candidate.Memory.Scope) {
		result.reject(RuleSources, i18n.T(i18n.AutoScopeUnknown, candidate.Memory.Scope))
	}
}

// lineSuffix 는 `파일:줄` · `#앵커` 꼬리다. 있나 볼 때는 떼고 본다.
var lineSuffix = regexp.MustCompile(`(:\d+(-\d+)?|#.*)$`)

// pathExists 는 저장소 뿌리 기준 상대 경로가 실제로 있는지다. 뿌리 밖은 없는 것으로
// 친다 — 자동 기억이 기계의 아무 파일이나 근거로 대면 안 된다.
func pathExists(root, path string) bool {
	if root == "" || path == "" {
		return false
	}
	path = lineSuffix.ReplaceAllString(strings.TrimSpace(path), "")
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return false
	}
	full := filepath.Join(root, filepath.FromSlash(strings.ReplaceAll(path, `\`, "/")))
	relative, err := filepath.Rel(root, full)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return false
	}
	_, err = os.Stat(full)
	return err == nil
}

func memoryExists(storeDir, id string) bool {
	if storeDir == "" || !model.IsID(id) {
		return false
	}
	_, err := os.Stat(filepath.Join(storeDir, filepath.FromSlash(model.StorePath(id))))
	return err == nil
}

// shortText 는 화면·log.md 에 되돌려 찍는 조각이다. 길면 자르고 한 줄로 만든다.
func shortText(text string) string {
	text = strings.Join(strings.Fields(text), " ")
	runes := []rune(text)
	if len(runes) > 40 {
		return string(runes[:40]) + "…"
	}
	return text
}

func contains(list []string, value string) bool {
	for _, item := range list {
		if item == value {
			return true
		}
	}
	return false
}
