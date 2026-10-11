package quality

import (
	"regexp"
	"strings"
)

// 기억 본문 꼬리표 떼기 — NLI 서버에 보내기 전에 줄 머리의 `왜 :` · `무엇을 :` 같은 틀 낱말을 뗀다.
//
// 판정 서버(JudgeModel)는 학습·평가·측정 때 같은 규칙으로 떼고, 서버 자체는 안 뗀다 — 손님인 이
// 툴이 떼고 보내야 측정과 서빙이 같은 글을 본다. **낱말 목록과 꼴은 JudgeModel
// `train/data_prep.py` 의 TAG_WORDS · TAG_RE 와 글자 하나까지 같아야 한다.** 바꾸면 두 쪽을 같이 바꾼다.

// tagWords 는 꼬리표 낱말이다. 차례도 Python 쪽과 같다 — 앞 것부터 맞춰 본다.
var tagWords = []string{"근거", "왜", "무엇을", "무엇", "이후", "다음에", "다음", "까닭", "내용", "결론", "주의", "정한 것",
	"확인", "어떻게", "무엇이", "무슨 일이"}

// pySpace 는 Python 의 `\s`(str 패턴) 와 같은 글자 묶음이다. Go 의 `\s` 는 ASCII 다섯 글자뿐이라
// 전각 공백(U+3000) · NBSP 같은 것을 못 먹는다 — str.isspace() 가 참인 글자를 다 적는다.
const pySpace = `[\t\n\x0b\x0c\r\x1c-\x1f \x{85}\x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}]`

// tagPattern 은 `^\s*(낱말|…)\s*[:：]\s*` (여러 줄 모드)다. 머리의 `\s*` 는 줄바꿈까지 먹는다 —
// Python 과 같게 둔다 (빈 줄 뒤 꼬리표는 빈 줄째 사라진다).
var tagPattern = regexp.MustCompile(`(?m)^` + pySpace + `*(` + strings.Join(tagWords, "|") + `)` +
	pySpace + `*[:：]` + pySpace + `*`)

// StripTags 는 줄마다 머리의 꼬리표를 뗀다. 줄 가운데 것과 꼬리표 아닌 낱말(`왜냐 :`)은 그대로다.
// 한 번만 지난다 — `왜 : 근거 : x` 는 `근거 : x` 가 된다 (Python strip_tags 와 같다).
func StripTags(text string) string {
	return tagPattern.ReplaceAllString(text, "")
}

// stripSentence 는 문장 하나에서 꼬리표를 떼고 앞뒤 공백을 다듬는다. 다 떼어 비면 빈 글이다.
func stripSentence(text string) string {
	return strings.TrimSpace(StripTags(text))
}
