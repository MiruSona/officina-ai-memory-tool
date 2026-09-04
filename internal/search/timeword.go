package search

import (
	"regexp"
	"strconv"
	"strings"
	"time"
)

// span 은 시간 표현 하나가 가리키는 범위와 사람에게 보일 이름이다.
type span struct {
	since time.Time
	until time.Time
	label string
}

var (
	dayPattern   = regexp.MustCompile(`^(\d{4})-(\d{2})-(\d{2})$`)
	monthPattern = regexp.MustCompile(`^(\d{4})-(\d{2})$`)
	korMonth     = regexp.MustCompile(`^(\d{1,2})월$`)
	recentDays   = regexp.MustCompile(`^(\d{1,4})일$`)
)

// readTime 은 words[at] 에서 시작하는 시간 표현을 읽는다. 몇 낱말을 썼는지와
// 범위를 준다 (설계 6-8).
func readTime(words []string, at int, now time.Time) (int, span, bool) {
	word := words[at]
	if used, found, ok := readTwoWords(words, at, now); ok {
		return used, found, true
	}
	if found, ok := readOneWord(word, now); ok {
		return 1, found, true
	}
	return 0, span{}, false
}

// readTwoWords 는 `최근 30일` 과 `30일 전` 처럼 두 낱말짜리 표현이다.
func readTwoWords(words []string, at int, now time.Time) (int, span, bool) {
	if at+1 >= len(words) {
		return 0, span{}, false
	}
	first, second := words[at], words[at+1]
	if first == "최근" {
		if days, ok := daysOf(second); ok {
			return 2, daysBack(now, days), true
		}
	}
	if second == "전" {
		if days, ok := daysOf(first); ok {
			return 2, daysBack(now, days), true
		}
	}
	return 0, span{}, false
}

func daysOf(word string) (int, bool) {
	match := recentDays.FindStringSubmatch(word)
	if match == nil {
		return 0, false
	}
	days, err := strconv.Atoi(match[1])
	if err != nil || days <= 0 {
		return 0, false
	}
	return days, true
}

func readOneWord(word string, now time.Time) (span, bool) {
	if found, ok := namedDay(word, now); ok {
		return found, true
	}
	if found, ok := namedRange(word, now); ok {
		return found, true
	}
	return datedRange(word, now)
}

func namedDay(word string, now time.Time) (span, bool) {
	back := map[string]int{"오늘": 0, "어제": 1, "그저께": 2, "그제": 2}
	shift, found := back[word]
	if !found {
		return span{}, false
	}
	start := startOfDay(now).AddDate(0, 0, -shift)
	return span{since: start, until: start.AddDate(0, 0, 1), label: label(start, start.AddDate(0, 0, 1))}, true
}

func namedRange(word string, now time.Time) (span, bool) {
	switch word {
	case "이번주":
		return weekOf(now, 0), true
	case "지난주":
		return weekOf(now, -1), true
	case "이번달":
		return monthOf(now, 0), true
	case "지난달":
		return monthOf(now, -1), true
	}
	return span{}, false
}

// datedRange 는 `2026-08-22`·`2026-08`·`8월` 을 읽는다.
func datedRange(word string, now time.Time) (span, bool) {
	if match := dayPattern.FindStringSubmatch(word); match != nil {
		start, err := time.ParseInLocation("2006-01-02", word, time.Local)
		if err != nil {
			return span{}, false
		}
		return span{since: start, until: start.AddDate(0, 0, 1), label: label(start, start.AddDate(0, 0, 1))}, true
	}
	if match := monthPattern.FindStringSubmatch(word); match != nil {
		start, err := time.ParseInLocation("2006-01", word, time.Local)
		if err != nil {
			return span{}, false
		}
		return span{since: start, until: start.AddDate(0, 1, 0), label: label(start, start.AddDate(0, 1, 0))}, true
	}
	return koreanMonth(word, now)
}

func koreanMonth(word string, now time.Time) (span, bool) {
	match := korMonth.FindStringSubmatch(word)
	if match == nil {
		return span{}, false
	}
	month, err := strconv.Atoi(match[1])
	if err != nil || month < 1 || month > 12 {
		return span{}, false
	}
	start := time.Date(now.Year(), time.Month(month), 1, 0, 0, 0, 0, time.Local)
	if start.After(now) {
		start = start.AddDate(-1, 0, 0)
	}
	return span{since: start, until: start.AddDate(0, 1, 0), label: label(start, start.AddDate(0, 1, 0))}, true
}

func daysBack(now time.Time, days int) span {
	start := startOfDay(now).AddDate(0, 0, -days+1)
	end := startOfDay(now).AddDate(0, 0, 1)
	return span{since: start, until: end, label: label(start, end)}
}

// weekOf 는 월요일부터 일요일까지다.
func weekOf(now time.Time, shift int) span {
	back := (int(now.Weekday()) + 6) % 7
	start := startOfDay(now).AddDate(0, 0, -back+7*shift)
	end := start.AddDate(0, 0, 7)
	return span{since: start, until: end, label: label(start, end)}
}

func monthOf(now time.Time, shift int) span {
	start := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.Local).AddDate(0, shift, 0)
	end := start.AddDate(0, 1, 0)
	return span{since: start, until: end, label: label(start, end)}
}

func startOfDay(now time.Time) time.Time {
	return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local)
}

func label(since, until time.Time) string {
	last := until.AddDate(0, 0, -1)
	if since.Format("2006-01-02") == last.Format("2006-01-02") {
		return since.Format("2006-01-02")
	}
	return since.Format("2006-01-02") + " ~ " + last.Format("2006-01-02")
}

// ParseSince 는 `30d` 나 `2026-08-01` 을 읽는다. --since 와 골든셋이 같이 쓴다.
func ParseSince(value string) (time.Time, bool) {
	if value == "" {
		return time.Time{}, true
	}
	if strings.HasSuffix(value, "d") {
		days, err := strconv.Atoi(value[:len(value)-1])
		if err != nil || days <= 0 {
			return time.Time{}, false
		}
		return time.Now().AddDate(0, 0, -days), true
	}
	parsed, err := time.ParseInLocation("2006-01-02", value, time.Local)
	if err != nil {
		return time.Time{}, false
	}
	return parsed, true
}
