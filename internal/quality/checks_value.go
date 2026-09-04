package quality

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// B12 `no-value` — 여섯 달 뒤에 이 기억이 쓸모가 있나 (설계 결정 37).
//
// v0.2 는 「짧다」(B01)만 봤고 NOVALUE 재현율이 0.400 이었다. 짧고 알찬 기억과
// 길고 텅 빈 기억을 길이로는 못 가른다. 기계가 물을 수 있는 것은 셋이다.
//
//	① 확인 가능한 조각 — 숫자·경로·주소·날짜·영문 이름이 하나라도 있나
//	② 결론 문장 꼴 — 「~로 정했다」·「~를 쓴다」·「~는 하지 않는다」가 있나
//	③ 제목 되풀이 — 본문이 title+summary 를 다시 늘어놓은 것인가
//
// 셋 중 하나라도 걸리면 경고다. 거절이 아닌 이유는 「쓸모」가 사람 판정이라서다.

// conclusionShapes 는 결론 문장 꼴이다. 무엇을 정했는지·무엇을 쓰는지·무엇을
// 안 하는지 셋 중 하나로 끝나야 여섯 달 뒤에 읽어도 쓸 것이 남는다.
var conclusionShapes = []*regexp.Regexp{
	regexp.MustCompile(`(정했다|정한다|못 박|확정|골랐다|택했다|뽑았다)`),
	regexp.MustCompile(`(기로 (했다|한다|정했다))`),
	regexp.MustCompile(`(쓴다|쓰지 않는다|안 쓴다|만든다|안 만든다|막는다|따른다|남긴다|버린다|삼는다|뺀다|넣는다)`),
	regexp.MustCompile(`(하지 않는다|안 한다|하면 안 된다|해서는 안 된다|금지)`),
	regexp.MustCompile(`(결론|규칙|원칙)\s*[:：]`),
}

// echoCut 은 「본문이 title+summary 의 재배열」로 보는 닮음이다. 설계 결정 37 이
// 0.9 를 적었고 골든셋 130건에서 이 값 위로 올라오는 것은 실제 되풀이뿐이다.
const echoCut = 0.9

// noValueReason 은 걸린 이유 한 줄이다. 안 걸리면 빈 글이다.
func noValueReason(m *model.Memory, kind model.TypeSpec) string {
	body := strings.TrimSpace(m.Body)
	if body == "" {
		return ""
	}
	if kind.Evidence && !hasVerifiable(body) {
		return "본문에 숫자·경로·주소·날짜가 하나도 없다. 여섯 달 뒤에 이 말이 맞는지 확인할 길이 없다"
	}
	if echoesHead(m) {
		return "본문이 제목·요약을 다시 늘어놓은 것뿐이다. 왜 그렇게 됐는지와 다음에 볼 것을 적는다"
	}
	if kind.Conclusion && !hasConclusion(m) {
		return "무엇을 정했는지·무엇을 쓰는지·무엇을 안 하는지가 한 문장도 없다. 결론을 한 줄로 적는다"
	}
	return ""
}

// hasVerifiable 은 확인 가능한 조각이 하나라도 있는지다. 2단 어긋남 검사가
// 쓰는 factsOf 를 그대로 쓴다 — 「확인 가능한 값」의 뜻이 두 곳에서 갈리면 안 된다.
func hasVerifiable(body string) bool {
	found := factsOf(body)
	return len(found.numbers) > 0 || len(found.names) > 0
}

// echoesHead 는 본문이 제목+요약의 재배열인지다.
func echoesHead(m *model.Memory) bool {
	head := grams(normalizeText(m.DisplayTitle() + " " + m.Summary))
	body := grams(normalizeText(m.Body))
	if len(head) == 0 || len(body) == 0 {
		return false
	}
	// 본문이 머리말보다 훨씬 길면 새 이야기가 든 것이다. 자카드는 길이 차이를
	// 벌로 세니 짧은 쪽 기준 겹침으로 본다.
	shared := sharedGrams(head, body)
	small := len(head)
	if len(body) < small {
		small = len(body)
	}
	return float64(shared)/float64(small) >= echoCut && len(body) <= len(head)*2
}

func sharedGrams(left, right []uint64) int {
	shared, at, other := 0, 0, 0
	for at < len(left) && other < len(right) {
		switch {
		case left[at] == right[other]:
			shared++
			at++
			other++
		case left[at] < right[other]:
			at++
		default:
			other++
		}
	}
	return shared
}

func hasConclusion(m *model.Memory) bool {
	text := m.Summary + "\n" + m.Body
	if hasPresentDeclarative(text) {
		return true
	}
	for _, shape := range conclusionShapes {
		if shape.MatchString(text) {
			return true
		}
	}
	return false
}

// checkValue 는 B12 하나다. checkBody 와 갈라 둔 것은 이 규칙이 본문 규격이
// 아니라 「쓸모」를 보기 때문이다 — 자가 다르면 파일도 가른다.
func checkValue(m *model.Memory, opt Options) []Finding {
	reason := noValueReason(m, opt.typeSpec(m))
	if reason == "" {
		return nil
	}
	return []Finding{opt.finding(RuleNoValue, m, reason,
		fmt.Sprintf("mem show %s   (무엇이 빠졌는지 본다)", m.ID),
		"숫자·경로·날짜 하나와 결론 한 줄을 더해 다시 넣는다")}
}

// hasPresentDeclarative 는 「지금 이렇게 한다」는 현재형 서술 종결이 있는지다.
//
// 한국어에서 결정문은 현재형으로 끝난다 — `자른다`·`않는다`·`쓴다`·`막는다`.
// 기록문은 과거형으로 끝난다 — `잘랐다`·`했다`·`됐다`. 둘을 가르는 것은 「다」
// 바로 앞 글자의 받침이 ㄴ 인가 하나뿐이다. 낱말 목록을 늘리는 것보다 이 자가
// 훨씬 넓고 정확하다 — 목록으로는 `자른다` 하나를 놓쳐 멀쩡한 결정이 걸렸다.
func hasPresentDeclarative(text string) bool {
	letters := []rune(text)
	for at := 1; at < len(letters); at++ {
		if letters[at] != '다' || !isHangul(letters[at-1]) {
			continue
		}
		if (letters[at-1]-0xAC00)%28 == jongNieun {
			return true
		}
	}
	return false
}

// jongNieun 은 한글 받침 ㄴ 의 자리다.
const jongNieun = 4
