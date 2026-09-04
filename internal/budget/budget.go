// Package budget holds the one token estimate that search, context and hook
// share. We never call an LLM, so the count is approximated from characters.
package budget

import (
	"math"
	"unicode"
)

// Weights measured on o200k: 0.58 tokens per Korean character, 0.23 per Latin
// one. The 1.2 margin is because Korean drifts up to 1.5x between encoders and
// going over budget is the dangerous side.
const (
	cjkWeight   = 0.6
	otherWeight = 0.25
	margin      = 1.2
)

// Estimate returns how many tokens a string is worth, rounded up.
func Estimate(text string) int {
	cjk, other := 0, 0
	for _, letter := range text {
		if isCJK(letter) {
			cjk++
			continue
		}
		other++
	}
	return int(math.Ceil((float64(cjk)*cjkWeight + float64(other)*otherWeight) * margin))
}

// Fits says whether the text stays inside a budget; a budget of zero is no limit.
func Fits(text string, limit int) bool {
	return limit <= 0 || Estimate(text) <= limit
}

func isCJK(letter rune) bool {
	return unicode.In(letter, unicode.Hangul, unicode.Han, unicode.Hiragana, unicode.Katakana)
}

// Clip cuts a text down by runes until the estimate fits the limit, and reports
// whether anything was removed. A limit of zero or less means no limit.
func Clip(text string, limit int) (string, bool) {
	if limit <= 0 || Estimate(text) <= limit {
		return text, false
	}
	letters := []rune(text)
	// The estimate only grows with length, so the longest prefix that fits can
	// be found by halving instead of by counting one rune at a time.
	low, high := 0, len(letters)
	for low < high {
		middle := (low + high + 1) / 2
		if Estimate(string(letters[:middle])) <= limit {
			low = middle
			continue
		}
		high = middle - 1
	}
	return string(letters[:low]), true
}
