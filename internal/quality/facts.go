package quality

import (
	"slices"
	"strings"
	"unicode"
)

// 사실 어긋남 검사 — 중복 판정 2단 (설계 결정 30).
//
// 문장 최대 정렬(1단)은 재현율은 좋지만 **뜻은 가깝고 사실은 다른 쌍**을 그대로
// 통과시킨다. 「32.5ms 였다」 와 「107.6ms 였다」 는 글자로는 거의 같다.
// 2단은 정렬된 두 조각에서 **숫자·경로·영문 이름·날짜**를 뽑아, 양쪽이 서로
// 없는 값을 하나씩 갖고 있으면 「다른 사실을 말한다」로 보고 중복에서 뺀다.
//
// 한쪽이 다른 쪽의 부분집합인 것은 안 막는다 — 줄여 쓴 같은 사실이 그 꼴이다.

// facts 는 조각 하나에서 뽑은 확인 가능한 값이다.
type facts struct {
	numbers []string
	names   []string
}

// factsOf 는 글에서 숫자와 영문·경로 이름을 뽑는다. 둘 다 정렬·중복 제거.
func factsOf(text string) facts {
	numbers, names := []string{}, []string{}
	letters := []rune(strings.ToLower(strings.ReplaceAll(text, ",", "")))
	at := 0
	for at < len(letters) {
		switch {
		case unicode.IsDigit(letters[at]):
			start := at
			for at < len(letters) && (unicode.IsDigit(letters[at]) || letters[at] == '.') {
				at++
			}
			numbers = append(numbers, strings.Trim(string(letters[start:at]), "."))
		case isAsciiLetter(letters[at]):
			start := at
			for at < len(letters) && isNameRune(letters[at]) {
				at++
			}
			word := strings.Trim(string(letters[start:at]), "-_./")
			if len([]rune(word)) >= 3 {
				names = append(names, word)
			}
		default:
			at++
		}
	}
	return facts{numbers: tidy(numbers), names: tidy(names)}
}

func isAsciiLetter(letter rune) bool {
	return (letter >= 'a' && letter <= 'z') || (letter >= 'A' && letter <= 'Z')
}

func isNameRune(letter rune) bool {
	if isAsciiLetter(letter) || unicode.IsDigit(letter) {
		return true
	}
	return letter == '-' || letter == '_' || letter == '.' || letter == '/' || letter == '\\'
}

func tidy(words []string) []string {
	out := make([]string, 0, len(words))
	for _, word := range words {
		if word != "" {
			out = append(out, word)
		}
	}
	slices.Sort(out)
	return slices.Compact(out)
}

// 어긋남을 재는 자는 둘이다 (v0.4 갈래 B②).
//
//	엄한 자   양쪽이 서로 없는 값을 하나씩 가지면 어긋남 (v0.3 까지 쓰던 하나뿐인 자)
//	느슨한 자 거기에 더해 **나눠 갖는 값이 하나도 없을 때만** 어긋남
//
// 엄한 자는 「같은 사실을 자세히·줄여 쓴 짝」을 어긋남으로 센다 — 한쪽이 곁수치를
// 더 적으면 그것만으로 어긋남이 됐다. 그렇다고 느슨한 자를 **거절(저장 막기)** 에까지
// 쓰면 실데이터 40건 `duplicate-hard` 가 1 → 5건(2.5% → 12.5%)으로 자(5%)를 넘는다.
// 그래서 **경고 쪽(부분 중복)에만** 느슨한 자를 쓴다 (stageTwoLoose).

// bothSidedLoose 는 느슨한 자다.
func bothSidedLoose(left, right []string) bool {
	return bothSided(left, right) && !overlaps(left, right)
}

// clashOf 는 두 사실 묶음이 어긋나는지를 두 자로 잰다.
func clashOf(one, two facts) (strict, loose bool) {
	strict = bothSided(one.numbers, two.numbers) || bothSided(one.names, two.names)
	loose = bothSidedLoose(one.numbers, two.numbers) || bothSidedLoose(one.names, two.names)
	return strict, loose
}

// factsClashUnits 는 조각 둘의 사실이 어긋나는지다. 미리 뽑아 둔 것이 있으면
// 그것을 쓴다 (리뷰 B · V3 — 짝마다 다시 뽑지 않는다).
func factsClashUnits(left, right *unit) (strict, loose bool) {
	return clashOf(factsForUnit(left), factsForUnit(right))
}

// factsForUnit 은 조각의 사실이다. 안 뽑아 뒀으면 그때 뽑는다 — **여기서
// 채워 넣지는 않는다.** 조각은 일꾼 여럿이 같이 보는 자료다.
func factsForUnit(one *unit) facts {
	if one.known != nil {
		return *one.known
	}
	return factsOf(one.text)
}

// factsClash 는 두 조각이 서로 다른 사실을 말하는지다 (엄한 자).
func factsClash(left, right string) bool {
	strict, _ := clashOf(factsOf(left), factsOf(right))
	return strict
}

// factsClashLoose 는 느슨한 자로 잰 어긋남이다.
func factsClashLoose(left, right string) bool {
	_, loose := clashOf(factsOf(left), factsOf(right))
	return loose
}

// sharedFact 는 두 조각이 **같은** 확인 가능한 값을 하나라도 나눠 갖는지다.
// 숫자·경로·이름이 하나도 안 겹치는 두 문장은 「같은 사실을 두 번 적은 것」이
// 아니라 「같은 말투로 쓴 다른 이야기」일 때가 많다.
func sharedFact(left, right string) bool {
	one, two := factsOf(left), factsOf(right)
	return overlaps(one.numbers, two.numbers) || overlaps(one.names, two.names)
}

func overlaps(left, right []string) bool {
	for _, one := range left {
		if _, found := slices.BinarySearch(right, one); found {
			return true
		}
	}
	return false
}

// bothSided 는 두 정렬 목록이 서로에게 없는 값을 하나씩 갖는지다.
func bothSided(left, right []string) bool {
	return missing(left, right) && missing(right, left)
}

// missing 은 left 에 있고 right 에 없는 값이 있는지다.
func missing(left, right []string) bool {
	for _, one := range left {
		if _, found := slices.BinarySearch(right, one); !found {
			return true
		}
	}
	return false
}
