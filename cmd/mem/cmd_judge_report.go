package main

// mem judge report — 서브에이전트 보고 속 결정 문장을 같은 scope 의 살아 있는 결정과 NLI 로
// 대조한다 (v1.2 적용 설계 4절). **확신 반대만** 찍고 나머지는 셈만 낸다. 안내일 뿐이라
// 종료 코드는 늘 0 이다 (사용 오류 · scope 를 못 고른 것만 다르다).
//
// 결정 문장 뽑기는 JudgeModel 측정 코드(use_sites/reports.py)의 extract_claims 를 그대로
// 옮긴 것이다. 규칙을 바꾸면 두 판이 갈린다 — 옮김 검사 시험(TestReportClaimsMatchPython)이 잡는다.

import (
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/llm"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/review"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/search"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
)

// 결정 문장 뽑기의 손잡이다. 값은 reports.py 와 같다.
const (
	reportClaimMin       = 8   // 문장 최소 글자(룬)
	reportClaimMax       = 200 // 문장 최대 글자(룬)
	reportClaimsMax      = 12  // 보고 하나에서 뽑는 문장 수 상한
	reportRivalsPerClaim = 3   // 문장마다 견줄 기존 결정 수
	reportPairsDefault   = 36  // 쌍 상한 기본값 (≈ 7초)
	reportShowRunes      = 60  // 찍는 줄에서 문장·요약을 자르는 길이
)

// 문장 종류 — JSON 의 kind 칸이다.
const (
	claimTable   = "table"
	claimDecided = "decided"
	claimMemAdd  = "mem-add"
)

var (
	reportDecideWords = []string{"정한 것", "정한것", "결정"}
	reportDecideHead  = []string{"결정", "정한", "정함"}
	reportWhyHead     = []string{"까닭", "근거", "이유", "왜"}

	// 파이썬 \s·\d 는 유니코드라 Go 쪽은 \p{Zs}·\p{Nd} 를 더해 맞춘다.
	reportMemID     = regexp.MustCompile(`\b(20\d{6}-[0-9a-f]{8})\b`)
	reportDecided   = regexp.MustCompile(`^[\s\p{Zs}]*(?:[-*][\s\p{Zs}]+)?(?:\*\*)?정함(?:\*\*)?[\s\p{Zs}]*[:：][\s\p{Zs}]*(.+)$`)
	reportMemAdd    = regexp.MustCompile(`mem add\b.*--type[ =]+decision`)
	reportSummary   = regexp.MustCompile(`--(summary|title)[ =]+(?:"([^"]+)"|'([^']+)')`)
	reportSeparator = regexp.MustCompile(`^\|?[\s\p{Zs}]*:?-{2,}`)
	reportEmphasis  = regexp.MustCompile("\\*\\*|__|`")
	reportLink      = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	reportListMark  = regexp.MustCompile(`^[\s\p{Zs}]*(?:[-*]|\p{Nd}+[.)])[\s\p{Zs}]+`)
	reportNumber    = regexp.MustCompile(`^[#\p{Nd}.\s\p{Zs}]*$`)
)

// reportClaim 은 보고에서 뽑은 결정 문장 하나다.
type reportClaim struct {
	Text string `json:"text"`
	Kind string `json:"kind"`
}

// extractClaims 는 보고 전문 → 결정 문장들이다 (8~200자 · 12개까지 · 겹침 없음).
// ① 머리에 결정 칸이 있거나 제목이 결정인 표 ② `정함 :` 줄 ③ `mem add --type decision` 의 --summary(없으면 --title).
func extractClaims(text string) []reportClaim {
	lines := reportLines(text)
	found := []reportClaim{}
	for _, table := range reportTables(lines) {
		titled := containsAny(table.above, reportDecideWords)
		for _, claim := range tableClaims(table.header, table.rows, titled) {
			found = append(found, reportClaim{Text: claim, Kind: claimTable})
		}
	}
	for _, line := range lines {
		if match := reportDecided.FindStringSubmatch(line); match != nil {
			found = append(found, reportClaim{Text: cleanCell(match[1]), Kind: claimDecided})
		}
		if reportMemAdd.MatchString(line) {
			// 파이썬 dict 처럼 같은 칸이 여럿이면 뒤의 것이 남는다.
			got := map[string]string{}
			for _, match := range reportSummary.FindAllStringSubmatch(line, -1) {
				got[match[1]] = match[2] + match[3]
			}
			value := got["summary"]
			if value == "" {
				value = got["title"]
			}
			if value != "" {
				found = append(found, reportClaim{Text: cleanCell(value), Kind: claimMemAdd})
			}
		}
	}
	out, seen := []reportClaim{}, map[string]bool{}
	for _, claim := range found {
		size := utf8.RuneCountInString(claim.Text)
		if size < reportClaimMin || size > reportClaimMax || seen[claim.Text] {
			continue
		}
		seen[claim.Text] = true
		out = append(out, claim)
	}
	if len(out) > reportClaimsMax {
		out = out[:reportClaimsMax]
	}
	return out
}

// reportLines 는 파이썬 str.splitlines 와 같게 줄을 나눈다 (\r\n · \r · \n 과 유니코드 줄 끝).
func reportLines(text string) []string {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := []string{}
	start := 0
	for at, letter := range text {
		switch letter {
		case '\n', '\r', '\v', '\f', 0x1c, 0x1d, 0x1e, 0x85, 0x2028, 0x2029:
			lines = append(lines, text[start:at])
			start = at + utf8.RuneLen(letter)
		}
	}
	if start < len(text) {
		lines = append(lines, text[start:])
	}
	return lines
}

// cleanCell 은 꾸밈(굵게 · 코드 · 링크 · 목록 머리)을 벗기고 띄어쓰기를 한 칸으로 접는다.
func cleanCell(text string) string {
	text = reportEmphasis.ReplaceAllString(text, "")
	text = reportLink.ReplaceAllString(text, "$1")
	text = reportListMark.ReplaceAllString(text, "")
	return strings.Join(strings.Fields(text), " ")
}

// tableCells 는 표 줄 하나를 칸으로 나눈다. 양 끝 `|` 는 뗀다.
func tableCells(line string) []string {
	parts := strings.Split(strings.Trim(strings.TrimSpace(line), "|"), "|")
	for at := range parts {
		parts[at] = strings.TrimSpace(parts[at])
	}
	return parts
}

func isNumberCell(cell string) bool {
	return reportNumber.MatchString(cleanCell(cell))
}

// reportTable 은 표 하나다 — 머리줄 · 몸줄들 · 표 바로 위 글줄(제목 판단용).
type reportTable struct {
	header []string
	rows   [][]string
	above  string
}

// reportTables 는 `|` 로 시작하는 줄이 이어진 덩어리를 표로 묶는다. 구분 줄(---)은 뺀다.
// 머리줄과 몸줄이 하나 이상 있어야 표다.
func reportTables(lines []string) []reportTable {
	found := []reportTable{}
	isRow := func(line string) bool {
		return strings.HasPrefix(strings.TrimLeftFunc(line, unicode.IsSpace), "|")
	}
	for at := 0; at < len(lines); {
		if !isRow(lines[at]) {
			at++
			continue
		}
		start := at
		for at < len(lines) && isRow(lines[at]) {
			at++
		}
		block := [][]string{}
		for _, line := range lines[start:at] {
			bare := strings.TrimSpace(strings.Trim(strings.TrimSpace(line), "|"))
			if !reportSeparator.MatchString(bare) {
				block = append(block, tableCells(line))
			}
		}
		above := ""
		for back := start - 1; back >= 0; back-- {
			if strings.TrimSpace(lines[back]) != "" {
				above = lines[back]
				break
			}
		}
		if len(block) >= 2 {
			found = append(found, reportTable{header: block[0], rows: block[1:], above: above})
		}
	}
	return found
}

// tableClaims 는 표 하나 → 문장들이다. 머리줄에 「결정」·「정한」 칸이 있으면 그 칸(앞에 주어 칸 하나),
// 없고 제목이 결정이면 쓸 만한 앞 두 칸을 « : » 로 잇는다. 「까닭」 칸 · 숫자 칸 · 빈 칸은 안 쓴다.
func tableClaims(header []string, rows [][]string, titled bool) []string {
	why := map[int]bool{}
	pick := -1
	for at, head := range header {
		head = cleanCell(head)
		if containsAny(head, reportWhyHead) {
			why[at] = true
			continue
		}
		if pick < 0 && containsAny(head, reportDecideHead) {
			pick = at
		}
	}
	if pick < 0 && !titled {
		return nil
	}
	out := []string{}
	for _, row := range rows {
		usable := []int{}
		for at, cell := range row {
			plain := cleanCell(cell)
			if why[at] || isNumberCell(cell) || plain == "" || plain == "—" || plain == "-" {
				continue
			}
			usable = append(usable, at)
		}
		parts := []string{}
		if pick >= 0 {
			if !containsInt(usable, pick) {
				continue
			}
			if len(usable) > 0 && usable[0] < pick {
				parts = append(parts, row[usable[0]])
			}
			parts = append(parts, row[pick])
		} else {
			for _, at := range usable[:min(2, len(usable))] {
				parts = append(parts, row[at])
			}
		}
		for at := range parts {
			parts[at] = cleanCell(parts[at])
		}
		out = append(out, strings.Join(parts, " : "))
	}
	return out
}

func containsAny(text string, words []string) bool {
	for _, word := range words {
		if strings.Contains(text, word) {
			return true
		}
	}
	return false
}

func containsInt(list []int, want int) bool {
	for _, one := range list {
		if one == want {
			return true
		}
	}
	return false
}

// errReportScope 는 scope 를 못 고른 것이다 — 「--scope 를 준다」.
var errReportScope = errors.New("scope")

// reportScope 는 대조할 scope 다. --scope 를 주면 그것, 아니면 보고에 나온 mem id 들의 scope 가
// 하나일 때 그것이다. 못 읽는 id 는 건너뛴다. 없거나 여럿이면 errReportScope 와 본 scope 들이다.
func reportScope(given, text string, lookup func(string) *model.Memory) (string, []string, error) {
	if scope := strings.TrimSpace(given); scope != "" {
		return scope, nil, nil
	}
	seen := map[string]bool{}
	for _, match := range reportMemID.FindAllStringSubmatch(text, -1) {
		if memory := lookup(match[1]); memory != nil && memory.Scope != "" {
			seen[memory.Scope] = true
		}
	}
	scopes := make([]string, 0, len(seen))
	for scope := range seen {
		scopes = append(scopes, scope)
	}
	sort.Strings(scopes)
	if len(scopes) == 1 {
		return scopes[0], scopes, nil
	}
	return "", scopes, errReportScope
}

// reportPair 는 물은 쌍 하나다 (--json 의 pairs).
type reportPair struct {
	Claim    string  `json:"claim"`
	ID       string  `json:"id"`
	Evidence string  `json:"evidence"`
	Letter   string  `json:"letter,omitempty"`
	Prob     float64 `json:"prob,omitempty"`
	Unsure   bool    `json:"unsure,omitempty"`
	Problem  string  `json:"problem,omitempty"`
}

// reportCounts 는 끝 셈 줄이다.
type reportCounts struct {
	Asked      int `json:"asked"`
	Contradict int `json:"contradict"`
	Support    int `json:"support"`
	Unrelated  int `json:"unrelated"`
	Unsure     int `json:"unsure"`
	Failed     int `json:"failed"`
	Refused    int `json:"refused"`
	Skipped    int `json:"skipped"`
}

// judgeReportJSON 은 `judge report --json` 이다.
type judgeReportJSON struct {
	Scope   string        `json:"scope,omitempty"`
	Skipped string        `json:"skipped,omitempty"`
	Claims  []reportClaim `json:"claims"`
	Pairs   []reportPair  `json:"pairs"`
	Counts  reportCounts  `json:"counts"`
}

// judgeReport 는 `mem judge report --file <md|-> [--scope] [--json] [--pairs]` 다.
func judgeReport(parsed *options) int {
	path := strings.TrimSpace(parsed.text("file"))
	if path == "" {
		return fail(i18n.T(i18n.JudgeReportUsage))
	}
	limit, ok := nliCountOf(parsed, "pairs")
	if !ok {
		return fail(i18n.T(i18n.JudgeReportUsage))
	}
	if limit == 0 {
		limit = reportPairsDefault
	}
	text, err := reportText(path)
	if errors.Is(err, errReportTooBig) {
		return fail(i18n.T(i18n.JudgeReportTooBig, reportReadLimit>>20))
	}
	if err != nil {
		return fail(i18n.T(i18n.JudgeFileUnreadable, path))
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	row := judgeReportJSON{Claims: extractClaims(text), Pairs: []reportPair{}}
	if len(row.Claims) == 0 {
		row.Skipped = "no-claims"
		return reportDone(parsed, row, i18n.T(i18n.JudgeReportNoClaims))
	}
	scope, seen, err := reportScope(parsed.text("scope"), text, lookupOf(opened))
	if err != nil {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.JudgeReportNeedScope, orDash(strings.Join(seen, ", "))))
		return exitCheck
	}
	row.Scope = scope
	judge := reportJudge(repository)
	if judge == nil {
		row.Skipped = "no-nli"
		return reportDone(parsed, row, i18n.T(i18n.JudgeReportNoServer))
	}
	repos, err := openSources(parsed)
	if err != nil {
		return exitFor(err)
	}
	defer repos.close()
	lines := []string{}
	for _, claim := range row.Claims {
		for _, hit := range reportRivals(repos, scope, claim.Text) {
			if row.Counts.Asked >= limit {
				row.Counts.Skipped++
				continue
			}
			pair := askReportPair(judge, claim.Text, hit, &row.Counts)
			row.Pairs = append(row.Pairs, pair)
			if pair.Letter == llm.LetterContradict && !pair.Unsure && pair.Problem == "" {
				lines = append(lines, i18n.T(i18n.JudgeReportLine, safe.Summary(claim.Text, reportShowRunes),
					pair.ID, safe.Summary(pair.Evidence, reportShowRunes), shortProb(pair.Prob)))
			}
		}
	}
	if parsed.flags["json"] {
		return printJSON(row)
	}
	for _, line := range lines {
		fmt.Println(line)
	}
	fmt.Println(reportCountLine(row))
	return exitOK
}

// reportReadLimit 는 보고 글 상한(1 MiB)이다. 보고 한 편으로는 넉넉하고, 잘못 준 큰 파일에 묶이지 않는다.
const reportReadLimit = 1 << 20

// errReportTooBig 는 보고 글이 reportReadLimit 를 넘은 것이다.
var errReportTooBig = errors.New("report-too-big")

// reportText 는 --file 의 글이다. `-` 면 표준입력을 읽는다. 상한을 넘으면 errReportTooBig 다.
func reportText(path string) (string, error) {
	var input io.Reader = os.Stdin
	if path != "-" {
		file, err := os.Open(path)
		if err != nil {
			return "", err
		}
		defer file.Close()
		input = file
	}
	data, err := io.ReadAll(io.LimitReader(input, reportReadLimit+1))
	if err != nil {
		return "", err
	}
	if len(data) > reportReadLimit {
		return "", errReportTooBig
	}
	return bodyText(data), nil
}

// reportDone 은 대조 없이 끝나는 길이다 (문장 없음 · 서버 없음). 한 줄 알리고 0 이다.
func reportDone(parsed *options, row judgeReportJSON, line string) int {
	if parsed.flags["json"] {
		return printJSON(row)
	}
	fmt.Println(line)
	return exitOK
}

// reportJudge 는 NLI 한 단만 쓰는 판정기다. nli_url 이 없거나 /health 가 안 닿으면 nil 이다.
// 꼬리표 떼기 · 비밀 꼴 거절 · 판정 기록은 review --nli 와 같은 것을 쓴다.
func reportJudge(repository *config.Repository) *llm.Judge {
	settings, _ := config.LoadLLM(config.LLMPath())
	nli := llm.NewNLI(settings)
	if nli == nil {
		return nil
	}
	if health := review.HealthOf(settings.NLIURL); health != nil && health() != nil {
		return nil
	}
	scanner := secret.ForConfig(repository.Config.Secret)
	return review.NLIJudge(nli, judgeDir(repository), func(text string) bool { return scanner.ScanText(text) != nil })
}

// reportRivals 는 문장 하나와 견줄 기존 결정이다 — `mem search` 와 같은 검색 함수로 같은 scope 의
// 살아 있는 결정 위 3개. 0건 질의 기록(MissPath)은 남기지 않는다.
func reportRivals(repos *opened, scope, claim string) []search.Hit {
	options := search.Options{
		Sources: repos.sources, Query: claim,
		Filter:    index.Filter{Types: []string{model.TypeDecision}, Scope: scope},
		Stopwords: repos.settings.Stopword.Words, Synonym: repos.settings.Synonym,
		RRFK: repos.settings.Search.RRFK, BonusCap: repos.settings.Search.BonusCap,
		Search: repos.settings.Search, Embed: repos.settings.Embed,
		RepoDir: repos.dirOf(), Types: config.TypesIn(repos.dirOf()),
		Limit: reportRivalsPerClaim,
	}
	result, err := search.Search(options)
	if err != nil || result == nil {
		return nil
	}
	if len(result.Hits) > reportRivalsPerClaim {
		return result.Hits[:reportRivalsPerClaim]
	}
	return result.Hits
}

// askReportPair 는 evidence = 기존 결정 요약, claim = 보고 문장으로 한 번 묻고 셈을 올린다.
func askReportPair(judge *llm.Judge, claim string, hit search.Hit, counts *reportCounts) reportPair {
	evidence := hit.Summary
	if strings.TrimSpace(evidence) == "" {
		evidence = hit.Title
	}
	evidence = quality.StripTags(evidence)
	pair := reportPair{Claim: claim, ID: hit.ID, Evidence: evidence}
	counts.Asked++
	verdict, err := judge.Support(evidence, claim)
	switch {
	case errors.Is(err, llm.ErrRefused):
		counts.Refused++
		counts.Failed++
		pair.Problem = err.Error()
	case err != nil:
		counts.Failed++
		pair.Problem = err.Error()
	default:
		pair.Letter, pair.Prob, pair.Unsure = verdict.Letter, verdict.Prob, verdict.Unsure
		switch {
		case verdict.Unsure || verdict.Letter == "":
			counts.Unsure++
		case verdict.Letter == llm.LetterContradict:
			counts.Contradict++
		case verdict.Letter == llm.LetterSupport:
			counts.Support++
		default:
			counts.Unrelated++
		}
	}
	return pair
}

// reportCountLine 은 끝 셈 한 줄이다.
func reportCountLine(row judgeReportJSON) string {
	c := row.Counts
	return i18n.T(i18n.JudgeReportCount, row.Scope, len(row.Claims), c.Asked, c.Contradict, c.Support,
		c.Unrelated, c.Unsure, c.Failed, c.Skipped)
}

// shortProb 는 0.91 → .91 이다.
func shortProb(prob float64) string {
	return strings.TrimPrefix(fmt.Sprintf("%.2f", prob), "0")
}
