package main

// mem status 의 하위 화면 넷이다 (설계 6-1).
//
//	--quality  S01~S06 품질 지표 (설계 3-6 · 규칙표 4절)
//	--db       색인 표별 바이트 (G4 부피가 어디서 나오는지)
//	--doctor   환경 점검 — v0.1 의 `mem doctor` 를 여기로 접었다
//	--embed    의미 검색이 켜져 있는지 · 벡터가 몇 건인지 (v0.3)
//	--log      Memory/log.md 보기 (--since 로 자른다)
//
// 명령을 늘리지 않고 status 밑으로 접는다 — v0.1 결정 9(26 → 13)를 되돌리지
// 않는다.

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/install"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/quality"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// 아래 둘은 한글이 없는 짜임새용 서식이라 i18n 표에 안 둔다.
const (
	statusDBRowFormat     = "  %-16s %10s"
	statusDoctorRowFormat = "  %s %s%s"
)

// qualityRow 는 지표 한 줄이다.
type qualityRow struct {
	Code   string  `json:"code"`
	Name   string  `json:"name"`
	Value  float64 `json:"value"`
	Text   string  `json:"text"`
	Target string  `json:"target"`
	OK     bool    `json:"ok"`
}

// runStatusQuality 는 S01~S06 이다. 실제 기억을 다 읽어 센다.
func runStatusQuality(repository *config.Repository, opened *store.Store) int {
	memories := allMemories(opened)
	rows := qualityRows(repository, opened, memories)
	fmt.Println(i18n.T(i18n.StatusQualityHead))
	bad := 0
	for _, row := range rows {
		if !row.OK {
			bad++
		}
		fmt.Println(i18n.T(i18n.StatusQualityRow, row.Code, row.Name, row.Text, row.Target, mark(row.OK)))
	}
	if bad > 0 {
		return exitCheck
	}
	return exitOK
}

func qualityRows(repository *config.Repository, opened *store.Store, memories []*model.Memory) []qualityRow {
	total := float64(len(memories))
	vocab := vocabOf(repository)
	sourced, links, offList, tagCount, recent, defect := 0, 0, 0, 0, 0, 0
	options := quality.Options{Config: repository.Config, Vocab: vocab, Now: time.Now()}
	since := time.Now().AddDate(0, 0, -30)
	// (리뷰 B · V7) 기억마다의 규칙 검사는 **한 번만** 한다. 옛 판은 최근 것에
	// 한 번, 경고 셈에 또 한 번 돌려 20k 에서 그 한 가지가 10초였다.
	checked := make([][]quality.Finding, len(memories))
	quality.InParallel(len(memories), func(from, to int) {
		for at := from; at < to; at++ {
			checked[at] = quality.Check(memories[at], options)
		}
	})
	for at, one := range memories {
		if len(one.Sources) > 0 && !model.SourcesNoteOnly(one.Sources) {
			sourced++
		}
		links += len(one.Links)
		for _, tag := range one.Tags {
			tagCount++
			if _, known, _ := vocab.NormalizeTag(tag); !known {
				offList++
			}
		}
		if one.Migrated || !isAfter(one.Date, since) {
			continue
		}
		recent++
		if rejectLevel(checked[at]) {
			defect++
		}
	}
	// 저장소 전체 규칙은 Wide(저장소 이야기)와 ByID(기억마다 걸린 것) 둘로
	// 나뉜다. Wide 만 세면 `notation-drift` 처럼 ByID 로 옮겨 간 규칙이 통째로
	// 안 세어져 S04 가 조용히 낮아진다 (2D 넘김).
	found := quality.CheckRepo(memories, quality.RepoOptions{Options: options})
	warnings := len(found.Wide)
	for _, list := range found.ByID {
		warnings += len(list)
	}
	for at := range memories {
		warnings += len(checked[at])
	}
	added, rejected, forced := logCounts(opened)
	return []qualityRow{
		ratioRow("S01", "sources-rate", float64(sourced), total, 0.80, true),
		ratioRow("S02", "link-density", float64(links), total, 0.50, true),
		ratioRow("S03", "tag-offlist", float64(offList), float64(tagCount), 0, false),
		ratioRow("S04", "warn-per-memory", float64(warnings), total, 0.20, false),
		rejectRow("S05", "add-reject-rate", added, rejected),
		newRateRow(forced, added),
		ratioRow("S06", "quality-defect-rate", float64(defect), float64(recent), 0.10, false),
	}
}

// rejectLevel 은 걸린 것 가운데 **거절 등급**이 하나라도 있는지다.
//
// S06 은 「새로 들어온 기억의 품질 문제 비율」(G6 ④)인데, 경고까지 세면
// `add` 가 일부러 통과시킨 것을 곧바로 결함으로 세게 되어 **구조적으로 0.10
// 아래로 못 내려간다**(실데이터 시험 4-4 · C10). 두 자가 같은 것을 재면서
// 서로 어긋나면 안 되므로, S06 은 `add` 가 막았을 것만 센다.
// 경고는 S04(warn-per-memory)가 따로 센다 — 경고를 안 보는 것이 아니다.
func rejectLevel(found []quality.Finding) bool {
	for _, one := range found {
		if one.Level == quality.GradeReject {
			return true
		}
	}
	return false
}

// ratioRow 는 비율 한 줄이다. atLeast 가 참이면 목표 이상이어야 합격이다.
func ratioRow(code, name string, top, bottom, target float64, atLeast bool) qualityRow {
	value := 0.0
	if bottom > 0 {
		value = top / bottom
	}
	row := qualityRow{Code: code, Name: name, Value: value,
		Text: fmt.Sprintf("%.2f", value), Target: targetText(target, atLeast)}
	row.OK = value <= target
	if atLeast {
		row.OK = value >= target
	}
	if bottom == 0 {
		// 잴 것이 없으면 「미달」 이 아니다. 갓 만든 저장소가 고장난 것으로
		// 보이면 안 된다.
		row.OK = true
		row.Text = "—"
	}
	return row
}

// rejectRow 는 S05 다. 너무 높으면 규칙이 빡빡하다는 뜻이라 위아래를 다 본다.
func rejectRow(code, name string, added, rejected int) qualityRow {
	whole := added + rejected
	row := qualityRow{Code: code, Name: name, Target: "0.05~0.20", Text: "—", OK: true}
	// 스무 번도 안 부른 저장소의 거절 비율은 뜻이 없다. 「미달」 이라고 말하면
	// 사람이 문턱을 헛되이 푼다.
	if whole < 20 {
		return row
	}
	row.Value = float64(rejected) / float64(whole)
	row.Text = fmt.Sprintf("%.2f", row.Value)
	row.OK = row.Value >= 0.05 && row.Value <= 0.20
	return row
}

func targetText(target float64, atLeast bool) string {
	if atLeast {
		return fmt.Sprintf("≥ %.2f", target)
	}
	return fmt.Sprintf("≤ %.2f", target)
}

// logCounts 는 log.md 에서 들어온 건수 · 거절 건수 · `--new` 로 밀어 넣은
// 건수를 센다 (S05 재료 + 결정 39 의 되돌림 신호).
func logCounts(opened *store.Store) (added, rejected, forced int) {
	text, err := store.ReadLog(opened.Dir)
	if err != nil {
		return 0, 0, 0
	}
	for _, line := range strings.Split(text, "\n") {
		switch {
		case strings.HasPrefix(line, "- "+store.LogAdded):
			added++
			if strings.Contains(line, newMark) {
				forced++
			}
		case strings.HasPrefix(line, "- "+store.LogRejected):
			rejected++
		}
	}
	return added, rejected, forced
}

// newMark 는 `add --new` 로 중복 관문을 밀고 들어온 기억이라는 표시다. log.md
// 한 줄 끝에 붙는다 — 관문(C04)을 조였을 때 사람이 얼마나 자주 밀고 들어오는지가
// **되돌릴지 말지의 신호**라서 남긴다 (결정 39).
const newMark = " --new"

// newRateRow 는 S05 옆에 붙는 참고값이다. 목표가 없다 — 자가 아니라 신호다.
// 이 값이 자꾸 오르면 C04 를 너무 조인 것이다.
func newRateRow(forced, added int) qualityRow {
	row := qualityRow{Code: "S05+", Name: "new-force-rate",
		Target: "참고값(C04 되돌림 신호)", Text: "—", OK: true}
	if added == 0 {
		return row
	}
	row.Value = float64(forced) / float64(added)
	row.Text = fmt.Sprintf("%.2f", row.Value)
	return row
}

func isAfter(date string, since time.Time) bool {
	when, err := time.ParseInLocation(model.DayLayout, date, time.Local)
	return err == nil && when.After(since)
}

func allMemories(opened *store.Store) []*model.Memory {
	files, err := opened.ListMemories()
	if err != nil {
		return nil
	}
	out := make([]*model.Memory, 0, len(files))
	for _, file := range files {
		one, err := opened.ReadListed(file)
		if err != nil {
			continue
		}
		out = append(out, one.Memory)
	}
	return out
}

// runStatusDB 는 색인 표별 바이트다. 87.6MB 가 어느 표에서 나오는지 모르면
// 부피를 못 줄인다 (G4).
func runStatusDB(opened *store.Store) int {
	if !index.Exists(opened.Dir) {
		fmt.Println(i18n.T(i18n.StatusDBNone))
		return exitCheck
	}
	database, err := index.Open(opened.Dir)
	if err != nil {
		return exitFor(err)
	}
	defer database.Close()
	fmt.Println(i18n.T(i18n.StatusDBHead, megabytes(index.DBPath(opened.Dir))))
	rows, err := database.SQL().Query(
		`SELECT name, SUM(pgsize) AS bytes FROM dbstat GROUP BY name ORDER BY bytes DESC`)
	if err != nil {
		// dbstat 가 없는 빌드도 있다. 그때는 표 이름과 줄 수라도 보여준다.
		return printTableRows(database)
	}
	defer rows.Close()
	for rows.Next() {
		name := ""
		bytes := int64(0)
		if err := rows.Scan(&name, &bytes); err != nil {
			return exitFor(err)
		}
		fmt.Printf(statusDBRowFormat+"\n", name, humanBytes(bytes))
	}
	printVectorRow(opened)
	return exitOK
}

// printVectorRow 는 `vectors.bin` 줄이다. **`index.db` 와 똑같은 파생물**이라
// 여기 서야 한다 (결정 13 · 스트레스 V9). 파일이 없으면 없다고 적는다 — 줄이
// 통째로 빠지면 사람이 「이 판에는 그런 게 없다」고 읽는다.
func printVectorRow(opened *store.Store) {
	info, err := os.Stat(embed.VectorPath(opened.Dir))
	if err != nil {
		fmt.Printf(statusDBRowFormat+"\n", embed.VectorFileName,
			"없음 (의미 검색을 안 켰거나 아직 안 만들었다)")
		return
	}
	fmt.Printf(statusDBRowFormat+"\n", embed.VectorFileName,
		humanBytes(info.Size())+" (따로 있는 파일 · 위 합계 밖)")
}

// printTableRows 는 dbstat 가 없을 때의 대체 화면이다.
func printTableRows(database *index.DB) int {
	rows, err := database.SQL().Query(
		`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if err != nil {
		return exitFor(err)
	}
	names := []string{}
	for rows.Next() {
		name := ""
		if err := rows.Scan(&name); err == nil {
			names = append(names, name)
		}
	}
	rows.Close()
	for _, name := range names {
		count := 0
		row := database.SQL().QueryRow(`SELECT COUNT(*) FROM "` + name + `"`)
		if err := row.Scan(&count); err != nil {
			continue
		}
		fmt.Printf(statusDBRowFormat+"\n", name, fmt.Sprintf("%d줄", count))
	}
	return exitOK
}

func humanBytes(size int64) string {
	switch {
	case size >= 1<<20:
		return fmt.Sprintf("%.2fMB", float64(size)/(1<<20))
	case size >= 1<<10:
		return fmt.Sprintf("%.1fKB", float64(size)/(1<<10))
	}
	return fmt.Sprintf("%dB", size)
}

// runStatusDoctor 는 환경 점검이다. 아무것도 안 고친다.
func runStatusDoctor(repository *config.Repository) int {
	fmt.Println(i18n.T(i18n.StatusDoctorHead))
	bad, unsafe := 0, false
	// 기억 폴더 자리를 그대로 넘긴다. 부모에서 `Memory` 를 다시 지어 찾으면
	// `--repo <기억폴더>` 로 준 사람의 색인을 「없다」고 한다 (리뷰 D6).
	for _, check := range install.DoctorAt(filepath.Dir(repository.Dir), repository.Dir) {
		if !check.OK {
			bad++
			unsafe = unsafe || check.Security
		}
		note := ""
		if check.Note != "" {
			note = " — " + check.Note
		}
		fmt.Printf(statusDoctorRowFormat+"\n", mark(check.OK), check.What, note)
	}
	// 보안 차단은 품질 실패와 숫자를 가른다 (설계 6-1 종료 4).
	if unsafe {
		return exitSecurity
	}
	if bad > 0 {
		return exitCheck
	}
	return exitOK
}

// runStatusLog 는 log.md 를 보여준다. --since 는 그 날부터다.
func runStatusLog(opened *store.Store, parsed *options) int {
	text, err := store.ReadLog(opened.Dir)
	if err != nil {
		return exitFor(err)
	}
	if strings.TrimSpace(text) == "" {
		fmt.Println(i18n.T(i18n.StatusLogEmpty))
		return exitOK
	}
	since := parsed.text("since")
	if since != "" && !model.IsDate(since) {
		return fail(i18n.T(i18n.StatusBadSince, since))
	}
	fmt.Print(logSince(text, since))
	return exitOK
}

// logSince 는 날짜 절 단위로 자른다. 절 머리말이 `## YYYY-MM-DD` 라 글자
// 비교만으로 차례가 맞는다.
func logSince(text, since string) string {
	if since == "" {
		return text
	}
	out := strings.Builder{}
	keep := false
	for _, line := range strings.Split(text, "\n") {
		if strings.HasPrefix(line, "## ") {
			keep = strings.TrimSpace(strings.TrimPrefix(line, "## ")) >= since
		}
		if keep {
			out.WriteString(line + "\n")
		}
	}
	return out.String()
}

// statusSubScreen 은 하위 화면을 골라 돌린다. 고른 것이 없으면 거짓이다.
func statusSubScreen(repository *config.Repository, opened *store.Store, parsed *options) (int, bool) {
	switch {
	case parsed.flags["quality"]:
		return runStatusQuality(repository, opened), true
	case parsed.flags["db"]:
		return runStatusDB(opened), true
	case parsed.flags["doctor"]:
		return runStatusDoctor(repository), true
	case parsed.flags["embed"]:
		return runStatusEmbed(repository), true
	case parsed.flags["log"]:
		return runStatusLog(opened, parsed), true
	}
	return exitOK, false
}

// 도움말이 stderr 로 가야 하는 자리는 훅뿐이다. 여기서는 안 쓴다.
var _ = os.Stdout

// tildePath 는 집 폴더를 `~` 로 줄인다. 설계 7절이 사람에게 보이는 경로는
// 줄여 찍으라고 했다 (보안연동 시험 L-6).
func tildePath(path string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" || path == "" {
		return path
	}
	clean, root := filepath.Clean(path), filepath.Clean(home)
	if strings.EqualFold(clean, root) {
		return "~"
	}
	prefix := root + string(filepath.Separator)
	if len(clean) <= len(prefix) || !strings.EqualFold(clean[:len(prefix)], prefix) {
		return path
	}
	return "~/" + filepath.ToSlash(clean[len(prefix):])
}

// runStatusEmbed 은 `mem status --embed` 다. 지금 의미 검색이 켜져 있는지,
// 무엇이 모자란지, 벡터가 몇 건인지 말한다. 아무것도 안 고친다.
func runStatusEmbed(repository *config.Repository) int {
	fmt.Println(i18n.T(i18n.EmbedStatusHead))
	name := modelName()
	missing, ok := embed.CheckDeep(name)
	state := i18n.T(i18n.EmbedStateOK)
	if !ok {
		state = "모자람 : " + strings.Join(missing, ", ")
	}
	fmt.Println(i18n.T(i18n.EmbedStatusModel, name, state))
	fmt.Println(i18n.T(i18n.EmbedStatusRuntime, tildePath(embed.DLLPath())))
	vectors := vectorsFor(repository.Dir)
	stale := false
	if vectors == nil {
		fmt.Println(i18n.T(i18n.EmbedStatusNoVec))
	} else {
		fmt.Println(i18n.T(i18n.EmbedStatusVectors, vectors.Len(),
			humanBytes(fileBytes(embed.VectorPath(repository.Dir))),
			vectors.Model(), vectors.Version()))
		stale = !vectors.Matches(name, embed.ModelVersion(name), embed.Dim)
	}
	mode := i18n.T(i18n.SearchModeWord)
	if ok && vectors != nil && !stale {
		mode = i18n.T(i18n.SearchModeMeaning)
	}
	fmt.Println(i18n.T(i18n.EmbedStatusMode, mode))
	if stale {
		fmt.Println(i18n.T(i18n.EmbedStatusStale))
	}
	if !ok {
		fmt.Println(i18n.T(i18n.EmbedNeedsInstall))
		return exitCheck
	}
	return exitOK
}

func fileBytes(path string) int64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return info.Size()
}
