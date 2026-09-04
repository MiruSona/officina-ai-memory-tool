// Package config reads mem.toml and finds the repository root by walking up
// from the working directory.
package config

import (
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// MissingKeys lists the "절.키" names Default writes that text does not have.
// Lint and install both call this, so they can never disagree about what
// counts as an old mem.toml (design: config-stale).
func MissingKeys(text string) []string {
	have := tomlKeySet(text)
	want := tomlKeySet(string(Encode(Default("x"))))
	missing := make([]string, 0, len(want))
	for _, key := range sortedStrings(want) {
		if !have[key] {
			missing = append(missing, key)
		}
	}
	return missing
}

// tomlKeySet scans `[절]` and `키 = 값` lines only, into a "절.키" set. A
// section with no keys of its own gives none, so an empty [synonym] or
// [score] block is skipped on its own.
func tomlKeySet(text string) map[string]bool {
	keys := map[string]bool{}
	section := ""
	for _, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			section = strings.Trim(line, "[]")
			continue
		}
		name, _, found := strings.Cut(line, "=")
		if !found {
			continue
		}
		keys[section+"."+strings.TrimSpace(name)] = true
	}
	return keys
}

func sortedStrings(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for key := range set {
		out = append(out, key)
	}
	sort.Strings(out)
	return out
}

// Schema 는 이 exe 가 아는 기억 파일 규격 판이다. v0.2 에서 2 로 올렸다 —
// `author`·`sources`·`todo_status` 가 생겼다 (설계 2-2).
const Schema = 2

// Tokenizer names the search scheme the index was built with. A different value
// in mem.toml means the index must be rebuilt.
const Tokenizer = "bigram-ko3col+word-en3col+contentless+norm"

// FileName is the settings file that marks a repository root.
const FileName = "mem.toml"

// SecretConfig holds the patterns that block a memory from being stored, plus
// the ones only lint warns about (design 3-4).
type SecretConfig struct {
	Patterns     []string
	WarnPatterns []string
}

// GCConfig holds the cleanup thresholds. Every step needs both count and days.
type GCConfig struct {
	WarmCount int
	WarmDays  int
	ColdCount int
	ColdDays  int
	// HitDays is how recently a search hit keeps a memory exempt.
	HitDays int
	// ArchiveDays and ArchiveMB together gate the delete step.
	ArchiveDays int
	ArchiveMB   int
	// BatchLimit caps how many memories one run may move.
	BatchLimit int
	// BatchPercent raises that cap for a big repo: the real cap is
	// max(BatchLimit, 전체 기억 수 * BatchPercent/100).
	BatchPercent int
	// FoldAfterDays 는 이 나이를 넘으면 접기 후보로 보는 날 수다 (설계 6-2).
	FoldAfterDays int
	// DBMaxMB 는 index.db 부피 상한이다. 넘으면 status 가 알린다 (G4).
	DBMaxMB int
}

// BudgetConfig holds the token target per hook occasion.
type BudgetConfig struct {
	Startup int
	Clear   int
	Resume  int
	Compact int
	Fork    int
	// Subagent 는 SubagentStart 훅 몫이다. 세션 시작과 계기가 달라 따로 둔다.
	Subagent int
}

// StopwordConfig 는 검색에서 AND 로 강제하지 않을 흔한 낱말이다. 빼는 게
// 아니라 구절 가산에만 쓴다 (설계 4-4 · 조사E 유형 D).
type StopwordConfig struct {
	Words []string
}

// HookConfig 는 훅에서 켜고 끄는 것이다. UserPrompt 는 기본 꺼짐이다 (설계 7-4).
type HookConfig struct {
	UserPrompt bool
	// MaxBytes 는 주입 블록의 UTF-8 바이트 상한이다. 넘으면 우리가 먼저
	// 자른다 — 상한을 넘기면 앞 2,000자만 남고 말없이 사라진다 (설계 결정 23).
	MaxBytes int
	// LinesPerSection 은 주입 블록 한 절에 넣을 줄 수다.
	LinesPerSection int
	// SubagentStart 는 서브에이전트가 시작할 때도 블록을 넣을지다.
	SubagentStart bool
	// SubagentMaxBytes 는 그 블록의 바이트 상한이다.
	SubagentMaxBytes int
	// SubagentSkip 에 든 agent_type 에는 아무것도 안 넣는다. settings.json 의
	// matcher 를 좁히는 대신 여기서 고른다 — settings.json 은 환경 변경이다.
	SubagentSkip []string
}

// PinConfig caps how many memories may stay pinned at once.
type PinConfig struct {
	Max int
}

// ScoreConfig holds the score weights a person may turn down. Only the
// importance term is settable so far; 0 kills it (design 2-1 #23c).
type ScoreConfig struct {
	Importance float64
}

// SearchConfig 는 검색 랭킹의 두 손잡이다. 설계 6-4·6-5 가 적은 값과 다르게
// 기본값을 잡은 자리라 사람이 mem.toml 에서 되돌릴 수 있어야 한다.
type SearchConfig struct {
	// RRFK 는 RRF 의 완충값이다. 설계는 60(Elasticsearch 기본값)을 적었지만
	// 그 값은 수천 줄 목록을 합칠 때 것이라 우리 50줄 목록에서는 1위와 50위가
	// 거의 안 갈려 가산이 관련도를 덮는다 (조사E 유형 C).
	RRFK float64
	// BonusCap 은 가산의 상한이다. 설계 6-5 가 1.5·1.8·2.2 를 재고 고르랬다.
	BonusCap float64
	// FieldWeights 는 제목·메타(태그+scope)·요약·본문 순 가중이다
	// (설계 결정 16 · v0.4 결정 6·7 로 열이 넷이 됐다).
	FieldWeights [4]float64
	// K1·B 는 재순위용 bm25 손잡이다. FTS5 bm25 는 이 둘을 못 바꿔서
	// 상위 RerankTop 건을 우리가 다시 매긴다 (설계 결정 18).
	K1 float64
	B  float64
	// RerankTop 은 다시 매길 건수다.
	RerankTop int
	// AbstainFloor 는 이 아래면 "없다" 고 말하는 선이다 (G2).
	AbstainFloor float64
	// SynWeight 는 동의어로 찾은 것의 랭킹 몫이다.
	SynWeight float64
}

// 검색 손잡이의 기본값. 206건 골든셋 실측으로 골랐다 (설계 6-4·6-5).
const (
	DefaultRRFK     = 10.0
	DefaultBonusCap = 1.5
)

// v0.2 가 새로 잡은 검색 상수 (설계 6-2 · 결정 16·18).
// 재순위는 우리 bm25 로 상위 200건을 다시 매기되 b=0.4 다 — FTS5 bm25 는
// k1·b 를 못 바꾼다.
//
// 필드 가중은 v0.2 의 16/6/1(제목+태그+scope / 요약 / 본문)이 v0.4 에서
// 제목·메타·요약·본문 넷으로 갈렸다. 값은 골든셋 3벌 × 두 모드 격자 스윕으로
// 다시 골랐다 (v0.4 결정 7 · 갈래 A 보고).
// 12/3/4/1 은 골든셋 3벌 × 두 모드 격자 스윕(83점)에서 고른 값이다. v2 80건
// r@5 는 어느 자리에서도 0.7612(의미)·0.7164(낱말)로 같았다 — 열 가르기가
// r@5 천장을 못 민다. 갈린 것은 MRR 과 오배제다. 요약 가중이 4 아래로 내려가면
// 낱말 모드 r@5 가 0.7015 로 떨어져 4 가 하한이다 (갈래 A 측정).
var DefaultFieldWeights = [4]float64{12, 3, 4, 1}

// MetaWeightSlot 은 v0.4 에서 새로 생긴 열(메타)의 자리다. 옛 mem.toml 이
// 세 값만 적었을 때 이 자리에 기본값을 채운다.
const MetaWeightSlot = 1

const (
	DefaultK1           = 1.2
	DefaultB            = 0.4
	DefaultRerankTop    = 200
	DefaultAbstainFloor = 0.15
	DefaultSynWeight    = 0.5
)

// 임베딩 기본값 (설계 6-2 `[embed]`). 표가 없으면 어차피 안 돈다.
const (
	// DefaultEmbedPath 는 **낱말 임베딩 표(ko.bin)** 자리다. 비면 저장소 안
	// `Memory/model/ko.bin` 을 본다.
	// **의미 검색 모델(ONNX)은 이 칸과 상관없다** — 그쪽은 `mem install` 이
	// `~/.aimemory/models/<모델>/` 에 깔고 `status --embed` 가 보여준다.
	// 옛 기본값 `~/.aimemory/model/ko.bin` 은 아무 데도 없는 자리를 가리켜
	// 설정 파일이 거짓말을 하고 있었다 (스트레스 V12).
	DefaultEmbedPath  = ""
	DefaultEmbedFloor = 0.35
	// 1.5 는 3A 가 골든셋 v2 80건으로 잰 값이다 : 0.6→1.5 에서 r@5 0.731→0.746 ·
	// ko/en 격차 0.079→0.005 이고, 5.0 까지 올리면 오배제가 0.015 로 G2 가 깨진다.
	DefaultEmbedRRFWeight = 1.5
)

// RepoConfig 는 이 저장소가 자기에 대해 아는 것이다 (설계 6-2 `[repo]`).
type RepoConfig struct {
	// Scopes 는 이 저장소가 등록해 쓰는 scope 다. 표준 목록의 주인은
	// vocab.toml 이고, 여기 값은 그 위에 이 저장소 몫을 더한다 (규칙 F12).
	Scopes []string
	// PinMax 는 고정할 수 있는 기억 수다. v0.1 의 `[pin] max` 와 같은 값이라
	// 둘 중 큰 쪽이 아니라 `[repo] pin_max` 를 먼저 본다.
	PinMax int
}

// EmbedConfig 는 임베딩 사다리다. 조건부라 기본은 꺼져 있고, 모델 파일이
// 없으면 조용히 낱말 검색만 한다 (설계 결정 21).
type EmbedConfig struct {
	Enabled bool
	Path    string
	// Model 은 의미 검색 모델 이름이다. 비면 embed.DefaultModel 이다.
	// `mem install --model` 은 모델을 깔 뿐이고, 검색이 어느 것을 쓸지는
	// 이 칸이 정한다. 환경변수 MEM_EMBED_MODEL 이 있으면 그것이 제일 세다.
	Model     string
	Floor     float64
	RRFWeight float64
}

// QualityConfig 는 문서 품질 장치의 문턱이다 (설계 3-2 · 품질규칙표).
type QualityConfig struct {
	// DupReject·DupWarn 은 닮음 점수 S 의 두 문턱이다 (규칙 C01·C02).
	DupReject float64
	DupWarn   float64
	// SimhashHamming 은 후보 좁히기용 해밍 거리다. 판정자가 아니다.
	SimhashHamming int
	BodyMinLines   int
	BodyWarn       int
	BodyMax        int
	TagMin         int
	TagMax         int
	TagBroadRatio  float64
	ScopeSkewRatio float64
	ColdDays       int
	// DupVectors 는 중복 판정에 임베딩을 쓸지다. **기본 꺼짐** (설계 18-7).
	// 켜면 닮음 점수에 벡터 항이 얹히고 벡터가 후보도 데려온다. 재측정에서
	// 대조군 거짓 경보 3건(G6② 깨짐) · 실데이터 40건 오거절 15% 가 나와
	// 「문서 품질 > 검색」 원칙대로 껐다. 검색 재정렬은 이 스위치와 무관하다.
	DupVectors bool
	// PrecisionDemote·PrecisionOff 는 규칙을 스스로 강등·차단하는 선이다.
	// 정밀도가 낮은 규칙을 켜 두면 사람이 경고 전체를 무시하기 시작한다
	// (설계 결정 14).
	PrecisionDemote float64
	PrecisionOff    float64
}

// 품질 문턱 기본값 (설계 3-2 문턱 한 표).
//
// dup_reject·dup_warn 은 설계가 적은 0.72/0.55 가 아니다. 2물결 D 가 실제
// 기억 206건으로 자카드 S 를 다 재 보니 **가장 닮은 짝이 0.248** 이라
// 0.72/0.55 로는 한 건도 안 걸렸다. 다른 말로 쓴 중복은 낱말이 안 겹쳐
// 자카드로 못 잡으니 그것은 임베딩 몫으로 미루고, 문턱은 실측 분포에
// 맞춰 내렸다. 확정은 `mem eval --tune-dup` 이 한다.
//
// **(리뷰 B) 0.22/0.17 → 0.08/0.06.** 태그·scope 몫을 그냥 더하지 않고 내용이
// 닮은 만큼만 싣게 고치면서(quality/similar.go) S 분포가 통째로 내려갔다.
// 같은 골든셋에서 대조군 거짓 경보가 **3건 → 0건**, DUP 재현율은 0.500(경고선
// 0.17) → **0.528** 이다. 값은 `mem eval --tune-dup` 곡선에서 그대로 왔다.
const (
	DefaultDupReject = 0.08
	DefaultDupWarn   = 0.06
	// DefaultDupVectors 는 중복 판정의 벡터 항 기본값이다. **꺼짐** (설계 18-7).
	DefaultDupVectors      = false
	DefaultSimhashHamming  = 8
	DefaultBodyMinLines    = 3
	DefaultBodyWarn        = 120
	DefaultBodyMax         = 300
	DefaultTagMin          = 2
	DefaultTagMax          = 5
	DefaultTagBroadRatio   = 0.20
	DefaultScopeSkewRatio  = 0.90
	DefaultColdDays        = 180
	DefaultPrecisionDemote = 0.80
	DefaultPrecisionOff    = 0.60
)

// 훅 상한 (설계 결정 23). 10,000자가 아니라 UTF-8 8,000바이트에서 우리가 먼저
// 자른다 — 한글은 1글자 3바이트라 글자 수만 보면 3배 위험하다.
const (
	DefaultHookMaxBytes        = 8000
	DefaultHookLinesPerSection = 5
	// 서브에이전트는 한 가지 일만 하고 컨텍스트가 짧아 세션 시작의 절반이다
	// (서브에이전트훅설계 3-4).
	DefaultSubagentMaxBytes = 4000
	DefaultSubagentBudget   = 600
)

// gc 가 접기 시작하는 나이와 DB 부피 상한 (설계 6-2 `[gc]`).
const (
	DefaultFoldAfterDays = 365
	DefaultDBMaxMB       = 60
)

// DefaultImportanceWeight is what [score] importance means when nobody said.
const DefaultImportanceWeight = 0.2

// Config is one repository's mem.toml.
type Config struct {
	Schema    int
	Name      string
	Tokenizer string
	Repo      RepoConfig
	Secret    SecretConfig
	GC        GCConfig
	Budget    BudgetConfig
	Pin       PinConfig
	Score     ScoreConfig
	Search    SearchConfig
	Embed     EmbedConfig
	Quality   QualityConfig
	Stopword  StopwordConfig
	Hook      HookConfig
	// Synonym maps one query word onto the words to try instead when it found
	// nothing. Query time only: the index is never touched (design 2-1 #22).
	Synonym map[string][]string
	// Canon 은 「같은 것을 다르게 쓴 것」을 대표말 하나로 모으는 표다
	// (`인덱싱` -> `색인`). 색인 쪽과 질의 쪽이 같이 타는 정규화가 쓴다
	// (결정 21·22·25). 1:다·외래어는 여기가 아니라 Synonym 이 맡는다.
	Canon map[string]string
	// Scope maps a folder name onto the scope string memories from that folder
	// get. The hook uses it to guess a scope from the session's cwd.
	Scope map[string]string
}

// Default returns the values mem init writes.
func Default(name string) Config {
	return Config{
		Schema:    Schema,
		Name:      name,
		Tokenizer: Tokenizer,
		Repo:      RepoConfig{Scopes: []string{}, PinMax: DefaultPinMax},
		Secret:    SecretConfig{Patterns: DefaultSecretPatterns(), WarnPatterns: DefaultWarnPatterns()},
		GC: GCConfig{WarmCount: 1000, WarmDays: 90, ColdCount: 20000, ColdDays: 180,
			HitDays: 30, ArchiveDays: 0, ArchiveMB: 1024, BatchLimit: 200, BatchPercent: 2,
			FoldAfterDays: DefaultFoldAfterDays, DBMaxMB: DefaultDBMaxMB},
		Budget: BudgetConfig{Startup: 1000, Clear: 1000, Resume: 500, Compact: 800, Fork: 500,
			Subagent: DefaultSubagentBudget},
		Pin:   PinConfig{Max: DefaultPinMax},
		Score: ScoreConfig{Importance: DefaultImportanceWeight},
		Search: SearchConfig{RRFK: DefaultRRFK, BonusCap: DefaultBonusCap,
			FieldWeights: DefaultFieldWeights, K1: DefaultK1, B: DefaultB,
			RerankTop: DefaultRerankTop, AbstainFloor: DefaultAbstainFloor,
			SynWeight: DefaultSynWeight},
		Embed: EmbedConfig{Enabled: false, Path: DefaultEmbedPath,
			Floor: DefaultEmbedFloor, RRFWeight: DefaultEmbedRRFWeight},
		Quality: QualityConfig{DupReject: DefaultDupReject, DupWarn: DefaultDupWarn,
			DupVectors:     DefaultDupVectors,
			SimhashHamming: DefaultSimhashHamming, BodyMinLines: DefaultBodyMinLines,
			BodyWarn: DefaultBodyWarn, BodyMax: DefaultBodyMax,
			TagMin: DefaultTagMin, TagMax: DefaultTagMax,
			TagBroadRatio: DefaultTagBroadRatio, ScopeSkewRatio: DefaultScopeSkewRatio,
			ColdDays: DefaultColdDays, PrecisionDemote: DefaultPrecisionDemote,
			PrecisionOff: DefaultPrecisionOff},
		Stopword: StopwordConfig{Words: DefaultStopwords()},
		Hook: HookConfig{UserPrompt: false, MaxBytes: DefaultHookMaxBytes,
			LinesPerSection: DefaultHookLinesPerSection, SubagentStart: true,
			SubagentMaxBytes: DefaultSubagentMaxBytes, SubagentSkip: []string{}},
		Synonym: DefaultSynonyms(),
		// 대표말 표는 비어서 시작한다. 비면 정규화가 항등이고 아무것도 안
		// 거절한다 - vocab 의 「배우는 중」과 같은 정신이다 (결정 25 · P12).
		Canon: map[string]string{},
		Scope: map[string]string{},
	}
}

// DefaultStopwords 는 질의에서 AND 로 강제하지 않을 낱말이다. 30개를 넘기지
// 않는다 — 많이 빼면 진짜 낱말까지 잃는다 (설계 4-4).
func DefaultStopwords() []string {
	return []string{"왜", "무엇", "뭐", "어떻게", "언제", "어디", "누가", "어느",
		"지금", "있는", "없는", "하는", "되는", "그것", "이것", "저것", "관련", "대한",
		"위한", "해서", "하고", "인가", "인지", "일까", "및", "등", "때", "수"}
}

// DefaultSynonyms 는 mem init 이 깔아 주는 씨앗 여섯 줄이다 (설계 6-7).
// 빈 표로 시작하면 랭킹의 한 축이 처음부터 없다 — 실데이터 시험에서 `유니티 버전`
// 이 0건이 난 까닭이다. 늘리는 것은 사람 몫이다.
func DefaultSynonyms() map[string][]string {
	return map[string][]string{
		"색인":  {"index", "인덱스", "인덱싱"},
		"기억":  {"memory", "메모리"},
		"훅":   {"hook"},
		"검색":  {"search"},
		"결정":  {"decision"},
		"저장소": {"repo", "repository"},
	}
}

// MaxSynonyms 는 낱말 하나가 데려올 수 있는 동의어 수다 (설계 6-7).
const MaxSynonyms = 3

// mergeSynonyms 는 기본 여섯 줄 위에 mem.toml 의 [synonym] 을 얹는다.
// 예전에는 파일 것으로 통째로 덮어써서 [synonym] 절이 없는 mem.toml 은
// 동의어가 0개가 됐다 — [stopword] 는 기본값을 지키는데 여기만 규칙이
// 달랐다 (리뷰B #8). 사람이 기본 한 줄을 지우고 싶으면 빈 값을 적는다 :
// `"훅" = []`.
func mergeSynonyms(base, file map[string][]string) map[string][]string {
	out := map[string][]string{}
	for word, list := range base {
		out[word] = append([]string{}, list...)
	}
	for word, list := range file {
		if len(list) == 0 {
			delete(out, word)
			continue
		}
		out[word] = append([]string{}, list...)
	}
	return out
}

// SynonymBoth 는 동의어를 양방향으로 편 표다. 설계 6-7 은 "양방향" 인데
// 표는 한쪽만 적혀 있는 것이 보통이다 — `index = ["색인"]` 만 있으면
// `색인` 으로 물었을 때 동의어 랭킹이 통째로 죽었다 (리뷰B #7).
// 자기 참조·중복은 버리고 낱말당 MaxSynonyms 개를 넘기지 않는다.
func SynonymBoth(table map[string][]string) map[string][]string {
	if len(table) == 0 {
		return table
	}
	out := map[string][]string{}
	// 낱말 차례를 정해 놓고 돌린다. map 차례로 돌리면 같은 표에서 같은
	// 질의가 다른 동의어를 골라 답이 흔들린다.
	words := sortedListKeys(table)
	for _, word := range words {
		for _, other := range table[word] {
			out[word] = addSynonym(out[word], word, other)
		}
	}
	for _, word := range words {
		for _, other := range table[word] {
			out[other] = addSynonym(out[other], other, word)
		}
	}
	return out
}

// addSynonym 은 한 자리에 한 낱말을 더한다. 상한을 넘거나 이미 있으면 그대로다.
func addSynonym(list []string, word, other string) []string {
	if other == "" || other == word || len(list) >= MaxSynonyms {
		return list
	}
	for _, item := range list {
		if item == other {
			return list
		}
	}
	return append(list, other)
}

// DefaultPinMax is how many pinned memories one repository should keep.
const DefaultPinMax = 10

// DefaultSecretPatterns are the shapes that stop a memory from being stored.
// The narrow rules come first so a finding is named by the tightest one that fits.
func DefaultSecretPatterns() []string {
	return []string{
		`sk-ant-[A-Za-z0-9_-]{20,}`,
		`sk-(proj|live|test)-[A-Za-z0-9_-]{20,}`,
		`sk-[A-Za-z0-9]{20,}`,
		`ghp_[A-Za-z0-9]{20,}`,
		`github_pat_[A-Za-z0-9_]{20,}`,
		`glpat-[A-Za-z0-9_-]{20,}`,
		`xox[baprs]-[A-Za-z0-9-]{10,}`,
		`AKIA[0-9A-Z]{16}`,
		`-----BEGIN [A-Z ]*PRIVATE KEY-----`,
		`eyJ[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}\.[A-Za-z0-9_-]{10,}`,
		`\b\d{6}-[1-4]\d{6}\b`,
		`\b(?:4\d{3}|5[1-5]\d{2}|3[47]\d{2}|6011)[ -]?\d{4}[ -]?\d{4}[ -]?\d{2,4}\b`,
		PasswordValue,
		LongBlobPadded,
		LongBlobPlain,
	}
}

// PasswordValue blocks "token = <a key>" but not "token = 1200" or
// "secret: patterns": the word needs its own boundaries, and the value has to
// look like a secret. The scanner's guard decides that part (review #6).
const PasswordValue = `(?i)\b(password|passwd|pw|token|secret|api[_-]?key)\b\s*[:=]\s*\S{4,}`

// PasswordWord is the same shape without the guard. It only warns, so a plain
// sentence about a token is still mentioned by lint and stored all the same.
const PasswordWord = `(?i)\b(password|passwd|pw|token|secret|api[_-]?key)\b\s*[:=]\s*\S+`

// The two shapes that replaced the first long-blob rule. The old one counted
// `/` as a base64 letter, so every file path longer than 32 letters was read as
// a key and mem add refused it (field test B1).
// LongBlobPadded still allows `/` but demands the `=` padding a path never has;
// LongBlobPlain drops `/` and `.`, so a path or a dotted identifier breaks the
// run apart, and raises the length to 40.
const (
	LongBlobPadded = `[A-Za-z0-9+/]{40,}={1,2}`
	LongBlobPlain  = `[A-Za-z0-9+]{40,}={0,2}`
)

// ReplacedSecretPatterns maps a retired default pattern onto what took its
// place, so lint does not tell a repository that a rule it already has under
// its old spelling is missing.
func ReplacedSecretPatterns() map[string][]string {
	return map[string][]string{
		`[A-Za-z0-9+/]{32,}={0,2}`: {LongBlobPadded, LongBlobPlain},
		`(?i)(password|passwd|pw|token|secret|api[_-]?key)\s*[:=]\s*\S{4,}`: {PasswordValue},
	}
}

// DefaultWarnPatterns are shapes lint mentions but nothing blocks: a teammate
// address or an issue link is an ordinary memory (design 5, question 2).
func DefaultWarnPatterns() []string {
	return []string{
		`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`,
		`\b01[016-9]-?\d{3,4}-?\d{4}\b`,
		PasswordWord,
	}
}

// Parse reads mem.toml text, filling anything missing from the defaults.
func Parse(text string) (Config, error) {
	config := Default("")
	file, err := parseTOML(text)
	if err != nil {
		return config, err
	}
	config.Schema = file.intOr("", "schema", config.Schema)
	config.Name = file.stringOr("", "name", config.Name)
	config.Tokenizer = file.stringOr("", "tokenizer", config.Tokenizer)
	config.Repo.Scopes = file.listOr("repo", "scopes", config.Repo.Scopes)
	config.Repo.PinMax = file.intOr("repo", "pin_max", config.Repo.PinMax)
	config.Secret.Patterns = file.listOr("secret", "patterns", config.Secret.Patterns)
	config.Secret.WarnPatterns = file.listOr("secret", "warn_patterns", config.Secret.WarnPatterns)
	config.GC.WarmCount = file.intOr("gc", "warm_count", config.GC.WarmCount)
	config.GC.WarmDays = file.intOr("gc", "warm_days", config.GC.WarmDays)
	config.GC.ColdCount = file.intOr("gc", "cold_count", config.GC.ColdCount)
	config.GC.ColdDays = file.intOr("gc", "cold_days", config.GC.ColdDays)
	config.GC.HitDays = file.intOr("gc", "hit_days", config.GC.HitDays)
	config.GC.ArchiveDays = file.intOr("gc", "archive_days", config.GC.ArchiveDays)
	config.GC.ArchiveMB = file.intOr("gc", "archive_mb", config.GC.ArchiveMB)
	config.GC.BatchLimit = file.intOr("gc", "batch_limit", config.GC.BatchLimit)
	config.GC.BatchPercent = file.intOr("gc", "batch_percent", config.GC.BatchPercent)
	config.Budget.Startup = file.intOr("budget", "startup", config.Budget.Startup)
	config.Budget.Clear = file.intOr("budget", "clear", config.Budget.Clear)
	config.Budget.Resume = file.intOr("budget", "resume", config.Budget.Resume)
	config.Budget.Compact = file.intOr("budget", "compact", config.Budget.Compact)
	config.Budget.Fork = file.intOr("budget", "fork", config.Budget.Fork)
	config.Budget.Subagent = file.intOr("budget", "subagent", config.Budget.Subagent)
	config.GC.FoldAfterDays = file.intOr("gc", "fold_after_days", config.GC.FoldAfterDays)
	config.GC.DBMaxMB = file.intOr("gc", "db_max_mb", config.GC.DBMaxMB)
	// `[pin] max` 는 v0.1 이름, `[repo] pin_max` 는 v0.2 이름이다. 둘 다 있으면
	// 옛 이름이 이긴다 — 사람이 손으로 고쳐 둔 쪽이 그쪽이기 때문이다.
	config.Pin.Max = file.intOr("pin", "max", config.Repo.PinMax)
	config.Repo.PinMax = config.Pin.Max
	config.Score.Importance = file.floatOr("score", "importance", config.Score.Importance)
	config.Search.RRFK = file.floatOr("search", "rrf_k", config.Search.RRFK)
	config.Search.BonusCap = file.floatOr("search", "bonus_cap", config.Search.BonusCap)
	config.Search.FieldWeights = weightsOr(file.listOr("search", "field_weights", nil), config.Search.FieldWeights)
	config.Search.K1 = file.floatOr("search", "k1", config.Search.K1)
	config.Search.B = file.floatOr("search", "b", config.Search.B)
	config.Search.RerankTop = file.intOr("search", "rerank_top", config.Search.RerankTop)
	config.Search.AbstainFloor = file.floatOr("search", "abstain_floor", config.Search.AbstainFloor)
	config.Search.SynWeight = file.floatOr("search", "syn_weight", config.Search.SynWeight)
	config.Embed.Enabled = file.boolOr("embed", "enabled", config.Embed.Enabled)
	config.Embed.Path = file.stringOr("embed", "path", config.Embed.Path)
	config.Embed.Model = file.stringOr("embed", "model", config.Embed.Model)
	config.Embed.Floor = file.floatOr("embed", "floor", config.Embed.Floor)
	config.Embed.RRFWeight = file.floatOr("embed", "rrf_weight", config.Embed.RRFWeight)
	config.Quality.DupReject = file.floatOr("quality", "dup_reject", config.Quality.DupReject)
	config.Quality.DupWarn = file.floatOr("quality", "dup_warn", config.Quality.DupWarn)
	config.Quality.DupVectors = file.boolOr("quality", "dup_vectors", config.Quality.DupVectors)
	config.Quality.SimhashHamming = file.intOr("quality", "simhash_hamming", config.Quality.SimhashHamming)
	config.Quality.BodyMinLines = file.intOr("quality", "body_min_lines", config.Quality.BodyMinLines)
	config.Quality.BodyWarn = file.intOr("quality", "body_warn", config.Quality.BodyWarn)
	config.Quality.BodyMax = file.intOr("quality", "body_max", config.Quality.BodyMax)
	config.Quality.TagMin = file.intOr("quality", "tag_min", config.Quality.TagMin)
	config.Quality.TagMax = file.intOr("quality", "tag_max", config.Quality.TagMax)
	config.Quality.TagBroadRatio = file.floatOr("quality", "tag_broad_ratio", config.Quality.TagBroadRatio)
	config.Quality.ScopeSkewRatio = file.floatOr("quality", "scope_skew_ratio", config.Quality.ScopeSkewRatio)
	config.Quality.ColdDays = file.intOr("quality", "cold_days", config.Quality.ColdDays)
	config.Quality.PrecisionDemote = file.floatOr("quality", "precision_demote", config.Quality.PrecisionDemote)
	config.Quality.PrecisionOff = file.floatOr("quality", "precision_off", config.Quality.PrecisionOff)
	config.Stopword.Words = file.listOr("stopword", "words", config.Stopword.Words)
	config.Hook.UserPrompt = file.boolOr("hook", "user_prompt", config.Hook.UserPrompt)
	config.Hook.MaxBytes = file.intOr("hook", "max_bytes", config.Hook.MaxBytes)
	config.Hook.LinesPerSection = file.intOr("hook", "lines_per_section", config.Hook.LinesPerSection)
	config.Hook.SubagentStart = file.boolOr("hook", "subagent_start", config.Hook.SubagentStart)
	config.Hook.SubagentMaxBytes = file.intOr("hook", "subagent_max_bytes", config.Hook.SubagentMaxBytes)
	config.Hook.SubagentSkip = file.listOr("hook", "subagent_skip", config.Hook.SubagentSkip)
	config.Synonym = mergeSynonyms(config.Synonym, file.mapOfLists("synonym"))
	config.Canon = file.mapOf("canon")
	config.Scope = file.mapOf("scope")
	// 잘못 쓴 대표말 표는 여기서 막는다. 그냥 넘기면 색인과 질의가 다른
	// 글자가 돼 아무 말 없이 틀린 답이 나온다.
	if _, err := config.CanonTable(); err != nil {
		return config, err
	}
	return config, nil
}

// Load reads one mem.toml file from disk.
func Load(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Default(""), err
	}
	return Parse(strings.TrimPrefix(string(data), "\ufeff"))
}

// Encode writes mem.toml with LF endings and no BOM.
func Encode(config Config) []byte {
	out := strings.Builder{}
	out.WriteString("schema    = " + strconv.Itoa(config.Schema) + "\n")
	out.WriteString("name      = " + quote(config.Name) + "\n")
	out.WriteString("tokenizer = " + quote(config.Tokenizer) + "\n\n")
	// 표준 목록의 주인은 vocab.toml 이다. 여기는 이 저장소 몫만 적는다.
	out.WriteString("[repo]\n")
	writeList(&out, "scopes", config.Repo.Scopes)
	writeInt(&out, "pin_max", config.Repo.PinMax)
	out.WriteString("\n")
	// 사람이 이 파일을 열었을 때 빠져나갈 길이 보여야 한다 (리뷰 C #28).
	out.WriteString("# 여기 든 패턴에 걸리면 mem add·set·index 가 그 기억을 안 받는다.\n")
	out.WriteString("# 오탐이면 이 목록에서 그 줄을 고친다. long-blob 은 대·소문자·숫자가\n")
	out.WriteString("# 다 섞인 덩어리만 잡으므로 CamelCase 이름은 안 걸린다.\n")
	out.WriteString("[secret]\n")
	writeList(&out, "patterns", config.Secret.Patterns)
	writeList(&out, "warn_patterns", config.Secret.WarnPatterns)
	out.WriteString("\n")
	out.WriteString("[gc]\n")
	writeInt(&out, "warm_count", config.GC.WarmCount)
	writeInt(&out, "warm_days", config.GC.WarmDays)
	writeInt(&out, "cold_count", config.GC.ColdCount)
	writeInt(&out, "cold_days", config.GC.ColdDays)
	writeInt(&out, "hit_days", config.GC.HitDays)
	writeInt(&out, "archive_days", config.GC.ArchiveDays)
	writeInt(&out, "archive_mb", config.GC.ArchiveMB)
	writeInt(&out, "batch_limit", config.GC.BatchLimit)
	writeInt(&out, "batch_percent", config.GC.BatchPercent)
	writeInt(&out, "fold_after_days", config.GC.FoldAfterDays)
	writeInt(&out, "db_max_mb", config.GC.DBMaxMB)
	// 설계 6-2 는 훅 예산을 `[hook] budget_*` 로 적었지만, 6-7 이 「예산 5종 유지」
	// 라서 v0.1 의 `[budget]` 다섯 줄을 그대로 둔다. 이름만 옮기면 clear·fork 가
	// 갈 곳이 없다.
	out.WriteString("\n[budget]\n")
	writeInt(&out, "startup", config.Budget.Startup)
	writeInt(&out, "clear", config.Budget.Clear)
	writeInt(&out, "resume", config.Budget.Resume)
	writeInt(&out, "compact", config.Budget.Compact)
	writeInt(&out, "fork", config.Budget.Fork)
	writeInt(&out, "subagent", config.Budget.Subagent)
	out.WriteString("\n[pin]\n")
	writeInt(&out, "max", config.Pin.Max)
	out.WriteString("\n[stopword]\n")
	writeList(&out, "words", config.Stopword.Words)
	out.WriteString("\n[hook]\n")
	writeBool(&out, "user_prompt", config.Hook.UserPrompt)
	writeInt(&out, "max_bytes", config.Hook.MaxBytes)
	writeInt(&out, "lines_per_section", config.Hook.LinesPerSection)
	out.WriteString("# subagent_* 는 서브에이전트가 시작할 때 넣는 블록의 손잡이다.\n" +
		"# subagent_skip 에 agent_type 을 적으면 그 종류에는 아무것도 안 넣는다.\n")
	writeBool(&out, "subagent_start", config.Hook.SubagentStart)
	writeInt(&out, "subagent_max_bytes", config.Hook.SubagentMaxBytes)
	writeList(&out, "subagent_skip", config.Hook.SubagentSkip)
	// [search]·[embed]·[quality] 는 늘 쓴다. 손잡이가 파일에 안 보이면 아무도
	// 못 돌린다 — v0.1 이 [synonym] 을 감췄다가 동의어가 0쌍이 된 것과 같은 자리다.
	out.WriteString("\n[search]\n")
	writeFloat(&out, "rrf_k", config.Search.RRFK)
	writeFloat(&out, "bonus_cap", config.Search.BonusCap)
	writeFloats(&out, "field_weights", config.Search.FieldWeights[:])
	writeFloat(&out, "k1", config.Search.K1)
	writeFloat(&out, "b", config.Search.B)
	writeInt(&out, "rerank_top", config.Search.RerankTop)
	writeFloat(&out, "abstain_floor", config.Search.AbstainFloor)
	writeFloat(&out, "syn_weight", config.Search.SynWeight)
	out.WriteString("\n# 임베딩은 조건부다. 모델 파일이 없으면 조용히 낱말 검색만 한다.\n" +
		"# path 는 낱말 임베딩 표(ko.bin) 자리다. 비면 Memory/model/ko.bin 을 본다.\n" +
		"# 의미 검색 모델은 이 칸이 아니라 mem install 이 ~/.aimemory/models/ 에 깐다.\n[embed]\n")
	writeBool(&out, "enabled", config.Embed.Enabled)
	out.WriteString("path = " + quote(config.Embed.Path) + "\n")
	out.WriteString("# model 이 비면 exe 가 아는 기본 모델을 쓴다.\n")
	out.WriteString("model = " + quote(config.Embed.Model) + "\n")
	writeFloat(&out, "floor", config.Embed.Floor)
	writeFloat(&out, "rrf_weight", config.Embed.RRFWeight)
	out.WriteString("\n# 문서 품질 문턱. mem eval --quality 가 규칙을 강등하면 여기 적는다.\n[quality]\n")
	writeFloat(&out, "dup_reject", config.Quality.DupReject)
	writeFloat(&out, "dup_warn", config.Quality.DupWarn)
	out.WriteString("# dup_vectors 는 중복 판정에 임베딩을 쓸지다. 기본 꺼짐 (설계 18-7).\n")
	writeBool(&out, "dup_vectors", config.Quality.DupVectors)
	writeInt(&out, "simhash_hamming", config.Quality.SimhashHamming)
	writeInt(&out, "body_min_lines", config.Quality.BodyMinLines)
	writeInt(&out, "body_warn", config.Quality.BodyWarn)
	writeInt(&out, "body_max", config.Quality.BodyMax)
	writeInt(&out, "tag_min", config.Quality.TagMin)
	writeInt(&out, "tag_max", config.Quality.TagMax)
	writeFloat(&out, "tag_broad_ratio", config.Quality.TagBroadRatio)
	writeFloat(&out, "scope_skew_ratio", config.Quality.ScopeSkewRatio)
	writeInt(&out, "cold_days", config.Quality.ColdDays)
	writeFloat(&out, "precision_demote", config.Quality.PrecisionDemote)
	writeFloat(&out, "precision_off", config.Quality.PrecisionOff)
	if config.Score.Importance != DefaultImportanceWeight {
		out.WriteString("\n[score]\n")
		writeFloat(&out, "importance", config.Score.Importance)
	}
	if len(config.Synonym) > 0 {
		out.WriteString("\n[synonym]\n")
		for _, word := range sortedListKeys(config.Synonym) {
			writeList(&out, quote(word), config.Synonym[word])
		}
	}
	// 「같은 것을 다르게 쓴 것」은 여기, 「1:다·외래어」는 [synonym] 이다.
	// 빈 절이라도 적어 둔다 - 손잡이가 파일에 안 보이면 아무도 못 채운다.
	out.WriteString("\n# 낱말 -> 대표말. 색인과 질의가 이 표를 똑같이 타서 같은 글자가 된다.\n")
	out.WriteString("# 왼쪽을 오른쪽으로 바꾼다. 여러 낱말 구·영어도 된다. 예 :\n")
	out.WriteString("# \"인덱싱\" = \"색인\"\n")
	out.WriteString("# \"index\" = \"색인\"\n")
	out.WriteString("[canon]\n")
	for _, word := range sortedKeys(config.Canon) {
		out.WriteString(quote(word) + " = " + quote(config.Canon[word]) + "\n")
	}
	out.WriteString("\n# 폴더 이름 -> scope 값. 비어 있어도 된다 (없으면 폴더 이름 소문자를 쓴다).\n")
	out.WriteString("[scope]\n")
	for _, folder := range sortedKeys(config.Scope) {
		out.WriteString(quote(folder) + " = " + quote(config.Scope[folder]) + "\n")
	}
	return []byte(out.String())
}

// sortedKeys keeps Encode's output stable so init writes the same bytes twice.
func sortedKeys(table map[string]string) []string {
	keys := make([]string, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// sortedListKeys keeps [synonym] in a stable order, for the same reason.
func sortedListKeys(table map[string][]string) []string {
	keys := make([]string, 0, len(table))
	for key := range table {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}

// weightsOr 는 `field_weights = [16, 6, 6, 1]` 를 읽는다. 넷도 아니고 셋도
// 아니거나 숫자가 아니면 기본값을 지킨다 — 반쯤 읽은 가중으로 랭킹을 돌리면
// 왜 틀렸는지 못 찾는다.
//
// **세 값이면 v0.3 까지의 mem.toml 이다.** 그때 순서는 제목+태그+scope / 요약 /
// 본문이라 제목·요약·본문 자리에 그대로 옮기고 새로 생긴 메타 자리에만
// 기본값을 채운다. 조용히 넘기지 않고 경고 한 줄을 남긴다 (불변조건 7).
func weightsOr(list []string, fallback [4]float64) [4]float64 {
	// 아예 안 적은 것은 잘못이 아니다 — 그때는 기본값이 맞다.
	if len(list) == 0 {
		return fallback
	}
	if len(list) != 3 && len(list) != 4 {
		Warn(i18n.T(i18n.BadFieldWeights, len(list)))
		return fallback
	}
	read := make([]float64, len(list))
	for i, item := range list {
		value, err := strconv.ParseFloat(strings.TrimSpace(item), 64)
		if err != nil || value < 0 {
			// 개수가 아니라 **몇 번째 값**을 짚는다. 개수 이야기를 하면 개수는
			// 맞는데 값 하나가 틀린 자리를 사람이 못 찾는다 (v0.4 리뷰 A R6).
			Warn(i18n.T(i18n.BadWeightValue, i+1, strings.TrimSpace(item)))
			return fallback
		}
		read[i] = value
	}
	if len(read) == 4 {
		return [4]float64{read[0], read[1], read[2], read[3]}
	}
	Warn(i18n.T(i18n.OldFieldWeights, DefaultFieldWeights[MetaWeightSlot]))
	return [4]float64{read[0], DefaultFieldWeights[MetaWeightSlot], read[1], read[2]}
}

// Problems 는 설정 값이 말이 안 되는 자리를 한 번에 다 돌려준다. 값을 조용히
// 고치지 않는다 — 사람이 적은 값을 도구가 몰래 바꾸면 왜 그렇게 도는지 못 찾는다.
func (c Config) Problems() []string {
	out := []string{}
	if c.Quality.DupWarn > c.Quality.DupReject {
		out = append(out, "[quality] dup_warn 이 dup_reject 보다 크다. 경고선이 거절선보다 높으면 경고가 안 뜬다.")
	}
	if c.Quality.TagMin > c.Quality.TagMax {
		out = append(out, "[quality] tag_min 이 tag_max 보다 크다.")
	}
	if c.Quality.BodyWarn > c.Quality.BodyMax {
		out = append(out, "[quality] body_warn 이 body_max 보다 크다.")
	}
	// add 관문과 승격이 같은 자를 써야 한다. 여기가 넘으면 add 가 통과시킨
	// 기억을 index 가 inbox/bad 로 버린다 (스트레스시험 D3).
	if c.Quality.BodyMax > model.BodyMaxLines {
		out = append(out, fmt.Sprintf("[quality] body_max 가 규격 상한 %d줄보다 크다. "+
			"add 가 받은 것을 index 가 버린다.", model.BodyMaxLines))
	}
	if c.Quality.PrecisionOff > c.Quality.PrecisionDemote {
		out = append(out, "[quality] precision_off 가 precision_demote 보다 크다.")
	}
	for _, pair := range []struct {
		name  string
		value float64
	}{{"[quality] dup_reject", c.Quality.DupReject}, {"[quality] dup_warn", c.Quality.DupWarn},
		{"[search] abstain_floor", c.Search.AbstainFloor}, {"[embed] floor", c.Embed.Floor}} {
		if pair.value < 0 || pair.value > 1 {
			out = append(out, pair.name+" 은 0~1 이어야 한다.")
		}
	}
	if c.Hook.MaxBytes < 500 {
		out = append(out, "[hook] max_bytes 가 너무 작다. 500 바이트 아래면 주입할 것이 없다.")
	}
	if c.Hook.SubagentStart && c.Hook.SubagentMaxBytes < 500 {
		out = append(out, "[hook] subagent_max_bytes 가 너무 작다. 500 바이트 아래면 주입할 것이 없다.")
	}
	if c.Search.RerankTop < 1 {
		out = append(out, "[search] rerank_top 은 1 이상이어야 한다.")
	}
	if c.Search.K1 <= 0 || c.Search.B < 0 || c.Search.B > 1 {
		out = append(out, "[search] k1 은 0보다 커야 하고 b 는 0~1 이어야 한다.")
	}
	return out
}

func writeFloats(out *strings.Builder, key string, values []float64) {
	parts := make([]string, 0, len(values))
	for _, value := range values {
		parts = append(parts, strconv.FormatFloat(value, 'f', -1, 64))
	}
	out.WriteString(key + " = [" + strings.Join(parts, ", ") + "]\n")
}

func writeFloat(out *strings.Builder, key string, value float64) {
	out.WriteString(key + " = " + strconv.FormatFloat(value, 'f', -1, 64) + "\n")
}

func writeList(out *strings.Builder, key string, values []string) {
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, quote(value))
	}
	out.WriteString(key + " = [" + strings.Join(quoted, ", ") + "]\n")
}

func writeBool(out *strings.Builder, key string, value bool) {
	out.WriteString(key + " = " + strconv.FormatBool(value) + "\n")
}

func writeInt(out *strings.Builder, key string, value int) {
	out.WriteString(key + " = " + strconv.Itoa(value) + "\n")
}

func quote(value string) string {
	return strconv.Quote(value)
}
