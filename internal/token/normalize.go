package token

import (
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// Canon 은 `mem.toml [canon]` 대표말 표를 최장 일치 치환에 쓸 꼴로 든 것이다.
// nil 이면 치환이 항등이다 — 표를 아직 안 채운 저장소가 아무 벌도 안 받는다
// (결정 25 · vocab 의 「배우는 중」과 같은 정신).
type Canon struct {
	keys []string // 긴 것부터. 최장 일치를 이 차례로 찾는다
	to   map[string]string
	// latin 은 어절 경계에서만 바꿀 키다. `index` 를 문자열로 치환하면
	// `indexer` 가 `색인er` 이 된다. 한글은 붙여 쓰는 말이 많아 그대로 둔다.
	latin map[string]bool
}

// NewCanon 은 표를 다듬어 들고 순환·빈 값을 거절한다. 키와 대표말은 둘 다
// 기본 정규화를 거쳐 담긴다 — 그래야 정규화된 글과 글자가 맞는다.
func NewCanon(table map[string]string) (*Canon, error) {
	canon := &Canon{to: map[string]string{}, latin: map[string]bool{}}
	for rawKey, rawValue := range table {
		key, value := baseNormalize(rawKey), baseNormalize(rawValue)
		if key == "" || value == "" {
			return nil, fmt.Errorf("[canon] %q = %q : 빈 값은 못 쓴다", rawKey, rawValue)
		}
		if old, twice := canon.to[key]; twice && old != value {
			return nil, fmt.Errorf("[canon] %q 가 %q 와 %q 두 대표말을 가리킨다", key, old, value)
		}
		canon.to[key] = value
		canon.latin[key] = isLatinWord(key)
	}
	for key, value := range canon.to {
		if _, chained := canon.to[value]; chained {
			return nil, fmt.Errorf("[canon] %q → %q : 대표말이 다시 바뀔 말이다 (순환)", key, value)
		}
	}
	for key := range canon.to {
		canon.keys = append(canon.keys, key)
	}
	sort.Slice(canon.keys, func(a, b int) bool {
		left, right := canon.keys[a], canon.keys[b]
		if len([]rune(left)) != len([]rune(right)) {
			return len([]rune(left)) > len([]rune(right))
		}
		return left < right
	})
	return canon, nil
}

// Len 은 표에 든 줄 수다. 0 이면 치환이 항등이다.
func (c *Canon) Len() int {
	if c == nil {
		return 0
	}
	return len(c.keys)
}

// Apply 는 이미 기본 정규화를 마친 글에 대표말을 넣는다. 한글 키는 어절이
// 아니라 최장 일치 문자열 치환이라 `기억 수정` 같은 여러 낱말 구도 잡는다.
// 라틴 키는 어절 단위다 (결정 25).
func (c *Canon) Apply(text string) string {
	if c.Len() == 0 {
		return text
	}
	letters := []rune(text)
	out := strings.Builder{}
	out.Grow(len(text))
	for at := 0; at < len(letters); {
		hit := ""
		for _, key := range c.keys {
			size := len([]rune(key))
			if at+size > len(letters) || string(letters[at:at+size]) != key {
				continue
			}
			// 라틴 키는 어절 단위로만 바꾼다 - `index` 를 문자열로 치환하면
			// `indexer` 가 `색인er` 이 된다.
			if c.latin[key] && !wordEdges(letters, at, at+size) {
				continue
			}
			hit = key
			break
		}
		if hit == "" {
			out.WriteRune(letters[at])
			at++
			continue
		}
		out.WriteString(c.to[hit])
		at += len([]rune(hit))
	}
	return out.String()
}

// isLatinWord 는 아스키 글자·숫자로만 된 키인지다.
func isLatinWord(key string) bool {
	for _, letter := range key {
		if letter > 127 || !(unicode.IsLetter(letter) || unicode.IsDigit(letter)) {
			return false
		}
	}
	return key != ""
}

// wordEdges 는 [from, to) 양쪽이 글자도 숫자도 아닌지다. 글 끝도 경계로 친다.
func wordEdges(letters []rune, from, to int) bool {
	if from > 0 && isWordRune(letters[from-1]) {
		return false
	}
	return to >= len(letters) || !isWordRune(letters[to])
}

func isWordRune(letter rune) bool {
	return unicode.IsLetter(letter) || unicode.IsDigit(letter)
}

// Normalize 는 색인 쪽과 질의 쪽이 같이 타는 정규화다 (결정 21). 순서는
// 소문자·전각/반각·공백 → 조사 떼기 → 대표말 치환이다 (설계 3절).
// 보여 주는 글은 늘 원문이다 — 이 결과는 색인·비교에만 쓴다.
func Normalize(text string, canon *Canon) string {
	return canon.Apply(stripParticles(baseNormalize(text)))
}

// NormBigrams 는 `fts_norm` 표에 넣을 조각 줄이다.
func NormBigrams(text string, canon *Canon) string {
	return ForIndex(Normalize(text, canon))
}

// NormQueryExpr 는 `fts_norm` 에 물을 FTS5 MATCH 식이다. NormBigrams 와 같은
// 정규화를 타야 `인덱싱` 과 `색인` 이 같은 글자가 된다.
func NormQueryExpr(query string, canon *Canon) string {
	return QueryExpr(Normalize(query, canon))
}

// baseNormalize 는 대표말·조사를 빼고 글자만 고르는 자리다.
func baseNormalize(text string) string {
	folded := strings.Builder{}
	folded.Grow(len(text))
	for _, letter := range text {
		switch {
		case letter >= 0xFF01 && letter <= 0xFF5E: // 전각 아스키
			folded.WriteRune(letter - 0xFEE0)
		case letter == 0x3000: // 전각 빈칸
			folded.WriteRune(' ')
		default:
			folded.WriteRune(letter)
		}
	}
	words := strings.Fields(strings.ToLower(folded.String()))
	for at, word := range words {
		words[at] = NormalizeNumber(word)
	}
	return strings.Join(words, " ")
}

// stripParticles 는 어절마다 v0.2 의 조사 떼기를 쓴다. 새 조사·어미를 안 더한다
// (결정 27 — v0.2 실측 효과 0.000).
func stripParticles(text string) string {
	words := strings.Fields(text)
	for at, word := range words {
		if stem, ok := StripParticle(word); ok {
			words[at] = stem
		}
	}
	return strings.Join(words, " ")
}

var (
	jamoLead  = [...]string{"ㄱ", "ㄲ", "ㄴ", "ㄷ", "ㄸ", "ㄹ", "ㅁ", "ㅂ", "ㅃ", "ㅅ", "ㅆ", "ㅇ", "ㅈ", "ㅉ", "ㅊ", "ㅋ", "ㅌ", "ㅍ", "ㅎ"}
	jamoVowel = [...]string{"ㅏ", "ㅐ", "ㅑ", "ㅒ", "ㅓ", "ㅔ", "ㅕ", "ㅖ", "ㅗ", "ㅘ", "ㅙ", "ㅚ", "ㅛ", "ㅜ", "ㅝ", "ㅞ", "ㅟ", "ㅠ", "ㅡ", "ㅢ", "ㅣ"}
	jamoTail  = [...]string{"", "ㄱ", "ㄲ", "ㄳ", "ㄴ", "ㄵ", "ㄶ", "ㄷ", "ㄹ", "ㄺ", "ㄻ", "ㄼ", "ㄽ", "ㄾ", "ㄿ", "ㅀ", "ㅁ", "ㅂ", "ㅄ", "ㅅ", "ㅆ", "ㅇ", "ㅈ", "ㅊ", "ㅋ", "ㅌ", "ㅍ", "ㅎ"}
)

// Jamo 는 한글 음절을 초·중·종성으로 푼다. 오타·종성 차이를 견주는 **보조**
// 신호다 — 우연 겹침이 많아 주 신호로 안 쓴다 (결정 29).
func Jamo(text string) string {
	out := strings.Builder{}
	for _, letter := range text {
		if !isHangul(letter) {
			out.WriteRune(letter)
			continue
		}
		code := int(letter - 0xAC00)
		out.WriteString(jamoLead[code/588])
		out.WriteString(jamoVowel[(code%588)/28])
		out.WriteString(jamoTail[code%28])
	}
	return out.String()
}
