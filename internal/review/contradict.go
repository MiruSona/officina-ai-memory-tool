package review

// C19 `contradict-candidate` — 서로 어긋날 수 있는 짝 (점검·정리 설계 2절 ⑤).
//
// 숫자 어긋남(C13)은 quality 가 잡는다. 여기는 숫자 없는 **부정 모순**이다 —
// 「기본 켬」↔「기본 끔」, 「한다」↔「안 한다」. 짝 후보는 quality 가 중복 판정
// 걸음에서 이미 뽑아 둔 닮은 짝(`RepoReport.Near`)을 받는다. 후보 뽑기를 다시 안 한다.
//
// 판정은 두 단이다.
//
//	① 규칙 층(`llm.RuleJudge`) — 프로세스 안의 계산이라 상한 없이 모든 짝에 돈다.
//	② NLI 단 — `--nli` 를 줬고 시작할 때 /health 가 통과했을 때만. 상한이 있다.
//
// quality·lint 가 llm 을 들여오면 「lint LLM 0」 경계가 흐려지므로 review 쪽에만 둔다
// (같은 설계 3절 (b)). 도구는 판정하지 않는다 — 반대 판정이 확신선 이상인 짝을 큐에
// 올릴 뿐이고, 어느 쪽이 맞는지는 사람이 정한다.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/llm"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
)

const (
	// DefaultNLIPairs 는 한 번에 NLI 에 물을 짝 수다 (`--nli-pairs`). p95 414ms 로 치면
	// 최악 약 41초다.
	DefaultNLIPairs = 100
	// DefaultNLIPerScope 는 scope 하나가 쓸 수 있는 NLI 짝 수다. 큰 scope 하나가 상한을
	// 다 먹으면 나머지 scope 는 규칙 층만 보게 된다.
	DefaultNLIPerScope = 20
	// healthTimeout 은 시작할 때 한 번 묻는 /health 의 기다림이다.
	healthTimeout = time.Second
	// healthPath 는 NLI 서버의 살아 있음 확인 자리다.
	healthPath = "/health"
)

// NLIOptions 는 C19 의 NLI 단이다. `--nli` 를 줬을 때만 만든다.
type NLIOptions struct {
	// Judge 는 NLI 단만 도는 판정기다 (Client nil · Only nli). nil 이거나 NLI 가 nil 이면
	// llm.toml 에 nli_url 이 없는 것이다.
	Judge *llm.Judge
	// Health 는 시작할 때 한 번 부른다. nil 이면 묻지 않고 산 것으로 본다.
	Health func() error
	// Pairs 는 전체 상한, PerScope 는 scope 당 상한이다. 0 이하면 기본값이다.
	Pairs, PerScope int
}

// NLIJudge 는 C19 가 쓰는 판정기다. SemIf 는 안 부른다(Client nil · 설계 결정 12).
// 비밀 꼴 거절(refuse)과 판정 기록 폴더(dir)는 부르는 쪽이 기존 것을 그대로 넘긴다.
func NLIJudge(nli *llm.NLIClient, dir string, refuse func(string) bool) *llm.Judge {
	if nli == nil {
		return nil
	}
	return &llm.Judge{NLI: nli, Dir: dir, Refuse: refuse, Only: []string{llm.StageNLI}}
}

// HealthOf 는 nli_url 의 /health 를 한 번 묻는 함수다. 주소가 비면 nil 이다.
func HealthOf(nliURL string) func() error {
	if strings.TrimSpace(nliURL) == "" {
		return nil
	}
	address := strings.TrimRight(nliURL, "/") + healthPath
	return func() error {
		ctx, cancel := context.WithTimeout(context.Background(), healthTimeout)
		defer cancel()
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, address, nil)
		if err != nil {
			return errors.New("bad-url")
		}
		response, err := http.DefaultClient.Do(request)
		if err != nil {
			// 주소·키가 오류 글에 섞여 「못 본 것」에 찍히지 않게 까닭은 짧게만 준다.
			return errors.New("unreachable")
		}
		response.Body.Close()
		if response.StatusCode != http.StatusOK {
			return fmt.Errorf("http-%d", response.StatusCode)
		}
		return nil
	}
}

// contradictTypes 는 C19 가 보는 종류다. 결론·주의·사실·방법만 어긋남이 뜻을 갖는다.
var contradictTypes = map[string]bool{
	model.TypeDecision: true, model.TypeCaution: true, model.TypeFact: true, model.TypeHowto: true,
}

// addContradicts 는 C19 걸림을 found.ByID 에 붙인다. 걸림은 작은 id 쪽에 달고 Related 에
// 큰 id 를 넣는다 (C20 과 같은 꼴).
func (r *Report) addContradicts(found *quality.RepoReport, byID map[string]*model.Memory, options Options) {
	// 서버 확인은 짝이 없어도 시작할 때 한 번 한다 — `--nli` 를 준 사람은 서버가 죽었는지 알아야 한다.
	run := r.nliFor(options)
	pairs := contradictPairs(found.Near, byID, scopeSet(options.Scopes), options.Now)
	for _, pair := range pairs {
		left, right := byID[pair.Left], byID[pair.Right]
		// 문장마다 꼬리표(`왜 :` 따위)를 떼고 판정한다 — NLI 학습·측정이 뗀 글로 했다. 까닭 줄에는 원문을 보인다.
		evidence, claim := quality.StripTags(pair.LeftText), quality.StripTags(pair.RightText)
		// 근거 = 옛 기억 쪽 문장, 주장 = 새 기억 쪽 문장 (설계 ⑤).
		if right.Date < left.Date {
			evidence, claim = claim, evidence
		}
		if strings.TrimSpace(evidence) == "" || strings.TrimSpace(claim) == "" {
			continue
		}
		label, reason, ok := llm.RuleJudge(evidence, claim)
		if ok {
			if label == llm.LetterContradict {
				addContradict(found, pair, "규칙 "+reason)
			}
			// 무관(C)도 규칙이 확신한 답이라 NLI 에 다시 안 묻는다.
			continue
		}
		if run == nil || !run.take(left.Scope) {
			continue
		}
		verdict, err := run.ask(evidence, claim)
		if err != nil {
			continue
		}
		if verdict.Letter == llm.LetterContradict && !verdict.Unsure {
			addContradict(found, pair, fmt.Sprintf("NLI 반대 %.2f", verdict.Prob))
		}
	}
	r.noteNLILimits()
}

// nliRun 은 review 한 번의 NLI 묻기다 — 상한(짝 수 · scope 당 짝 수)과 호출 셈을 한곳에 둔다.
type nliRun struct {
	judge             *llm.Judge
	pairCap, scopeCap int
	perScope          map[string]int
	summary           *NLISummary
}

// NLISummary 는 review 한 번에 NLI 를 얼마나 물었는지다. `--nli` 를 줬고 서버가 살아 있을 때만 생긴다.
type NLISummary struct {
	// Pairs 는 상한에서 쓴 짝 수, Calls 는 /judge 를 부른 횟수다.
	Pairs int `json:"pairs"`
	Calls int `json:"calls"`
	// Skipped 는 상한에 걸려 못 물은 짝, Failed 는 답을 못 받은 물음 수다.
	Skipped int `json:"skipped"`
	Failed  int `json:"failed"`
}

// take 는 짝 하나를 상한에서 뗀다. 상한에 걸리면 거짓이고 못 물은 짝으로 센다.
func (n *nliRun) take(scope string) bool {
	if n.summary.Pairs >= n.pairCap || n.perScope[scope] >= n.scopeCap {
		n.summary.Skipped++
		return false
	}
	n.summary.Pairs++
	n.perScope[scope]++
	return true
}

// ask 는 한 번 묻는다. 부른 횟수와 못 받은 수를 센다.
func (n *nliRun) ask(evidence, claim string) (llm.Verdict, error) {
	n.summary.Calls++
	verdict, err := n.judge.Support(evidence, claim)
	if err != nil {
		n.summary.Failed++
	}
	return verdict, err
}

// noteNLILimits 는 상한·실패를 「못 본 것」에 남긴다. 큐마다 부르면 같은 줄이 겹치니 마지막 값으로
// 한 번만 둔다.
func (r *Report) noteNLILimits() {
	if r.run == nil {
		return
	}
	summary := r.run.summary
	kept := r.Notes[:0]
	for _, note := range r.Notes {
		if !strings.HasPrefix(note, nliCapNote) && !strings.HasPrefix(note, nliFailNote) {
			kept = append(kept, note)
		}
	}
	r.Notes = kept
	if summary.Skipped > 0 {
		r.note(fmt.Sprintf("%s(전체 %d쌍 · scope 당 %d쌍)에 걸려 %d쌍은 규칙 층만 봤다 (NLI 에 안 물었다)",
			nliCapNote, r.run.pairCap, r.run.scopeCap, summary.Skipped))
	}
	if summary.Failed > 0 {
		r.note(fmt.Sprintf("%s %d번 답을 못 줬다 (그 짝은 규칙 층만 봤다)", nliFailNote, summary.Failed))
	}
}

// 「못 본 것」 줄 머리 — noteNLILimits 가 옛 줄을 찾아 바꾸는 열쇠다.
const (
	nliCapNote  = "NLI 상한"
	nliFailNote = "NLI 가"
)

// nliFor 는 이번 판에 쓸 NLI 묻기다. `--nli` 를 안 줬으면 조용히 nil 이고, 줬는데 못 쓰면
// 「못 본 것」에 한 줄 남기고 nil 이다. /health 는 첫 부름에서 한 번만 묻는다 — 뒤 큐는 같은 답을 쓴다.
func (r *Report) nliFor(review Options) *nliRun {
	if r.runAsked {
		return r.run
	}
	r.runAsked = true
	options := review.NLI
	if options == nil {
		return nil
	}
	if options.Judge == nil || options.Judge.NLI == nil {
		r.note("NLI 를 못 써서 규칙 층만 봤다 (llm.toml 에 nli_url 이 없다)")
		return nil
	}
	if options.Health != nil {
		if err := options.Health(); err != nil {
			r.note("NLI 를 못 써서 규칙 층만 봤다 (/health " + err.Error() + ")")
			return nil
		}
	}
	pairCap, scopeCap := nliCaps(options)
	// `--scope` 로 scope 하나만 볼 때는 scope 당 상한이 지킬 다른 scope 가 없다 — 전체 상한만 건다.
	// 안 그러면 scope 하나를 볼 때도 전체 상한(100)이 아니라 scope 당 상한(20)에서 멈춘다.
	if len(scopeSet(review.Scopes)) == 1 && options.PerScope <= 0 {
		scopeCap = pairCap
	}
	r.NLI = &NLISummary{}
	r.run = &nliRun{judge: options.Judge, pairCap: pairCap, scopeCap: scopeCap, perScope: map[string]int{},
		summary: r.NLI}
	return r.run
}

func nliCaps(options *NLIOptions) (int, int) {
	pairCap, scopeCap := DefaultNLIPairs, DefaultNLIPerScope
	if options != nil && options.Pairs > 0 {
		pairCap = options.Pairs
	}
	if options != nil && options.PerScope > 0 {
		scopeCap = options.PerScope
	}
	return pairCap, scopeCap
}

// contradictPairs 는 C19 에 물을 짝이다 — 같은 scope · 둘 다 살아 있음 · 종류가
// decision·caution·fact·howto. (작은 id, 큰 id) 로 한 번씩, 그 차례로 놓는다 —
// 상한에 걸릴 때 어느 짝이 밀리는지가 매번 같아야 한다.
func contradictPairs(near []quality.NearPair, byID map[string]*model.Memory, scopes map[string]bool,
	now time.Time) []quality.NearPair {
	seen := map[[2]string]bool{}
	out := []quality.NearPair{}
	for _, pair := range near {
		if pair.Right < pair.Left {
			pair.Left, pair.Right = pair.Right, pair.Left
			pair.LeftText, pair.RightText = pair.RightText, pair.LeftText
		}
		key := [2]string{pair.Left, pair.Right}
		if seen[key] || pair.Left == pair.Right {
			continue
		}
		left, right := byID[pair.Left], byID[pair.Right]
		if left == nil || right == nil || left.Scope != right.Scope || !inScopes(scopes, left) {
			continue
		}
		if !contradictTypes[left.Type] || !contradictTypes[right.Type] {
			continue
		}
		if !quality.Live(left, now) || !quality.Live(right, now) {
			continue
		}
		if strings.TrimSpace(pair.LeftText) == "" || strings.TrimSpace(pair.RightText) == "" {
			continue
		}
		seen[key] = true
		out = append(out, pair)
	}
	sort.Slice(out, func(a, b int) bool {
		if out[a].Left != out[b].Left {
			return out[a].Left < out[b].Left
		}
		return out[a].Right < out[b].Right
	})
	return out
}

// addContradict 는 C19 걸림 하나를 작은 id 쪽에 단다. 까닭에는 판정한 단과 두 문장을 넣는다 —
// 사람이 `mem show` 두 번 전에 어디가 어긋나는지 본다. 글은 itemOf 에서 safe 를 지난다.
func addContradict(found *quality.RepoReport, pair quality.NearPair, by string) {
	reason := fmt.Sprintf("`%s` 와 어긋날 수 있다 (%s) : 「%s」 ↔ 「%s」",
		pair.Right, by, pair.LeftText, pair.RightText)
	found.ByID[pair.Left] = append(found.ByID[pair.Left], quality.Finding{Rule: quality.RuleContradictCand,
		Level: quality.GradeCandidate, ID: pair.Left, Reason: reason, Related: []string{pair.Right}})
}
