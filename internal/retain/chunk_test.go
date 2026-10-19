package retain

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// exchanges 는 사용자·AI 한 쌍씩 n 번 주고받은 발화 목록이다. 글 길이는 같다.
func exchanges(n int, size int) []Utterance {
	list := []Utterance{}
	for at := 0; at < n; at++ {
		mark := string(rune('A' + at))
		list = append(list, Utterance{Role: RoleUser, Text: mark + strings.Repeat("u", size)},
			Utterance{Role: RoleAssistant, Text: mark + strings.Repeat("a", size)})
	}
	return list
}

func TestChunksKeepsExchangeBoundary(t *testing.T) {
	list := exchanges(4, 10)
	one := len([]rune(chunkExchange(list[0:2])))
	chunks, dropped := Chunks(list, one*2+1, 10)
	if dropped != 0 || len(chunks) != 2 {
		t.Fatalf("두 주고받기씩 둘로 나눠야 한다 : %d 토막 · 버림 %d", len(chunks), dropped)
	}
	if !strings.HasPrefix(chunks[0], HeadUser+"\nA") || !strings.Contains(chunks[1], "\nD") {
		t.Fatalf("시간순(앞→뒤)이어야 한다 : %q", chunks)
	}
	if !strings.Contains(chunks[0], HeadAssistant+"\nB") {
		t.Fatalf("토막 안은 머리 줄을 단 줄들이다 : %q", chunks[0])
	}
	for _, chunk := range chunks {
		if strings.Count(chunk, HeadUser) != 2 {
			t.Fatalf("주고받기 중간에서 끊었다 : %q", chunk)
		}
	}
}

func TestChunksTakesFromTheEnd(t *testing.T) {
	list := exchanges(5, 10)
	one := len([]rune(chunkExchange(list[0:2])))
	chunks, dropped := Chunks(list, one, 2)
	if len(chunks) != 2 || dropped != 3 {
		t.Fatalf("끝에서 2 토막만, 앞 셋은 버린다 : %d · %d", len(chunks), dropped)
	}
	if !strings.Contains(chunks[0], "\nD") || !strings.Contains(chunks[1], "\nE") {
		t.Fatalf("뒤쪽 토막이어야 한다 : %q", chunks)
	}
}

func TestChunksCutsLongExchangeAtAIEnd(t *testing.T) {
	list := []Utterance{{Role: RoleUser, Text: "짧은 물음"}, {Role: RoleAssistant, Text: "답머리" + strings.Repeat("가", 500) + "끝표시"}}
	chunks, dropped := Chunks(list, 100, 6)
	if len(chunks) != 1 || dropped != 0 {
		t.Fatalf("혼자 넘는 주고받기도 한 토막으로 넣는다 : %d · %d", len(chunks), dropped)
	}
	if utf8.RuneCountInString(chunks[0]) > 100 {
		t.Fatalf("상한을 넘었다 : %d", utf8.RuneCountInString(chunks[0]))
	}
	if !strings.Contains(chunks[0], "짧은 물음") || !strings.Contains(chunks[0], "답머리") || strings.Contains(chunks[0], "끝표시") {
		t.Fatalf("AI 글 끝만 잘라야 한다 : %q", chunks[0])
	}
}

func TestChunksLeadingAssistantAndEmpty(t *testing.T) {
	list := append([]Utterance{{Role: RoleAssistant, Text: "앞선 AI 글"}}, exchanges(1, 3)...)
	chunks, _ := Chunks(list, 10000, 6)
	if len(chunks) != 1 || !strings.HasPrefix(chunks[0], HeadAssistant+"\n앞선 AI 글") {
		t.Fatalf("첫 사용자 말 앞 AI 글도 넣는다 : %q", chunks)
	}
	if chunks, dropped := Chunks(nil, 100, 6); len(chunks) != 0 || dropped != 0 {
		t.Fatal("빈 목록은 토막 0 이다")
	}
}
