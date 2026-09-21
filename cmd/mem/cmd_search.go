package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/embed"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/index"
	"github.com/mirusona/officina-ai-memory-tool/internal/lint"
	"github.com/mirusona/officina-ai-memory-tool/internal/search"
	"github.com/mirusona/officina-ai-memory-tool/internal/store"
)

var searchBools = []string{"pinned", "all", "explain", "json", "no-index", "include-held"}
var searchValues = []string{"type", "scope", "tag", "status", "severity", "since", "limit", "budget", "facet", "repo"}

func init() {
	register(command{name: "search", run: runSearch, bools: searchBools, values: searchValues})
}

// opened 는 이 프로세스가 연 저장소 한 벌이다. 색인은 한 번만 연다 (설계 7-4a).
type opened struct {
	sources    []search.Source
	settings   config.Config
	projectDir string
	closers    []func()
}

// dirOf 는 골든셋 같은 딸린 파일이 있는 저장소 폴더다.
func (o *opened) dirOf() string {
	return o.projectDir
}

// emptyIndex 는 프로젝트 색인이 0건이고 **한 번도 안 돈** 것인지다. 한 번
// 돌고 나서 0건인 것(규격을 다 어겨 다 빠진 저장소)은 색인이 깨진 것이 아니라
// 진짜 0건이라 여기 안 걸린다.
func (o *opened) emptyIndex() bool {
	for _, item := range o.sources {
		count, err := item.DB.Count()
		if err != nil || count > 0 {
			return false
		}
		when, err := item.DB.LastIndexAt()
		return err == nil && when == ""
	}
	return false
}

func (o *opened) close() {
	for _, item := range o.closers {
		item()
	}
}

func runSearch(argv []string) int {
	parsed, err := parseOptions(argv, searchBools, searchValues)
	if err != nil {
		return fail(err.Error())
	}
	repos, err := openSources(parsed)
	if err != nil {
		return exitFor(err)
	}
	defer repos.close()
	options, err := searchOptions(parsed, repos)
	if err != nil {
		return fail(err.Error())
	}
	if kind := parsed.text("facet"); kind != "" {
		return runFacet(parsed, options, kind)
	}
	result, err := search.Search(options)
	if err != nil {
		return exitFor(err)
	}
	return printSearch(parsed, result, options.Embed.Enabled)
}

// openSources 는 저장소를 열고 색인을 한 번 따라잡는다.
func openSources(parsed *options) (*opened, error) {
	working, err := os.Getwd()
	if err != nil {
		return nil, err
	}
	project, err := config.Resolve(parsed.text("repo"), working)
	if err != nil {
		return nil, err
	}
	out := opened{settings: config.Default("")}
	if project != nil {
		out.settings = project.Config
	}
	// 질의 쪽 정규화도 색인 쪽과 같은 표를 타야 한다 (결정 21).
	wireNormalize(project)
	out.add(project, parsed)
	// 빈 파일을 SQLite 는 「멀쩡한 빈 DB」 로 연다. 그러면 기억이 400건
	// 있는데도 0건이라고 답한다 — 거짓 0건이 제일 나쁘다 (스트레스시험 D4).
	if project != nil && out.emptyIndex() && store.New(project).HasMemory() {
		// 연 색인을 안 닫고 나가면 윈도우에서 그 파일을 못 지운다 —
		// 바로 이어 치는 `mem index --full` 이 실패한다.
		out.close()
		return nil, &index.UnusableError{Path: index.DBPath(project.Dir)}
	}
	if len(out.sources) == 0 {
		// 저장소는 찾았는데 색인이 없거나 깨진 것이다. 「저장소가 없다」 로
		// 말하면 사람이 `mem init` 을 다시 돌린다 (스트레스시험 D4).
		if dir := out.dirOf(); dir != "" {
			return nil, &index.UnusableError{Path: index.DBPath(dir)}
		}
		return nil, &config.NoRepositoryError{}
	}
	return &out, nil
}

// add 는 저장소 하나를 따라잡고 연다. 색인이 없거나 못 열면 조용히 건너뛴다 —
// 읽기 명령이 색인 때문에 멈추면 안 된다.
func (o *opened) add(repository *config.Repository, parsed *options) {
	if repository == nil {
		return
	}
	o.projectDir = repository.Dir
	database := openReady(repository, parsed)
	if database == nil {
		return
	}
	o.sources = append(o.sources, search.Source{DB: database})
	o.closers = append(o.closers, func() { database.Close() })
}

// openReady 는 읽을 색인을 연다. 따라잡을 것이 없으면 처음 연 핸들을 그대로
// 쓰고, 있으면 닫고 따라잡은 다음 다시 연다.
func openReady(repository *config.Repository, parsed *options) *index.DB {
	// mem.toml 의 필드 가중을 색인 쪽에 알린다. 여기서 안 알리면 파일에 적은
	// 값이 죽고 기본 16/6/1 만 돈다 (파도 B).
	index.SetFieldWeights(repository.Config.Search.FieldWeights)
	database := openIndex(repository.Dir)
	if parsed.flags["no-index"] {
		return database
	}
	if database != nil && !needCatchUp(repository, database) {
		return database
	}
	if database != nil {
		database.Close()
	}
	catchUp(repository)
	return openIndex(repository.Dir)
}

// openIndex 는 색인이 있으면 연다. 없거나 못 열면 nil 이다 — 읽기 명령이
// 색인 때문에 멈추면 안 된다.
func openIndex(dir string) *index.DB {
	if !index.Exists(dir) {
		return nil
	}
	database, err := index.Open(dir)
	if err != nil {
		return nil
	}
	return database
}

// needCatchUp 은 읽기 전에 따라잡아야 하는지다. 큐가 비었고 store/ 폴더
// 자리표가 마지막 색인 때와 같으면 파일 2만 개를 stat 할 이유가 없다
// (20k 에서 224ms).
func needCatchUp(repository *config.Repository, database *index.DB) bool {
	opened := store.New(repository)
	names, err := opened.ListInbox()
	if err != nil || len(names) > 0 {
		return true
	}
	return !index.StoreUnchanged(database, opened)
}

// catchUp 은 큐를 승격하고 바뀐 파일을 색인한다. 락을 못 잡으면 그냥 있는
// 것을 읽는다.
func catchUp(repository *config.Repository) {
	opened := store.New(repository)
	index.Run(index.Options{Store: opened, GC: repository.Config.GC,
		Secret: repository.Config.Secret, Quiet: true})
}

func searchOptions(parsed *options, repos *opened) (search.Options, error) {
	narrow, err := filterOf(parsed)
	if err != nil {
		return search.Options{}, err
	}
	options := search.Options{
		Sources: repos.sources, Query: joinWords(parsed.rest), Filter: narrow,
		Stopwords: repos.settings.Stopword.Words, Synonym: repos.settings.Synonym,
		Explain: parsed.flags["explain"], MissPath: missPath(repos),
		RRFK: repos.settings.Search.RRFK, BonusCap: repos.settings.Search.BonusCap,
		// 재순위·동의어 손잡이와 임베딩 사다리를 그대로 넘긴다. 안 넘기면
		// mem.toml 에 적은 값이 죽고 코드 기본값만 돈다 (파도 C).
		Search: repos.settings.Search, Embed: repos.settings.Embed,
		RepoDir: repos.dirOf(), Types: config.TypesIn(repos.dirOf()),
	}
	// 의미 재정렬을 꽂는다. 벡터 파일이 없으면 nil 이고 낱말 모드로 답한다.
	if near := rerankerFor(repos.dirOf(), repos.sources); near != nil {
		options.Vectors = near
		repos.closers = append(repos.closers, near.Close)
	}
	if value := parsed.text("limit"); value != "" {
		count, err := strconv.Atoi(value)
		if err != nil || count <= 0 {
			return options, errors.New(i18n.T(i18n.NeedArgument, "--limit"))
		}
		if count > search.MaxLimit {
			count, options.LimitCapped = search.MaxLimit, true
		}
		options.Limit = count
	}
	return options, nil
}

// missPath 는 0건 질의를 남길 자리다. 프로젝트 저장소가 있을 때만 남긴다 —
// lint 의 synonym-candidate 가 이 파일을 읽는다 (설계 6-7 · 리뷰B #6).
func missPath(repos *opened) string {
	if repos.projectDir == "" {
		return ""
	}
	return filepath.Join(store.LocalDir(repos.projectDir), lint.MissFileName)
}

func filterOf(parsed *options) (index.Filter, error) {
	narrow := index.Filter{
		Types: parsed.list("type"), Scope: parsed.text("scope"), Tags: parsed.list("tag"),
		Status: parsed.text("status"), Severity: severityOf(parsed.text("severity")),
		All: parsed.flags["all"], Pinned: parsed.flags["pinned"],
		// 보류(`review: true`)는 기본으로 안 뜬다. `--all` 은 무효·덮인 것을
		// 여는 옵션이라 뜻이 다르다 — 따로 켠다 (결정 6).
		IncludeHeld: parsed.flags["include-held"],
	}
	since, ok := search.ParseSince(parsed.text("since"))
	if !ok {
		return narrow, errors.New(i18n.T(i18n.SearchBadSince, parsed.text("since")))
	}
	narrow.Since = since
	return narrow, nil
}

func joinWords(words []string) string {
	out := ""
	for at, word := range words {
		if at > 0 {
			out += " "
		}
		out += word
	}
	return out
}

func runFacet(parsed *options, options search.Options, kind string) int {
	if kind != "scope" && kind != "tag" {
		return fail(i18n.T(i18n.SearchBadFacet, kind))
	}
	// --facet 은 저장소 전체의 분포다. 같이 친 낱말은 안 쓴다 — 조용히
	// 무시하면 사람은 그 낱말로 좁힌 분포라고 읽는다 (리뷰B #30).
	if len(parsed.rest) > 0 && !parsed.flags["json"] {
		fmt.Println(i18n.T(i18n.SearchFacetWords, joinWords(parsed.rest)))
	}
	counts, err := search.Facet(options, kind)
	if err != nil {
		return exitFor(err)
	}
	if parsed.flags["json"] {
		return printJSON(counts)
	}
	fmt.Print(search.FacetMarkdown(counts))
	return exitOK
}

func printSearch(parsed *options, result *search.Result, wantEmbed bool) int {
	if parsed.flags["explain"] {
		fmt.Fprintln(os.Stderr, search.Explain(result))
	}
	if parsed.flags["json"] {
		return printJSON(search.Neutralize(result))
	}
	limit, err := budgetOf(parsed)
	if err != nil {
		return fail(err.Error())
	}
	// 낱말 모드로 **떨어졌으면** 크게 알린다. 조용히 반쪽으로 도는 것이
	// 제일 나쁘다 (사용자 결정 2026-08-23).
	//
	// **다만 `[embed] enabled = false` 는 사람의 뜻이다** (스트레스 V12).
	// 안 쓰기로 정한 사람에게 검색·색인마다 설치를 재촉하면 그것은 알림이
	// 아니라 광고다. 켜 뒀는데 모델이 없는 것만 「떨어진 것」이다.
	if result.Mode == search.ModeWord && wantEmbed && !embed.Ready(modelName()) {
		fmt.Fprintln(os.Stderr, i18n.T(i18n.EmbedNeedsInstall))
	}
	fmt.Print(search.Markdown(result, limit))
	return exitOK
}

// budgetOf 는 --budget 값이다. 못 읽는 값은 조용히 무제한이 되면 안 된다 —
// 예산을 걸었다고 믿은 채로 답이 통째로 나온다 (리뷰B #29).
func budgetOf(parsed *options) (int, error) {
	text := parsed.text("budget")
	if text == "" {
		return 0, nil
	}
	value, err := strconv.Atoi(text)
	if err != nil || value <= 0 {
		return 0, errors.New(i18n.T(i18n.SearchBadBudget, text))
	}
	return value, nil
}

func printJSON(payload any) int {
	text, err := json.Marshal(payload)
	if err != nil {
		return fail(err.Error())
	}
	fmt.Println(string(text))
	return exitOK
}
