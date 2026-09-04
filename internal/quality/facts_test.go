package quality

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// t11Path 는 「뜻은 가깝고 사실은 다른 쌍」 20개다.
const t11Path = "../../testdata/quality/t11-facts.tsv"

// TestT11FactPairs 는 **서로 다른 두 기억 안에 거의 같은 한 줄이 박혀 있는데
// 그 줄의 숫자·경로·이름만 다른** 쌍을 부분 중복으로 잡지 않는지다
// (설계 결정 30 의 2단). 오거절 1건까지만 봐준다.
//
// 통째로 닮은 쌍이 아니라 **문장 정렬 1단이 데려오는 쌍**을 잰다 — 통째로 거의
// 같은 것은 v0.2 부터 그냥 중복이고 그건 이 시험의 몫이 아니다.
func TestT11FactPairs(t *testing.T) {
	pairs := loadPairs(t)
	if len(pairs) != 20 {
		t.Fatalf("T11 쌍이 20개가 아니다 : %d", len(pairs))
	}
	wrong := []string{}
	for at, pair := range pairs {
		if flagged(t11Memory(at*2, pair[0], 0), t11Memory(at*2+1, pair[1], 1)) {
			wrong = append(wrong, pair[0]+"  ↔  "+pair[1])
		}
	}
	if len(wrong) > 1 {
		t.Errorf("사실이 다른 쌍 %d개를 중복이라고 한다 (1건까지 봐준다) :\n%s",
			len(wrong), strings.Join(wrong, "\n"))
	}
	t.Logf("T11 오거절 %d / 20", len(wrong))

	// 헛시험이 아닌지 — **같은 줄을 그대로** 넣으면 잡혀야 한다.
	caught := 0
	for at, pair := range pairs {
		if flagged(t11Memory(at*2, pair[0], 0), t11Memory(at*2+1, pair[0], 1)) {
			caught++
		}
	}
	if caught < len(pairs)/2 {
		t.Errorf("같은 줄을 넣어도 %d/%d 밖에 안 잡는다 — 시험이 헛돈다", caught, len(pairs))
	}
	t.Logf("같은 줄일 때 잡음 %d / 20", caught)
}

// flagged 는 두 기억 사이에 중복 규칙이 켜지는지다.
func flagged(left, right *model.Memory) bool {
	table := newSimilarFor(testOptions())
	table.Add(NewDoc(left))
	return len(DuplicateFindings(NewDoc(right), table, right, testOptions())) > 0
}

// t11Filler 는 쌍 양쪽에 붙일 **서로 딴 이야기**다. 이것이 있어야 통째 점수가
// 낮게 남아 문장 정렬 1단이 판을 가른다.
// 두 기억이 통째로는 안 닮게 넉넉히 길게 둔다 — 그래야 「통째 점수로 잡히는
// 쌍」이 아니라 「문장 정렬 1단이 데려오는 쌍」을 재게 된다.
var t11Filler = [2]string{
	"이 판에서 고친 것은 색인 쪽이다.\n스키마를 올리고 빠진 열을 채웠다.\n" +
		"마이그레이션은 한 번만 돌고 다시 부르면 아무 일도 안 한다.\n" +
		"되돌리려면 색인 파일을 지우고 처음부터 다시 만들면 된다.\n" +
		"열을 늘리는 대신 딸림표를 새로 만들어 붙였다.\n" +
		"본문 표는 통째로 고칠 수 없어 지우고 다시 넣는 길로 갔다.\n" +
		"쓰기는 언제나 락을 쥔 하나만 한다.\n" +
		"나머지 갈래는 받은 것을 임시 자리에 두고 이름만 바꾼다.\n" +
		"읽기는 여덟 갈래로 갈라 한꺼번에 한다.\n" +
		"머리말을 못 읽은 파일은 고치지도 옮기지도 지우지도 않는다.\n" +
		"규격을 지킨 것만 색인에 올린다.\n" +
		"0건이면 왜 0건인지 말한다.",
	"이 판에서 고친 것은 훅 쪽이다.\n세션이 열릴 때 붙는 블록 문구를 다듬었다.\n" +
		"실패해도 조용히 나가고 사람이 볼 것은 로그 한 줄뿐이다.\n" +
		"따라잡기는 시간이 넘으면 그 자리에서 그만둔다.\n" +
		"락은 기다리지 않는다. 이미 누가 쥐고 있으면 그냥 물러난다.\n" +
		"저장소 전체를 훑는 길은 아예 만들지 않았다.\n" +
		"주는 글은 한 덩어리로 완성해서 한 번에 내보낸다.\n" +
		"밖에서 온 글은 모두 중화 관문을 지난다.\n" +
		"비밀정보로 보이는 것은 값을 안 찍는다.\n" +
		"도움말은 다른 출구로 내보내 주는 글과 안 섞이게 했다.\n" +
		"터지면 잡아서 조용히 0으로 끝낸다.\n" +
		"예산은 다섯 갈래로 나눠 각각 상한을 뒀다.",
}

// t11Memory 는 쌍 한쪽을 기억 하나로 만든다. 태그·scope 를 일부러 같게 둬서
// 곁 몫이 최대로 실리는 가장 불리한 자리에서 잰다.
func t11Memory(at int, line string, side int) *model.Memory {
	titles := [2]string{"색인 스키마를 올린 판", "세션 훅 문구를 다듬은 판"}
	summaries := [2]string{
		"색인 스키마를 올리고 마이그레이션을 한 번만 돌게 고쳤다",
		"세션이 열릴 때 붙는 훅 블록 문구를 다듬고 실패해도 조용히 나가게 했다",
	}
	return &model.Memory{ID: t11ID(at), Type: model.TypeHistory, Scope: "aimemorytool",
		Title: titles[side], Summary: summaries[side], Tags: []string{"decision-table", "aimemory"},
		Body: t11Filler[side] + "\n\n" + line + "\n"}
}

func t11ID(at int) string {
	letters := "0123456789abcdef"
	tail := []byte("00000000")
	for pos := 7; pos >= 0 && at > 0; pos-- {
		tail[pos] = letters[at%16]
		at /= 16
	}
	return "20260823-" + string(tail)
}

func loadPairs(t *testing.T) [][2]string {
	t.Helper()
	raw, err := os.ReadFile(filepath.FromSlash(t11Path))
	if err != nil {
		t.Fatalf("T11 자료를 못 읽었다 : %v", err)
	}
	pairs := [][2]string{}
	for _, line := range strings.Split(strings.ReplaceAll(string(raw), "\r\n", "\n"), "\n") {
		if strings.TrimSpace(line) == "" || strings.HasPrefix(line, "#") {
			continue
		}
		cells := strings.Split(line, "\t")
		if len(cells) != 2 {
			t.Fatalf("탭이 하나가 아니다 : %q", line)
		}
		pairs = append(pairs, [2]string{strings.TrimSpace(cells[0]), strings.TrimSpace(cells[1])})
	}
	return pairs
}
