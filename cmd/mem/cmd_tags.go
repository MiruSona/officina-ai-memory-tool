package main

// mem tags — 태그 표준 목록을 손본다 (설계 6-1).
//
//	--add <태그[=상위]>      표준 목록에 넣는다
//	--add-scope <이름>       표준 scope 를 넣는다 (첫 하나를 넣으면 F09·F12 가 거절로 선다)
//	--alias <별칭=표준>      별칭을 넣는다 (add·lint 가 조용히 치환한다)
//	--alias-scope <별칭=표준> scope 별칭을 넣는다
//	--rename <옛=새>         기억 파일의 태그를 바꾼다. --dry-run 이 기본이다
//	--check                  표준 밖·못 쓰는 태그와 scope 를 센다
//	--suggest                동의어·대표말 후보를 제안한다 (아무것도 안 고친다)
//	--list                   표준 태그·scope 를 그대로 보여준다
//
// 설계 6-1 은 「목록 보기는 안 만든다 — `mem search --facet` 이 한다」였는데
// 뒤집었다. facet 은 **쓰인** 태그만 세서 표준에 있지만 아직 안 쓴 태그가 안 보인다.

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// tagsPlanFormat 은 한글이 없는 짜임새용 서식이라 i18n 표에 안 둔다.
const tagsPlanFormat = "  %s : %s → %s"

var tagsBools = []string{"check", "apply", "dry-run", "json", "suggest", "list"}
var tagsValues = []string{"add", "add-scope", "alias", "alias-scope", "rename", "repo"}

func init() {
	register(command{name: "tags", run: runTags, bools: tagsBools, values: tagsValues})
}

func runTags(argv []string) int {
	parsed, err := parseOptions(argv, tagsBools, tagsValues)
	if err != nil {
		return fail(err.Error())
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	switch {
	case parsed.has("rename"):
		return renameTag(repository, opened, parsed)
	case parsed.has("add") || parsed.has("alias") || parsed.has("add-scope") || parsed.has("alias-scope"):
		return editVocab(repository, parsed)
	case parsed.flags["list"]:
		return listTags(repository, parsed)
	case parsed.flags["check"]:
		return checkTags(repository, opened)
	case parsed.flags["suggest"]:
		return runTagsSuggest(repository, opened)
	}
	return fail(i18n.T(i18n.TagsNothing))
}

// editVocab 은 vocab.toml 을 고친다. 기억 파일은 안 건드린다.
// **락을 잡고 고친다** — 두 프로세스가 같이 쓰면 뒤에 쓴 쪽이 앞의 태그를
// 통째로 지운다. 못 잡으면 종료 6 이다 (설계 8절 · 보안연동 시험 L-2).
func editVocab(repository *config.Repository, parsed *options) int {
	release, got, err := index.TryLock(repository.Dir)
	if err != nil {
		return exitFor(err)
	}
	if !got {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.MigrateLocked))
		return exitLocked
	}
	defer release()
	vocab := vocabOf(repository)
	changed := []string{}
	if parsed.has("add") {
		tag, parent := splitPair(parsed.text("add"))
		if !model.IsTag(tag) {
			return fail(i18n.T(i18n.TagsBadTag, tag))
		}
		addStandardTag(&vocab, tag, parent)
		fmt.Println(i18n.T(i18n.TagsAdded, tag))
		changed = append(changed, tag)
	}
	if parsed.has("alias") {
		from, to := splitPair(parsed.text("alias"))
		if to == "" {
			return fail(i18n.T(i18n.TagsBadPair, parsed.text("alias")))
		}
		if !model.IsTag(from) || !model.IsTag(to) {
			return fail(i18n.T(i18n.TagsBadTag, from+"="+to))
		}
		vocab.TagAlias[from] = to
		fmt.Println(i18n.T(i18n.TagsAliased, from, to))
		changed = append(changed, from)
	}
	// scope 를 늘리는 길이 없으면 F12 거절이 막다른 골목이 된다 (실데이터 시험 A2).
	if parsed.has("add-scope") {
		wasLearning := vocab.ScopeLearning()
		scope := strings.ToLower(strings.TrimSpace(parsed.text("add-scope")))
		if !model.IsTag(scope) {
			return fail(i18n.T(i18n.TagsBadScope, scope))
		}
		if vocab.Scopes == nil {
			vocab.Scopes = map[string][]string{}
		}
		if _, found := vocab.Scopes[scope]; !found {
			vocab.Scopes[scope] = []string{}
		}
		fmt.Println(i18n.T(i18n.TagsScopeAdded, scope))
		if wasLearning {
			fmt.Println(i18n.T(i18n.TagsConfirmed, len(vocab.Scopes)))
		}
		changed = append(changed, scope)
	}
	if parsed.has("alias-scope") {
		from, to := splitPair(parsed.text("alias-scope"))
		if to == "" {
			return fail(i18n.T(i18n.TagsBadPair, parsed.text("alias-scope")))
		}
		if !model.IsTag(from) || !model.IsTag(to) {
			return fail(i18n.T(i18n.TagsBadScope, from+"="+to))
		}
		if vocab.ScopeAlias == nil {
			vocab.ScopeAlias = map[string]string{}
		}
		vocab.ScopeAlias[from] = to
		fmt.Println(i18n.T(i18n.TagsScopeAlias, from, to))
		changed = append(changed, from)
	}
	path := filepath.Join(repository.Dir, config.VocabFileName)
	if err := os.WriteFile(path, config.EncodeVocab(vocab), 0o644); err != nil {
		return exitFor(err)
	}
	fmt.Println(i18n.T(i18n.TagsSaved, strings.Join(changed, " · ")))
	return exitOK
}

// addStandardTag 는 상위가 있으면 그 밑에, 없으면 스스로 상위로 넣는다.
// 상하위는 한 겹만 둔다 (설계 결정 9).
func addStandardTag(vocab *config.Vocab, tag, parent string) {
	if vocab.Tags == nil {
		vocab.Tags = map[string][]string{}
	}
	if parent == "" {
		if _, found := vocab.Tags[tag]; !found {
			vocab.Tags[tag] = []string{}
		}
		return
	}
	for _, child := range vocab.Tags[parent] {
		if child == tag {
			return
		}
	}
	vocab.Tags[parent] = append(vocab.Tags[parent], tag)
}

// renameTag 는 기억 파일의 태그를 바꾼다. 기억을 고치는 일이라 --dry-run 이
// 기본이고, 고칠 때도 inbox 큐로만 간다 (불변조건 2).
func renameTag(repository *config.Repository, opened *store.Store, parsed *options) int {
	from, to := splitPair(parsed.text("rename"))
	if to == "" {
		return fail(i18n.T(i18n.TagsBadPair, parsed.text("rename")))
	}
	if !model.IsTag(from) || !model.IsTag(to) {
		return fail(i18n.T(i18n.TagsBadTag, from+"="+to))
	}
	files, err := opened.ListMemories()
	if err != nil {
		return exitFor(err)
	}
	plans := [][3]string{}
	for _, file := range files {
		one, err := opened.ReadListed(file)
		if err != nil {
			continue
		}
		tags, hit := replaceTag(one.Memory.Tags, from, to)
		if !hit {
			continue
		}
		plans = append(plans, [3]string{one.Memory.ID, strings.Join(one.Memory.Tags, ","), strings.Join(tags, ",")})
	}
	if len(plans) == 0 {
		fmt.Println(i18n.T(i18n.TagsRenameNone, from))
		return exitOK
	}
	for _, plan := range plans {
		fmt.Printf(tagsPlanFormat+"\n", plan[0], plan[1], plan[2])
	}
	if !parsed.flags["apply"] {
		fmt.Println(i18n.T(i18n.TagsDryRun))
		return exitOK
	}
	return applyRename(repository, opened, plans, from, to)
}

func applyRename(repository *config.Repository, opened *store.Store, plans [][3]string, from, to string) int {
	release, got, err := index.TryLock(opened.Dir)
	if err != nil {
		return exitFor(err)
	}
	if !got {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.MigrateLocked))
		return exitLocked
	}
	// 락은 vocab.toml 을 다 쓸 때까지 쥔다. 여기서 놓으면 동시에 도는
	// `mem tags --add` 가 쓴 태그가 통째로 지워진다 (리뷰 A2).
	defer release()
	if err := opened.EnsureDirs(); err != nil {
		return exitFor(err)
	}
	for _, plan := range plans {
		set := map[string]any{"tags": toAny(strings.Split(plan[2], ","))}
		if _, err := opened.WritePatch(plan[0], set); err != nil {
			return exitFor(err)
		}
	}
	// 다음부터는 옛 이름이 저절로 새 이름이 되게 별칭도 같이 남긴다.
	vocab := vocabOf(repository)
	vocab.TagAlias[from] = to
	path := filepath.Join(repository.Dir, config.VocabFileName)
	if err := os.WriteFile(path, config.EncodeVocab(vocab), 0o644); err != nil {
		return exitFor(err)
	}
	fmt.Println(i18n.T(i18n.TagsRenameDone, len(plans)))
	noteLog(opened, store.LogRenamed, fmt.Sprintf("%s → %s (%d건)", from, to, len(plans)))
	return exitOK
}

func replaceTag(tags []string, from, to string) ([]string, bool) {
	out := []string{}
	hit := false
	for _, tag := range tags {
		if tag == from {
			hit = true
			tag = to
		}
		if !contains(out, tag) {
			out = append(out, tag)
		}
	}
	return out, hit
}

// tagsListFormat 은 「상위 : 하위 · 하위」 줄이다. 한글이 없어 i18n 표에 안 둔다.
const tagsListFormat = "  %s : %s"

// tagsListJSON 은 `tags --list --json` 한 줄이다. tags 는 「상위 : 하위들」 표다.
type tagsListJSON struct {
	Parents int                 `json:"parents"`
	Total   int                 `json:"total"`
	Tags    map[string][]string `json:"tags"`
	Scopes  []string            `json:"scopes"`
}

// listTags 는 표준 목록을 그대로 보여준다. 기억 파일은 안 읽는다 — 아직 한 번도
// 안 쓴 표준 태그까지 보여야 이 명령이 뜻이 있다.
func listTags(repository *config.Repository, parsed *options) int {
	vocab := vocabOf(repository)
	if parsed.flags["json"] {
		return printJSON(tagsListJSON{Parents: len(vocab.Tags), Total: len(vocab.StandardTags()),
			Tags: sortedTagTable(vocab), Scopes: vocab.StandardScopes()})
	}
	fmt.Println(i18n.T(i18n.TagsListHead, len(vocab.Tags), len(vocab.StandardTags())))
	parents := make([]string, 0, len(vocab.Tags))
	for parent := range vocab.Tags {
		parents = append(parents, parent)
	}
	sort.Strings(parents)
	for _, parent := range parents {
		children := append([]string{}, vocab.Tags[parent]...)
		sort.Strings(children)
		if len(children) == 0 {
			fmt.Println("  " + parent)
			continue
		}
		fmt.Printf(tagsListFormat+"\n", parent, strings.Join(children, " · "))
	}
	scopes := vocab.StandardScopes()
	if len(scopes) == 0 {
		fmt.Println(i18n.T(i18n.TagsListNoScope))
		return exitOK
	}
	fmt.Println(i18n.T(i18n.TagsListScopes, len(scopes), strings.Join(scopes, " · ")))
	return exitOK
}

// sortedTagTable 은 상위마다 하위를 차례로 세운 표다. 하위가 없는 상위도
// 빈 목록으로 남긴다 — 「없다」와 「비었다」를 기계가 갈라 읽어야 한다.
func sortedTagTable(vocab config.Vocab) map[string][]string {
	table := map[string][]string{}
	for parent, children := range vocab.Tags {
		sorted := append([]string{}, children...)
		sort.Strings(sorted)
		table[parent] = sorted
	}
	return table
}

// checkTags 는 저장소가 실제로 쓰는 태그를 표준 목록과 견준다.
func checkTags(repository *config.Repository, opened *store.Store) int {
	vocab := vocabOf(repository)
	files, err := opened.ListMemories()
	if err != nil {
		return exitFor(err)
	}
	offList := map[string]int{}
	denied := map[string]int{}
	aliased := map[string]int{}
	offScope := map[string]int{}
	offType := map[string]int{}
	table := vocab.TypeTable()
	for _, file := range files {
		one, err := opened.ReadListed(file)
		if err != nil {
			continue
		}
		for _, tag := range one.Memory.Tags {
			countTag(vocab, tag, offList, denied, aliased)
		}
		if scope := one.Memory.Scope; scope != "" {
			if _, known, _ := vocab.NormalizeScope(scope); !known {
				offScope[strings.ToLower(scope)]++
			}
		}
		// 표 밖 종류를 쓴 기억은 승격·색인에서 통째로 빠진다. 세어서 알린다.
		if kind := one.Memory.Type; kind != "" && !table.Has(kind) {
			offType[strings.ToLower(kind)]++
		}
	}
	fmt.Println(i18n.T(i18n.TagsCheckHead, len(files), len(offList)))
	printCounts(i18n.TagsOffList, offList)
	printCounts(i18n.TagsDenied, denied)
	printCounts(i18n.TagsScopeOff, offScope)
	for _, kind := range sortedCountKeys(offType) {
		fmt.Println(i18n.T(i18n.TagsTypeOff, kind, offType[kind], kind))
	}
	for _, tag := range sortedCountKeys(aliased) {
		fmt.Println(i18n.T(i18n.TagsAliasUsed, tag, vocab.TagAlias[tag], aliased[tag]))
	}
	// 표준 목록을 정했는지부터 말해 준다. 경고만 나는 까닭이 여기 있다.
	// **태그와 scope 를 따로 말한다** — 둘이 따로 서기 때문이다 (리뷰 D7).
	if vocab.ScopeLearning() {
		fmt.Println(i18n.T(i18n.TagsLearning))
	} else {
		fmt.Println(i18n.T(i18n.TagsConfirmed, len(vocab.StandardScopes())))
	}
	if vocab.TagLearning() {
		fmt.Println(i18n.T(i18n.TagsTagLearning))
	} else {
		fmt.Println(i18n.T(i18n.TagsTagConfirmed, len(vocab.Tags)))
	}
	if len(offList) == 0 && len(denied) == 0 && len(offScope) == 0 && len(offType) == 0 {
		fmt.Println(i18n.T(i18n.TagsCheckClean))
		return exitOK
	}
	printConfirmSteps(offList, offScope)
	return exitCheck
}

// printConfirmSteps 는 「이대로 표준으로 삼으려면」 칠 명령을 그대로 찍는다.
// 목록에 없는 낱말을 세어 주기만 하고 늘리는 길을 안 보여주면 막다른 골목이다.
func printConfirmSteps(offList, offScope map[string]int) {
	steps := []string{}
	for _, scope := range sortedCountKeys(offScope) {
		steps = append(steps, "mem tags --add-scope "+scope)
	}
	for _, tag := range sortedCountKeys(offList) {
		steps = append(steps, "mem tags --add "+tag)
	}
	if len(steps) > tagsStepMax {
		steps = steps[:tagsStepMax]
	}
	fmt.Println(i18n.T(i18n.GateNextHead))
	for _, step := range steps {
		fmt.Println("  " + step)
	}
}

// tagsStepMax 는 다음 명령을 몇 줄까지 보여줄지다. 200가지를 다 찍으면 화면이
// 넘쳐 아무것도 안 읽힌다.
const tagsStepMax = 10

func countTag(vocab config.Vocab, tag string, offList, denied, aliased map[string]int) {
	if vocab.TagDenied(tag) {
		denied[tag]++
		return
	}
	fixed, known, isAlias := vocab.NormalizeTag(tag)
	switch {
	case isAlias:
		aliased[tag]++
	case !known:
		offList[fixed]++
	}
}

func printCounts(key i18n.Key, counts map[string]int) {
	for _, name := range sortedCountKeys(counts) {
		fmt.Println(i18n.T(key, name, counts[name]))
	}
}

func sortedCountKeys(counts map[string]int) []string {
	names := make([]string, 0, len(counts))
	for name := range counts {
		names = append(names, name)
	}
	sort.Slice(names, func(a, b int) bool {
		if counts[names[a]] != counts[names[b]] {
			return counts[names[a]] > counts[names[b]]
		}
		return names[a] < names[b]
	})
	return names
}

// splitPair 는 `왼쪽=오른쪽` 을 가른다. `=` 가 없으면 오른쪽이 빈 글이다.
func splitPair(text string) (string, string) {
	left, right, found := strings.Cut(text, "=")
	if !found {
		// `--add tag:parent` 꼴도 받는다. 사람이 둘을 헷갈린다.
		left, right, found = strings.Cut(text, ":")
		if !found {
			return strings.TrimSpace(text), ""
		}
	}
	return strings.TrimSpace(left), strings.TrimSpace(right)
}
