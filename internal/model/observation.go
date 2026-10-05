package model

import (
	"crypto/sha256"
	"encoding/hex"
	"regexp"
	"sort"
	"strings"
)

// 모음 기억(observation)이 같이 쓰는 셈 셋 — 본문 해시 · 근거 해시 · 줄 근거 읽기.
// 색인(낡음 판정)·quality(검토 큐)·consolidate(카드 쓰기)가 **같은 한 벌**을
// 써야 한쪽만 「낡았다」 고 말하는 일이 없다 (자동쌓기설계 3-4).

// basisHashRunes 는 머리말에 적는 근거 해시 길이다 (설계 3-2 「앞 8자」).
const basisHashRunes = 8

var (
	basisHashPattern = regexp.MustCompile(`^[0-9a-f]{8}$`)
	// citePattern 은 모음 기억 한 줄 끝의 근거 표시 `[mem:<id>]` 다.
	citePattern = regexp.MustCompile(`\[mem:(\d{8}-[0-9a-f]{8})\]`)
	// numberedLine 은 번호 줄 머리 `1. ` 이다.
	numberedLine = regexp.MustCompile(`^\d+\.\s`)
)

// BodyHash 는 본문 글자의 sha256 이다. 색인의 body_hash 칸과 같은 값이다.
func BodyHash(body string) string {
	sum := sha256.Sum256([]byte(body))
	return hex.EncodeToString(sum[:])
}

// BasisPart 는 근거 기억 하나에서 해시에 넣는 것이다. 카드가 보여 주는 것(제목·
// 요약)과 본문, 그리고 죽음·보류 표시다. 링크·조회수처럼 뜻이 안 바뀌는 칸은
// 안 넣는다 — 넣으면 링크 하나 달 때마다 모음이 낡는다.
type BasisPart struct {
	ID       string
	Title    string
	Summary  string
	BodyHash string
	// Dead 는 덮였거나 무효 날짜가 적힌 것이다. 시각에 안 기대게 「적혀 있나」만 본다.
	Dead bool
	// Held 는 보류(`review: true`)다. 되돌리기(`mem auto undo`)가 이 칸을 단다.
	Held bool
}

// PartOf 는 기억 파일 하나를 해시 조각으로 만든다.
func PartOf(m *Memory) BasisPart {
	return BasisPart{ID: m.ID, Title: m.DisplayTitle(), Summary: m.Summary, BodyHash: BodyHash(m.Body),
		Dead: m.SupersededBy != "" || m.InvalidAt != "", Held: m.Review}
}

// BasisHash 는 근거 조각들의 해시 앞 8자다. id 차례로 줄 세워 넣으니 근거를 적은
// 차례가 달라도 같은 값이다.
func BasisHash(parts []BasisPart) string {
	sorted := append([]BasisPart{}, parts...)
	sort.Slice(sorted, func(a, b int) bool { return sorted[a].ID < sorted[b].ID })
	text := strings.Builder{}
	for _, part := range sorted {
		text.WriteString(part.ID + "\x00" + part.Title + "\x00" + part.Summary + "\x00" + part.BodyHash +
			"\x00" + flag(part.Dead) + flag(part.Held) + "\n")
	}
	return BodyHash(text.String())[:basisHashRunes]
}

func flag(on bool) string {
	if on {
		return "1"
	}
	return "0"
}

// IsBasisHash 는 머리말 basis_hash 꼴(소문자 16진 8자)인지다.
func IsBasisHash(value string) bool { return basisHashPattern.MatchString(value) }

// MemSources 는 sources 의 `mem:` 근거 id 만 적힌 차례로 준다.
func MemSources(sources []string) []string {
	out := []string{}
	for _, source := range sources {
		if strings.HasPrefix(source, SourceMem) {
			out = append(out, strings.TrimSpace(strings.TrimPrefix(source, SourceMem)))
		}
	}
	return out
}

// LineCites 는 본문 줄마다 그 줄이 단 근거 id 들이다. 빈 줄은 건너뛴다.
// 두 번째 값은 번호 줄이 아닌 줄의 수다.
func LineCites(body string) ([][]string, int) {
	cites := [][]string{}
	stray := 0
	for _, line := range strings.Split(body, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !numberedLine.MatchString(line) {
			stray++
			continue
		}
		ids := []string{}
		for _, found := range citePattern.FindAllStringSubmatch(line, -1) {
			ids = append(ids, found[1])
		}
		cites = append(cites, ids)
	}
	return cites, stray
}
