package quality

import (
	"fmt"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// relativeDateWords 는 반년 뒤에 거짓말이 되는 말이다 (규칙 B09).
var relativeDateWords = []string{"어제", "지난주", "지난달", "요즘", "이번에", "며칠 전", "얼마 전"}

// recentTime 은 「최근」이 **시점을 주장하는** 자리다. 조사가 바로 붙은 꼴만 센다.
//
//	최근에 고쳤다 · 최근까지 · 최근은     시점이다 — 반년 뒤에 거짓말이 된다
//	최근 결정 · 최근 이슈 · 최근 조회     뒤 이름씨를 꾸미는 **갈래 이름**이다
//	최근 500건 · 최근건 · 최근성          개수·성질 말이다
//
// 옛 자는 「최근」이 들어가기만 하면 잡고 「최근 500건」만 빼 줬다. 그래서 이
// 저장소의 실기억 206건에서 오거절 세 건이 전부 「최근 결정」·「최근 이슈」·
// 「최근 조회」였다 (리뷰 B). B09 가 경고일 때는 넘어갔지만 **거절이 된 뒤로는
// 멀쩡한 기억을 막는다.** 거절 규칙은 놓치는 쪽이 낫다.
var recentTime = regexp.MustCompile(`최근(에|은|까지|부터|이다|이었|이라)`)

// unfixedWords 는 「아직 안 됐다」 표시다 (규칙 B10).
//
// **(2D) 맨 「나중에」는 뺐다.** 「나중에 다시 볼 때 아래를 먼저 본다」 같은
// 멀쩡한 글이 걸린다 — 대조군 20건 중 한 건이 그것뿐이었다. 미루겠다는 뜻이
// 분명한 꼴만 남긴다.
var unfixedWords = []string{"안 고침", "안고침", "미해결", "나중에 고친다", "나중에 한다",
	"나중에 본다", "나중으로", "다음 판으로 미룬다", "미룬다"}

// numberedStep 은 `1.` `2)` 같은 번호 목록이다. ①② 는 번호로 안 친다 (규칙 B07).
var numberedStep = regexp.MustCompile(`^\s*(\d+)[.)]\s+\S`)

// questionTail 은 결론이 아니라 물음으로 끝난 첫 줄이다 (규칙 B03).
var questionTail = []string{"?", "에 대해", "에 관해", "무엇인가", "인가", "일까"}

// multiSummaryShapes 는 「여러 결정을 한 건에 묶었다」는 요약 꼴이다 (규칙 M02).
var multiSummaryShapes = []*regexp.Regexp{
	regexp.MustCompile(`에서 정한 것`),
	regexp.MustCompile(`정한 것 (모음|목록)`),
	regexp.MustCompile(`확정\s*(값|—|-|:)`),
	regexp.MustCompile(`[0-9]+\s*(개|가지|건|편|종)(를|을)?\s*(정했다|정한다|골랐다|추렸다|견줘|비교)`),
	regexp.MustCompile(`(결정|규칙|항목|후보)\s*[0-9]+\s*(개|건|가지)`),
	regexp.MustCompile(`(다섯|넷|셋|여섯|일곱|여덟|아홉|열)(을|를)?\s*(견줘|비교|골라|정했다)`),
	regexp.MustCompile(`목록$`),
}

// checkBody 는 본문 규격(B 계열)을 본다.
func checkBody(m *model.Memory, opt Options) []Finding {
	found := []Finding{}
	add := func(rule, reason string, next ...string) {
		found = append(found, opt.finding(rule, m, reason, next...))
	}
	lines := bodyLines(m.Body)
	if real := substantialLines(m.Body); real < opt.Config.Quality.BodyMinLines {
		add(RuleBodyThin, fmt.Sprintf("본문이 실질 %d줄이다. %d줄 이상으로 왜·무엇을·다음에 뭘 을 적는다",
			real, opt.Config.Quality.BodyMinLines))
	}
	switch {
	case len(lines) > opt.Config.Quality.BodyMax:
		// 상한을 넘으면 거절이다. 설계 3-1 ①·사용법.md 가 「300줄을 넘으면
		// 오류」라고 적어 뒀는데 관문에는 경고밖에 없었다 (스트레스 시험 2절).
		add(RuleBodyMax, fmt.Sprintf("본문이 %d줄이다. %d줄이 상한이라 그대로는 못 넣는다",
			len(lines), opt.Config.Quality.BodyMax),
			"본문을 주제별로 나눠 mem add 를 여러 번 친다",
			"긴 원문은 파일로 두고 --sources file:<경로> 로 가리킨다")
	case len(lines) > opt.Config.Quality.BodyWarn:
		add(RuleBodyLong, fmt.Sprintf("본문이 %d줄이다. %d줄이 넘으면 쪼개는 편이 낫다 (%d줄이면 거절)",
			len(lines), opt.Config.Quality.BodyWarn, opt.Config.Quality.BodyMax))
	}
	if reason := firstLineProblem(lines); reason != "" {
		add(RuleFirstLineConclusion, reason)
	}
	if count := len(headings(m.Body)); count > headingMax {
		add(RuleHeadingsMax, fmt.Sprintf("`##` 절이 %d개다. %d개를 넘으면 한 기억에 여러 이야기가 들어 있다", count, headingMax))
	}
	for _, one := range sections(m.Body) {
		if len(one.Lines) == 0 {
			add(RuleEmptySection, fmt.Sprintf("절 `%s` 에 내용이 없다", one.Title))
		}
	}
	found = append(found, checkTypeBody(m, opt)...)
	if reason := summaryBodyGap(m); reason != "" {
		add(RuleSummaryBodyMatch, reason)
	}
	if word := relativeDate(m.Body); word != "" {
		add(RuleRelativeDate, fmt.Sprintf("본문에 「%s」 가 있다. 반년 뒤에 읽으면 거짓말이 되니 날짜를 적는다", word))
	}
	return found
}

const headingMax = 8

// firstLineProblem 은 첫 줄이 결론인지 본다 (규칙 B03).
func firstLineProblem(lines []string) string {
	first := ""
	for _, line := range lines {
		text := strings.TrimSpace(line)
		if text == "" || strings.HasPrefix(text, "#") {
			continue
		}
		first = text
		break
	}
	if first == "" {
		return ""
	}
	// 「결론 : 바이트로 자른다.」(14자)처럼 짧아도 마침표로 끝난 완결된 문장은
	// 좋은 결론이다. 글자 수만 보면 이런 한 줄이 경고로 걸린다 (실데이터 시험 C7).
	if count := utf8.RuneCountInString(first); count < firstLineMinRunes && !endsAsSentence(first) {
		return fmt.Sprintf("본문 첫 줄이 %d자다. 첫 줄에 결론을 %d자 이상 적는다 (한 문장으로 끝맺어도 된다)",
			count, firstLineMinRunes)
	}
	for _, tail := range questionTail {
		if strings.HasSuffix(first, tail) {
			return "본문 첫 줄이 물음으로 끝난다. 첫 줄은 결론이어야 한다"
		}
	}
	return ""
}

const (
	firstLineMinRunes = 20
	// firstLineFloor 는 아무리 문장으로 끝나도 이보다 짧으면 결론이 아니다.
	firstLineFloor = 10
)

// endsAsSentence 는 첫 줄이 맺음말로 끝난 문장인지다. 「…다.」 「…한다」 처럼
// 끝나면 짧아도 결론 한 줄로 본다.
func endsAsSentence(first string) bool {
	if utf8.RuneCountInString(first) < firstLineFloor {
		return false
	}
	for _, tail := range []string{"다.", "다", ".", "!", "함", "음"} {
		if strings.HasSuffix(first, tail) {
			return true
		}
	}
	return false
}

func checkTypeBody(m *model.Memory, opt Options) []Finding {
	switch opt.typeSpec(m).Body {
	case model.BodyIssueSections:
		if reason := issueSectionProblem(m.Body); reason != "" {
			// 무엇이 모자란지만 말하면 사람이 절 제목을 고치려 든다. 채우는
			// 길을 같이 준다 (리뷰 B → C 넘김 · 실데이터 시험 4-1).
			return []Finding{opt.finding(RuleIssueSections, m, reason,
				"그 절에 줄을 하나씩 더 적는다 — 무엇이 보였나 · 무엇을 고쳤나",
				fmt.Sprintf("한 줄로 길게 써도 된다 (한 절 %d자 넘으면 통과)", sectionMinRunes))}
		}
	case model.BodyNumbered:
		if !numberedSteps(m.Body) {
			return []Finding{opt.finding(RuleHowtoNumbered, m,
				"howto 는 `1.` `2.` 번호 차례가 두 단계 이상 있어야 한다 (①② 는 번호로 안 친다)")}
		}
	}
	return nil
}

// issueSectionProblem 은 issue 가 `## 증상` · `## 해결` 을 각 2줄 이상 갖췄는지다.
func issueSectionProblem(body string) string {
	want := map[string]bool{"증상": false, "해결": false}
	for _, one := range sections(body) {
		for key := range want {
			// 「각 2줄 이상」이 규격이지만 한 줄에 길게 쓴 절도 내용이 있는 것이다
			// (대조군 43a62aa5 의 `## 해결` 이 그 꼴이다). 글자 수로도 본다.
			if strings.Contains(one.Title, key) && (len(one.Lines) >= 2 || sectionRunes(one) >= sectionMinRunes) {
				want[key] = true
			}
		}
	}
	missing := []string{}
	for _, key := range []string{"증상", "해결"} {
		if !want[key] {
			missing = append(missing, "## "+key)
		}
	}
	if len(missing) == 0 {
		return ""
	}
	return fmt.Sprintf("issue 는 %s 절이 각 2줄 이상 있어야 한다", strings.Join(missing, " · "))
}

// sectionRunes 는 절 하나의 글자 수다.
func sectionRunes(one section) int {
	count := 0
	for _, line := range one.Lines {
		count += utf8.RuneCountInString(line)
	}
	return count
}

const sectionMinRunes = 60

func numberedSteps(body string) bool {
	seen := map[string]bool{}
	for _, line := range bodyLines(body) {
		if match := numberedStep.FindStringSubmatch(line); match != nil {
			seen[match[1]] = true
		}
	}
	return len(seen) >= 2
}

// summaryBodyGap 은 요약의 낱말 중 본문에 한 번도 안 나온 것이 절반을 넘는지다
// (규칙 B08). 요약이 본문과 다른 이야기를 하면 검색은 맞히고 사람은 헛읽는다.
func summaryBodyGap(m *model.Memory) string {
	words := nounWords(m.Summary)
	if len(words) < summaryWordFloor {
		return ""
	}
	body := strings.ToLower(m.Body)
	missing := []string{}
	for _, word := range words {
		if !containsWord(body, word) {
			missing = append(missing, word)
		}
	}
	// 절반 초과가 규격이지만 그것만 보면 잘 쓴 요약도 걸린다 — 요약은 본문을
	// 그대로 베끼는 것이 아니라 풀어 쓰는 자리라서다. 본문이 요약을 받치고
	// 있으면 다른 이야기가 아니다.
	//
	// **(2D) 「받친다」를 개수가 아니라 비율로 본다.** 셋만 나오면 봐주던 옛 자는
	// 요약이 길수록 헐거워져서(이름씨 여덟 개 중 다섯이 없어도 안 울림) 규칙이
	// 사실상 안 돌았다.
	backed := len(words) - len(missing)
	if len(missing)*2 <= len(words) || float64(backed)/float64(len(words)) > summaryBackedShare {
		return ""
	}
	sample := missing
	if len(sample) > 3 {
		sample = sample[:3]
	}
	return fmt.Sprintf("요약 낱말 %d개 중 %d개가 본문에 없다 (%s …). 요약과 본문이 다른 이야기다",
		len(words), len(missing), strings.Join(sample, " "))
}

const (
	summaryWordFloor = 4
	// summaryBackedShare 는 「본문이 요약을 받친다」고 볼 이름씨 비율이다.
	//
	// 개수(셋)를 비율로 바꾼 것은 요약이 길수록 옛 자가 헐거워졌기 때문이다.
	// 값은 실기억 130건 스윕으로 잡았다 — 0.20 : 잡음 6 · 대조군 0 · 정밀도 1.000 /
	// 0.30 : 잡음 10 · 대조군 1 / 0.40 : 잡음 14 · 대조군 2. **더 울리게 하면 G6 ②
	// 가 그 자리에서 깨진다.** 규칙을 세게 하는 길은 자를 푸는 것이 아니라
	// 이름씨 뽑기를 고치는 것이다 (text.go 의 2D 주석).
	summaryBackedShare = 0.20
)

func relativeDate(body string) string {
	for _, word := range relativeDateWords {
		if strings.Contains(body, word) {
			return word
		}
	}
	if recentTime.MatchString(body) {
		return "최근"
	}
	return ""
}

// checkMulti 는 여러 사실이 한 건에 섞였는지다 (M 계열). decision 이면 거절,
// 그 밖은 경고다 — 조사 표(history)는 한 건으로 두는 편이 낫다 (설계 결정 8).
func checkMulti(m *model.Memory, opt Options) []Finding {
	found := []Finding{}
	if rows := decisionTableRows(m.Body); rows > 0 {
		found = append(found, opt.finding(RuleMultiTable, m,
			fmt.Sprintf("표에 서로 다른 주제의 행이 %d개 있다. 결정은 한 건에 하나만 담는다", rows),
			"기억을 행마다 나눠 mem add 를 여러 번 친다"))
	}
	if shape := multiSummaryShape(m.Summary); shape != "" {
		found = append(found, opt.finding(RuleMultiSummary, m,
			fmt.Sprintf("요약이 「%s」 꼴이다. 묶음 하나가 낡으면 나머지도 못 믿는다", shape),
			"기억을 결정마다 나눠 mem add 를 여러 번 친다"))
	}
	if titles := splitHeadings(m.Body); len(titles) > 0 {
		found = append(found, opt.finding(RuleMultiHeading, m,
			fmt.Sprintf("절 %d개(%s)가 서로 다른 주제다", len(titles), strings.Join(titles, " · "))))
	}
	return found
}

// decisionTableRows 는 「결정 묶음 표」의 데이터 행 수다. 0 이면 아니다.
//
// 데이터 행 3개 이상이면서 첫 열 값들이 서로 다른 주제여야 한다. 두 가지를
// 빼는데, 둘 다 실제 대조군에서 나온 꼴이다.
//   - 첫 열 전부가 같은 낱말을 물고 있으면 한 주제의 변이 목록이다
//     (`\..\..\AGENTS` · `../../AGENTS` …).
//   - 값 열이 죄다 수치면 한 주제의 지표 표다 (신뢰도 % 표).
func decisionTableRows(body string) int {
	for _, one := range tables(body) {
		if len(one.Rows) < multiTableRows {
			continue
		}
		heads := firstColumn(one)
		if len(heads) < multiTableRows || sharedWord(heads) || numericValues(one) {
			continue
		}
		return len(one.Rows)
	}
	return 0
}

const multiTableRows = 3

func firstColumn(one table) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, row := range one.Rows {
		if len(row) == 0 {
			continue
		}
		cell := strings.TrimSpace(strings.Trim(row[0], "*`_ "))
		if cell == "" || cell == "—" || seen[cell] {
			continue
		}
		seen[cell] = true
		out = append(out, cell)
	}
	return out
}

// sharedWord 는 모든 칸이 같은 내용 낱말을 물고 있는지다.
func sharedWord(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	shared := map[string]int{}
	for _, cell := range cells {
		for word := range uniqueWords(cell) {
			shared[word]++
		}
	}
	for _, count := range shared {
		if count == len(cells) {
			return true
		}
	}
	return false
}

func uniqueWords(text string) map[string]bool {
	set := map[string]bool{}
	for _, word := range contentWords(text) {
		set[word] = true
	}
	return set
}

// numericValues 는 값 열이 죄다 수·비율인지다.
func numericValues(one table) bool {
	numbers := 0
	total := 0
	for _, row := range one.Rows {
		if len(row) < 2 {
			continue
		}
		total++
		if numberCell(row[1]) {
			numbers++
		}
	}
	return total > 0 && numbers*2 >= total
}

func numberCell(cell string) bool {
	cell = strings.TrimSpace(strings.Trim(cell, "*`_ "))
	if cell == "" {
		return false
	}
	digits := false
	for _, letter := range cell {
		switch {
		case unicode.IsDigit(letter):
			digits = true
		case strings.ContainsRune("%.,~-–— /초분시간ms일건개", letter):
		default:
			return false
		}
	}
	return digits
}

func multiSummaryShape(summary string) string {
	for _, shape := range multiSummaryShapes {
		if hit := shape.FindString(summary); hit != "" {
			return strings.TrimSpace(hit)
		}
	}
	return chainedSummaryShape(summary)
}

// chainedSummaryShape 는 「A 는 …고 B 는 …며 C 는 …」처럼 **이음씨끝으로 결정
// 여럿을 한 줄에 이어 붙인** 요약이다 (리뷰 C3 · B14).
//
// 결정을 셋 담은 요약은 낱말 꼴이 아니라 문장 짜임으로 드러난다. 「…개를
// 정했다」 같은 셈말이 없어도 이음씨끝이 둘이면 마디가 셋이다.
//
// **이음씨끝만으로는 못 가른다.** 「색인은 언제든 지우고 다시 만들 수 있는
// 파생물이라, 망가지면 통째로 버리고 …」는 한 결정을 두 마디로 말한 좋은
// 요약인데 이음씨끝이 둘이다. 그래서 **말머리(은/는)가 셋 이상**일 것을 같이
// 요구한다 — 마디마다 주어가 다르면 그때가 결정 여럿이다.
//
// 좋은 기억 10건·골든셋 대조군 20건·실기억 206건에서 거짓 경보 0 이다.
func chainedSummaryShape(summary string) string {
	joints := chainEnding.FindAllString(summary, -1)
	if len(joints) < chainMinJoints || len(topicWords(summary)) < chainMinTopics {
		return ""
	}
	return strings.Join(trimAll(joints), " … ")
}

// chainEnding 은 마디를 잇는 씨끝이다. 두 글자 이상 붙어 있어야 씨끝이고 뒤에는
// 빈칸이 온다 — 「고민」·「며칠」처럼 낱말 앞머리에 든 같은 글자를 안 세려는 고삐다.
var chainEnding = regexp.MustCompile(`[가-힣]{2,}(고|며|면서)\s`)

// topicMark 는 말머리(주제 자리)다. 「무엇은 …」·「무엇는 …」 꼴을 센다.
var topicMark = regexp.MustCompile(`[가-힣]+(은|는)\s`)

const (
	chainMinJoints = 2
	chainMinTopics = 3
)

// topicWords 는 말머리로 쓰인 서로 다른 낱말이다.
func topicWords(summary string) []string {
	seen := map[string]bool{}
	out := []string{}
	for _, one := range topicMark.FindAllString(summary, -1) {
		one = strings.TrimSpace(one)
		if seen[one] {
			continue
		}
		seen[one] = true
		out = append(out, one)
	}
	return out
}

func trimAll(words []string) []string {
	out := make([]string, 0, len(words))
	for _, one := range words {
		out = append(out, strings.TrimSpace(one))
	}
	return out
}

// splitHeadings 는 서로 다른 주제를 다루는 절 제목이다. 절이 3개 이상이고 제목이
// 낱말을 하나도 안 나눠 가질 때만 「갈렸다」고 본다 (규칙 M03).
func splitHeadings(body string) []string {
	titles := headings(body)
	if len(titles) < multiHeadingCount {
		return nil
	}
	if sharedWord(titles) {
		return nil
	}
	words := map[string]bool{}
	for _, title := range titles {
		for word := range uniqueWords(title) {
			words[word] = true
		}
	}
	if len(words) < len(titles) {
		return nil
	}
	return titles
}

const multiHeadingCount = 3
