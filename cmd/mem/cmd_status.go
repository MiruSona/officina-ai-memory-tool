package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/gc"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/install"
	"github.com/mirusona/officina-ai-memory-tool/internal/secret"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

// unindexedShown 은 색인 안 된 파일 이름을 몇 개까지 적는지다 (설계 9-5).
const unindexedShown = 5

var statusBools = []string{"json", "quality", "db", "doctor", "log", "embed"}
var statusValues = []string{"repo", "since"}

func init() {
	register(command{name: "status", run: runStatus, bools: statusBools, values: statusValues})
}

// statusData 는 한 화면에 보여줄 것 전부다. --json 은 이것을 그대로 낸다.
type statusData struct {
	Repo       string             `json:"repo"`
	Total      int                `json:"total"`
	ByType     []index.GroupCount `json:"by_type"`
	ByStatus   []index.GroupCount `json:"by_status"`
	Pinned     int                `json:"pinned"`
	Invalid    int                `json:"invalid"`
	Hot        int                `json:"hot"`
	Warm       int                `json:"warm"`
	Cold       int                `json:"cold"`
	OnDisk     int                `json:"on_disk"`
	Megabytes  float64            `json:"db_mb"`
	LastIndex  string             `json:"last_index_at"`
	LastGC     string             `json:"last_gc_at"`
	Queued     int                `json:"queued"`
	Bad        int                `json:"bad"`
	Unindexed  []string           `json:"unindexed"`
	UnindexedN int                `json:"unindexed_count"`
	HasIndex   bool               `json:"has_index"`
	Version    string             `json:"version"`
	Schema     int                `json:"schema"`
	DBVersion  int                `json:"db_version"`
	InPath     bool               `json:"in_path"`
	HookOn     bool               `json:"hook"`
	MissingKey int                `json:"missing_keys"`
	Patterns   int                `json:"secret_patterns"`
	BadPattern int                `json:"bad_patterns"`
	Synonyms   int                `json:"synonyms"`
	Stopwords  int                `json:"stopwords"`
	Usage      map[string]int     `json:"usage"`
	Healthy    bool               `json:"healthy"`
}

func runStatus(argv []string) int {
	parsed, err := parseOptions(argv, statusBools, statusValues)
	if err != nil {
		return fail(err.Error())
	}
	repository, opened, err := openStore(parsed)
	if err != nil {
		return exitFor(err)
	}
	if code, chosen := statusSubScreen(repository, opened, parsed); chosen {
		return code
	}
	data, err := collectStatus(repository, opened, parsed)
	if err != nil {
		return exitFor(err)
	}
	// 셈을 못 남겨도 보이는 것은 그대로다.
	store.AppendHit(opened.Dir, "status", "")
	if parsed.flags["json"] {
		return printStatusJSON(data)
	}
	printStatus(data)
	if data.Healthy {
		return exitOK
	}
	fmt.Fprintln(os.Stderr, i18n.T(i18n.StatusHealthBad))
	return exitCheck
}

func collectStatus(repository *config.Repository, opened *store.Store, parsed *options) (*statusData, error) {
	data := statusData{Repo: filepath.Clean(repository.Dir), Version: i18n.Version,
		Schema: config.Schema, Usage: map[string]int{}}
	files, err := opened.ListMemories()
	if err != nil {
		return nil, err
	}
	data.OnDisk = len(files)
	queued, err := opened.ListInbox()
	if err != nil {
		return nil, err
	}
	data.Queued = len(queued)
	data.Bad = countEntries(opened.InboxBadDir())
	if err := addIndex(&data, opened, files); err != nil {
		return nil, err
	}
	addInstall(&data, repository)
	addSettings(&data, repository)
	data.Usage, _ = store.CommandCounts(opened.Dir)
	data.Healthy = healthy(&data)
	return &data, nil
}

func addIndex(data *statusData, opened *store.Store, files []store.FileInfo) error {
	if !index.Exists(opened.Dir) {
		return nil
	}
	database, err := index.Open(opened.Dir)
	if err != nil {
		return err
	}
	defer database.Close()
	data.HasIndex = true
	data.Megabytes = megabytes(index.DBPath(opened.Dir))
	if err := addCounts(data, database); err != nil {
		return err
	}
	data.LastIndex, _ = database.LastIndexAt()
	data.LastGC, _ = database.Meta(gc.MetaKey)
	data.DBVersion, _ = database.UserVersion()
	known, err := database.IndexedPaths()
	if err != nil {
		return err
	}
	data.Unindexed, data.UnindexedN = unindexedNames(files, known)
	return nil
}

func addCounts(data *statusData, database *index.DB) error {
	total, err := database.Count()
	if err != nil {
		return err
	}
	data.Total = total
	if data.ByType, err = database.CountByType(); err != nil {
		return err
	}
	if data.ByStatus, err = database.CountByStatus(); err != nil {
		return err
	}
	if data.Pinned, err = database.PinnedCount(); err != nil {
		return err
	}
	if data.Invalid, err = database.InvalidCount(time.Now()); err != nil {
		return err
	}
	states, err := database.CountByState()
	if err != nil {
		return err
	}
	data.Hot, data.Warm, data.Cold = states[index.StateHot], states[index.StateWarm], states[index.StateCold]
	return nil
}

// addInstall 은 exe 가 PATH 에서 잡히는지와 훅이 붙었는지를 본다.
func addInstall(data *statusData, repository *config.Repository) {
	machine, err := install.Paths()
	if err == nil && machine.InPath != "" {
		data.InPath = true
	}
	data.HookOn = hookAttached(filepath.Dir(repository.Dir))
}

// hookAttached 는 .claude/settings.json 안 어딘가에 우리 훅 명령이 있는지 본다.
// 읽기만 한다 — status 는 아무것도 안 고친다.
func hookAttached(root string) bool {
	raw, err := os.ReadFile(install.ClaudeSettingsPath(root))
	if err != nil {
		return false
	}
	tree := any(nil)
	if err := json.Unmarshal(raw, &tree); err != nil {
		return false
	}
	return findMemCommand(tree)
}

// findMemCommand 는 "command" 칸이 mem 인 자리를 찾는다. settings.json 의
// 생김새가 판마다 조금씩 달라서 이름으로 훑는 편이 안전하다.
func findMemCommand(node any) bool {
	switch value := node.(type) {
	case map[string]any:
		if name, ok := value["command"].(string); ok && isMemExe(name) {
			return true
		}
		return anyChild(childrenOf(value), findMemCommand)
	case []any:
		return anyChild(value, findMemCommand)
	}
	return false
}

func childrenOf(value map[string]any) []any {
	out := make([]any, 0, len(value))
	for _, child := range value {
		out = append(out, child)
	}
	return out
}

func anyChild(list []any, test func(any) bool) bool {
	for _, child := range list {
		if test(child) {
			return true
		}
	}
	return false
}

func isMemExe(name string) bool {
	base := strings.ToLower(filepath.Base(name))
	return base == install.ExeName || base == install.ExeName+".exe"
}

func addSettings(data *statusData, repository *config.Repository) {
	settings := repository.Config
	data.Patterns = len(settings.Secret.Patterns)
	both := append(append([]string{}, settings.Secret.Patterns...), settings.Secret.WarnPatterns...)
	data.BadPattern = len(secret.New(both).Skipped())
	data.Synonyms = len(settings.Synonym)
	data.Stopwords = len(settings.Stopword.Words)
	text, err := os.ReadFile(filepath.Join(repository.Dir, config.FileName))
	if err != nil {
		return
	}
	data.MissingKey = len(config.MissingKeys(string(text)))
}

// healthy 는 색인·설치·설정 줄에 문제가 없는지다. 하나라도 걸리면 종료 코드
// 2 다 (설계 9-5).
func healthy(data *statusData) bool {
	// 갓 mem init 한 저장소는 색인이 없는 게 정상이다. 첫 경험이 「저장소가
	// 고장났다」 로 시작하면 안 된다 (리뷰 C #12).
	empty := data.Total == 0 && data.Queued == 0
	if !empty && (!data.HasIndex || data.DBVersion != index.SchemaVersion) {
		return false
	}
	if data.HasIndex && data.DBVersion != index.SchemaVersion {
		return false
	}
	if data.UnindexedN > 0 || data.Bad > 0 {
		return false
	}
	if !data.InPath || !data.HookOn {
		return false
	}
	return data.MissingKey == 0 && data.BadPattern == 0
}

func printStatusJSON(data *statusData) int {
	encoded, err := json.Marshal(data)
	if err != nil {
		return fail(err.Error())
	}
	fmt.Println(string(encoded))
	if data.Healthy {
		return exitOK
	}
	return exitCheck
}

func printStatus(data *statusData) {
	fmt.Println(i18n.T(i18n.StatusRepoLine, data.Repo, ""))
	fmt.Println(i18n.T(i18n.StatusCountLine, data.Total, groupText(data.ByType)))
	fmt.Println(i18n.T(i18n.StatusStateLine, data.Pinned, data.Invalid, data.Hot, data.Warm, data.Cold))
	printStatusIndex(data)
	fmt.Println(i18n.T(i18n.StatusInstallLine, data.Version, data.Schema, data.DBVersion,
		mark(data.InPath), mark(data.HookOn)))
	fmt.Println(i18n.T(i18n.StatusConfigLine, data.MissingKey, data.Patterns,
		badPatternPart(data), data.Synonyms, data.Stopwords))
	fmt.Println(i18n.T(i18n.StatusUsageLine, usageText(data.Usage)))
}

func printStatusIndex(data *statusData) {
	if !data.HasIndex {
		fmt.Println(i18n.T(i18n.StatusIndexMissing, data.Queued))
		return
	}
	fmt.Println(i18n.T(i18n.StatusIndexLine2, data.Megabytes, orNone(data.LastIndex),
		orNone(data.LastGC), data.Queued, data.Bad))
	if data.UnindexedN == 0 {
		return
	}
	fmt.Println(i18n.T(i18n.StatusUnindexed2, data.UnindexedN, strings.Join(data.Unindexed, " · ")))
}

func badPatternPart(data *statusData) string {
	if data.BadPattern == 0 {
		return ""
	}
	return i18n.T(i18n.StatusBadPattern, data.BadPattern)
}

func groupText(groups []index.GroupCount) string {
	parts := make([]string, 0, len(groups))
	for _, group := range groups {
		parts = append(parts, fmt.Sprintf("%s %d", group.Name, group.Count))
	}
	return strings.Join(parts, " · ")
}

// usageText 는 명령 사용 횟수를 많은 것부터 적는다.
func usageText(usage map[string]int) string {
	if len(usage) == 0 {
		return i18n.T(i18n.StatusUsageNone)
	}
	names := make([]string, 0, len(usage))
	for name := range usage {
		names = append(names, name)
	}
	sort.Slice(names, func(a, b int) bool {
		if usage[names[a]] != usage[names[b]] {
			return usage[names[a]] > usage[names[b]]
		}
		return names[a] < names[b]
	})
	parts := make([]string, 0, len(names))
	for _, name := range names {
		parts = append(parts, fmt.Sprintf("%s %d", name, usage[name]))
	}
	return strings.Join(parts, " · ")
}

// unindexedNames 는 디스크에 있는데 색인에 없는 파일이다. 세는 것은 전부,
// 이름을 보여주는 것은 다섯 개까지다 (설계 9-5).
func unindexedNames(files []store.FileInfo, known map[string]bool) ([]string, int) {
	names := []string{}
	count := 0
	for _, file := range files {
		if known[file.Path] {
			continue
		}
		count++
		if len(names) < unindexedShown {
			names = append(names, filepath.Base(file.Path))
		}
	}
	return names, count
}

func countEntries(dir string) int {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}

func mark(yes bool) string {
	if yes {
		return i18n.T(i18n.StatusYes)
	}
	return i18n.T(i18n.StatusNo)
}

func megabytes(path string) float64 {
	info, err := os.Stat(path)
	if err != nil {
		return 0
	}
	return float64(info.Size()) / (1024 * 1024)
}

func orNone(value string) string {
	if value == "" {
		return i18n.T(i18n.StatusNone)
	}
	return value
}
