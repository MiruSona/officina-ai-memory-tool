// Package simhash 는 글 하나를 64비트 지문으로 줄이고, 그 지문을 16비트씩 넷으로
// 잘라 "닮은 것끼리만 비교" 하는 열쇠를 만든다. lint 의 중복 검사와 승격 합치기가
// 같이 쓴다 (설계 9-2 · 12-1).
package simhash

import (
	"strings"
	"unicode/utf8"
)

// GramSize 는 지문을 뜨는 조각 길이다. 3글자면 한글 두 낱말이 겹치는 자리를
// 잡아 낸다.
const GramSize = 3

// BandCount 는 지문을 몇 토막으로 나누는지다. 64 / 4 = 16비트짜리 열쇠 넷.
const BandCount = 4

// BandBits 는 열쇠 하나의 비트 수다.
const BandBits = 64 / BandCount

// Grams 는 글의 3글자 조각 집합이다. 공백은 한 칸으로 눌러서 줄바꿈이 다른
// 같은 글이 다른 조각을 내지 않게 한다.
func Grams(text string) map[string]bool {
	flat := strings.Join(strings.Fields(text), " ")
	starts := runeStarts(flat)
	set := map[string]bool{}
	for at := 0; at+GramSize < len(starts); at++ {
		set[flat[starts[at]:starts[at+GramSize]]] = true
	}
	return set
}

// runeStarts 는 글자마다의 바이트 자리에 끝자리 하나를 더한 것이다. 조각을
// 자를 때마다 []rune 을 새로 만들지 않으려고 한 번만 만든다.
func runeStarts(text string) []int {
	starts := make([]int, 0, len(text)+1)
	for at := 0; at < len(text); {
		starts = append(starts, at)
		_, size := utf8.DecodeRuneInString(text[at:])
		at += size
	}
	return append(starts, len(text))
}

// Of 는 글 하나의 지문이다. 조각 집합을 따로 안 만들고 훑으면서 세기 때문에
// 2만 건을 돌려도 메모리가 안 튄다.
func Of(text string) uint64 {
	flat := strings.Join(strings.Fields(text), " ")
	starts := runeStarts(flat)
	weights := [64]int{}
	for at := 0; at+GramSize < len(starts); at++ {
		addGram(&weights, flat[starts[at]:starts[at+GramSize]])
	}
	return foldWeights(&weights)
}

// OfGrams 는 이미 만들어 둔 조각 집합으로 지문을 뜬다. 같은 글이면 Of 와 같은
// 값이 나온다.
func OfGrams(grams map[string]bool) uint64 {
	weights := [64]int{}
	for gram := range grams {
		addGram(&weights, gram)
	}
	return foldWeights(&weights)
}

// addGram 은 조각 하나의 해시를 비트마다 +1 / -1 로 쌓는다. 가지치기 없이
// 더하는 꼴이라야 2만 건에서 억 단위로 도는 이 고리가 안 느려진다.
func addGram(weights *[64]int, gram string) {
	value := hashOf(gram)
	for bit := 0; bit < 64; bit++ {
		weights[bit] += int((value>>uint(bit))&1)*2 - 1
	}
}

// foldWeights 는 쌓인 무게를 비트 하나씩으로 접는다.
func foldWeights(weights *[64]int) uint64 {
	out := uint64(0)
	for bit := 0; bit < 64; bit++ {
		if weights[bit] > 0 {
			out |= 1 << uint(bit)
		}
	}
	return out
}

// fnv1a 상수. hash/fnv 와 같은 값을 내는데, 조각마다 해시 객체를 새로 만들지
// 않는다 — 조각이 천만 개면 그 할당이 그대로 시간이다.
const (
	fnvOffset = 14695981039346656037
	fnvPrime  = 1099511628211
)

func hashOf(gram string) uint64 {
	sum := uint64(fnvOffset)
	for at := 0; at < len(gram); at++ {
		sum ^= uint64(gram[at])
		sum *= fnvPrime
	}
	return sum
}

// Bands 는 지문을 16비트 열쇠 넷으로 나눈다. 두 글이 한 밴드라도 똑같으면
// 후보다 — 밴드 하나가 같을 확률이 닮은 정도를 따라 확 올라간다.
func Bands(fingerprint uint64) [BandCount]uint16 {
	out := [BandCount]uint16{}
	for band := 0; band < BandCount; band++ {
		out[band] = uint16(fingerprint >> uint(band*BandBits))
	}
	return out
}

// Distance 는 두 지문이 몇 비트 다른지다.
func Distance(left, right uint64) int {
	diff := left ^ right
	count := 0
	for diff != 0 {
		diff &= diff - 1
		count++
	}
	return count
}

// Jaccard 는 두 조각 집합이 얼마나 겹치는지다. 0~1.
func Jaccard(left, right map[string]bool) float64 {
	if len(right) < len(left) {
		left, right = right, left
	}
	shared := 0
	for gram := range left {
		if right[gram] {
			shared++
		}
	}
	union := len(left) + len(right) - shared
	if union == 0 {
		return 0
	}
	return float64(shared) / float64(union)
}
