package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"strconv"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/safe"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var showBools = []string{"json", "no-index", "from-archive", "raw"}
var showValues = []string{"repo", "head"}

func init() {
	register(command{name: "show", run: runShow, bools: showBools, values: showValues})
}

// runShow 는 id 로 기억 한 건을 꺼낸다. 색인을 먼저 보고, 없으면 파일 자리로
// 바로 찾는다 — 방금 승격된 기억은 아직 색인에 없을 수 있다.
func runShow(argv []string) int {
	parsed, err := parseOptions(argv, showBools, showValues)
	if err != nil {
		return fail(err.Error())
	}
	if len(parsed.rest) == 0 {
		return fail(i18n.T(i18n.NeedArgument, "show <id>"))
	}
	id := parsed.rest[0]
	if !model.IsID(id) {
		return fail(i18n.T(i18n.BadID, id))
	}
	head, err := headCount(parsed)
	if err != nil {
		return fail(err.Error())
	}
	_, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	memory, usedBy, err := loadMemory(parsed, opened, id)
	if err != nil {
		return fail(err.Error())
	}
	// 조회수를 올리는 명령은 show 하나뿐이다 (설계 18-3). 실패해도 기억은
	// 기억이라 삼킨다.
	opened.AppendHit("show", id)
	return printMemory(parsed, memory, head, usedBy)
}

// headCount 는 --head 값이다. 안 줬으면 0 이고 그것은 「본문 전부」 다.
func headCount(parsed *options) (int, error) {
	if !parsed.has("head") {
		return 0, nil
	}
	text := parsed.text("head")
	value, err := strconv.Atoi(text)
	if err != nil || value < 1 {
		return 0, fmt.Errorf("%s", i18n.T(i18n.BadHeadValue, text))
	}
	return value, nil
}

// loadMemory 는 store/ 에서 꺼내고, --from-archive 면 아카이브에 남은 옛 판을
// 꺼낸다. 아카이브는 본문을 갈아 끼우기 전 판이 유일하게 남는 자리다.
func loadMemory(parsed *options, opened *store.Store, id string) (*model.Memory, *[]index.UsedByRow, error) {
	if parsed.flags["from-archive"] {
		memory, err := opened.ArchivedMemory(id)
		return memory, nil, err
	}
	path, usedBy, err := showLookup(parsed, opened, id)
	if err != nil {
		return nil, nil, err
	}
	file, err := opened.ReadMemory(path)
	if err != nil {
		// 큐를 보는 것은 **자리에 파일이 없을 때**뿐이다. 깨진 파일까지
		// 「아직 색인 전」으로 뭉개면 고칠 자리를 못 찾는다 (리뷰 2026-09-21).
		if !errors.Is(err, fs.ErrNotExist) {
			return nil, nil, err
		}
		if queued := queuedNote(opened, id); queued != "" {
			return nil, nil, fmt.Errorf("%s", queued)
		}
		return nil, nil, fmt.Errorf("%s", i18n.T(i18n.MemoryNotFound, id))
	}
	return file.Memory, usedBy, nil
}

// queuedNote 는 그 id 가 아직 색인 대기 큐에 있는지 본다. 방금 add 로 받은 id 를
// 「없다」고 하면 사람은 저장이 실패한 줄 안다 (사용 피드백 2026-09-20).
// 큐 파일이 깨졌으면 그냥 건너뛴다 — 그것은 mem index 가 따로 알린다.
func queuedNote(opened *store.Store, id string) string {
	names, err := opened.ListInbox()
	if err != nil {
		return ""
	}
	for _, name := range names {
		item, err := opened.ReadInbox(name)
		if err != nil || item.Add == nil {
			continue
		}
		if model.QueueID(item.Name, item.Add.Body, item.Add.Date) == id {
			return i18n.T(i18n.ShowQueued, id)
		}
	}
	return ""
}

// showJSON 은 화면에 낼 것만 담는다. model.Memory 를 그대로 품어 칸 이름이
// 지금과 같고, used_by 는 색인이 만든 파생값이라 파일에 안 나간다.
//
// **`used_by: []` 와 키 없음은 다르다** — 앞엣것은 「센 결과가 0건」이고
// 뒤엣것은 「못 셌다」다. 색인이 없거나 방금 다시 만들어졌을 때 0건이라고
// 말하면 읽는 쪽이 근거를 삼은 기억이 없다고 믿는다 (리뷰 #4).
type showJSON struct {
	*model.Memory
	UsedBy *[]index.UsedByRow `json:"used_by,omitempty"`
}

// printMemory 는 --json 이면 한 줄 JSON, 아니면 머리말이 붙은 마크다운이다.
func printMemory(parsed *options, memory *model.Memory, head int, usedBy *[]index.UsedByRow) int {
	if head > 0 {
		memory = withHead(memory, head)
	}
	// 기억 본문은 남이 쓴 글이고 show 결과는 그대로 AI 컨텍스트로 들어간다.
	// 훅과 같은 중화를 지난다. 원문 그대로 보려면 --raw 다 (설계 6-1 · M-1).
	if !parsed.flags["raw"] {
		memory = neutralized(memory)
	}
	if !parsed.flags["raw"] && usedBy != nil {
		washed := safeUsedBy(*usedBy)
		usedBy = &washed
	}
	if parsed.flags["json"] {
		data, err := json.Marshal(showJSON{Memory: memory, UsedBy: usedBy})
		if err != nil {
			return fail(err.Error())
		}
		fmt.Println(string(data))
		return exitOK
	}
	fmt.Print(string(model.Encode(memory)))
	if usedBy != nil {
		fmt.Print(usedBySection(*usedBy))
	}
	return exitOK
}

// usedByRoom 은 역참조 줄의 제목 자리에 쓰는 룬 수다 (review 의 제목 자리와 같다).
const usedByRoom = 40

// safeUsedBy 는 역참조 줄에 든 남의 글을 씻는다. show 출력은 그대로 AI 문맥으로
// 들어간다 (불변조건 I3 · neutralized 와 같은 이유).
func safeUsedBy(rows []index.UsedByRow) []index.UsedByRow {
	if len(rows) == 0 {
		return rows
	}
	out := make([]index.UsedByRow, len(rows))
	for at, row := range rows {
		row.Title = safe.Summary(row.Title, usedByRoom)
		out[at] = row
	}
	return out
}

// usedBySection 은 「나를 근거로 삼은 기억」 절이다. 0건이면 절을 통째로 안
// 찍는다 — 대개 0건이라 빈 절이 매번 나오면 화면만 는다.
func usedBySection(rows []index.UsedByRow) string {
	if len(rows) == 0 {
		return ""
	}
	out := strings.Builder{}
	out.WriteString(fmt.Sprintf("\n%s\n\n", i18n.T(i18n.ShowUsedByHead, len(rows))))
	for _, row := range rows {
		line := fmt.Sprintf("- %s  %s  %s  %s  `%s`", row.ID, usedByWord(row.By), row.Type, row.Date, row.Title)
		if !row.Live {
			line += " " + i18n.T(i18n.ShowUsedByDead)
		}
		out.WriteString(line + "\n")
	}
	return out.String()
}

// usedByWord 는 근거로 걸렸나 링크로 걸렸나다.
func usedByWord(by string) string {
	if by == index.UsedByLink {
		return i18n.T(i18n.ShowUsedByLink)
	}
	return i18n.T(i18n.ShowUsedBySource)
}

// neutralized 는 주입으로 읽힐 수 있는 글자를 무디게 한 복사본이다. 원래
// 기억 파일은 안 건드린다 — 화면에 찍는 것만 바꾼다.
func neutralized(memory *model.Memory) *model.Memory {
	copied := *memory
	copied.Title = safe.Neutralize(safe.OneLine(memory.Title))
	copied.Summary = safe.Neutralize(safe.OneLine(memory.Summary))
	copied.Body = safe.Text(memory.Body)
	copied.Sources = neutralList(memory.Sources)
	copied.Links = neutralList(memory.Links)
	return &copied
}

func neutralList(items []string) []string {
	if len(items) == 0 {
		return items
	}
	out := make([]string, len(items))
	for at, item := range items {
		out[at] = safe.Neutralize(safe.OneLine(item))
	}
	return out
}

// withHead 는 본문 앞 N줄만 남긴 복사본이다. 원래 기억은 안 건드린다.
func withHead(memory *model.Memory, head int) *model.Memory {
	copied := *memory
	lines := strings.Split(strings.TrimRight(memory.Body, "\n"), "\n")
	if len(lines) > head {
		lines = lines[:head]
	}
	copied.Body = strings.Join(lines, "\n")
	return &copied
}

// showPath 는 색인이 아는 자리를 먼저 쓰고, 못 찾으면 id 가 말하는 자리로 간다.
func showPath(parsed *options, opened *store.Store, id string) (string, error) {
	path, _, err := showLookup(parsed, opened, id)
	return path, err
}

// showLookup 은 자리와 역참조를 **한 번 연 색인에서** 같이 꺼낸다. 둘을 따로
// 열면 프로세스당 열기 2회 상한(설계 7-4a)을 넘긴다.
// 돌려주는 역참조가 nil 이면 「못 셌다」다 — 0건과 가른다.
func showLookup(parsed *options, opened *store.Store, id string) (string, *[]index.UsedByRow, error) {
	fallback := model.StorePath(id)
	if parsed.flags["no-index"] || !index.Exists(opened.Dir) {
		return fallback, nil, nil
	}
	// 판이 낮은 색인은 여기서 통째로 다시 만들어진다 — 그때 표는 비어 있다.
	database, rebuilt, err := index.OpenRebuilding(opened.Dir)
	if err != nil {
		if index.IsCheckFailure(err) {
			return "", nil, err
		}
		return fallback, nil, nil
	}
	defer database.Close()
	usedBy := usedByOf(database, id, rebuilt)
	row, err := database.ByID(id)
	if err != nil || row == nil {
		return fallback, usedBy, nil
	}
	return row.Path, usedBy, nil
}

// usedByOf 는 역참조를 센다. 못 셌으면 nil 이다 — 역참조는 곁다리라 못 읽어도
// 명령이 실패하지는 않지만, 0건이라고 말하지도 않는다.
func usedByOf(database *index.DB, id string, rebuilt bool) *[]index.UsedByRow {
	if rebuilt {
		return nil
	}
	rows, err := database.UsedBy(id)
	if err != nil {
		return nil
	}
	return &rows
}
