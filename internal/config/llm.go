package config

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// llm.toml 은 이 기계의 바깥 LLM 설정이다 (자동쌓기설계 2-5 · U5). 저장소마다가
// 아니라 기계마다 하나라 `~/.aimemory/` 에 두고 git 에 안 들어간다. 파일이 없거나
// url 이 비면 LLM 을 쓰는 기능만 건너뛴다 — 오류로 멈추지 않는다.
const (
	LLMFileName = "llm.toml"
	// LLMPathEnv 는 llm.toml 자리를 바꾸는 환경 변수다. 시험·측정이 이 기계의
	// 진짜 설정을 안 건드리게 쓴다. 없는 파일을 가리키면 꺼진 것과 같다.
	LLMPathEnv       = "MEM_LLM_CONFIG"
	llmTimeoutMS     = 5000
	llmTimeoutMaxMS  = 60000
	llmMachineFolder = ".aimemory"

	// NLI 판정 단(판정 사다리 ② · 길1 한국어 NLI 분류기 설계 7절). 주소가 비면 그 단을 건너뛴다.
	// 문턱 둘의 기본값은 「dev 로 고른 값이 없을 때 안전한 쪽」이다 — 지지 쪽이 더 엄하다.
	nliTimeoutMS     = 1000
	nliTimeoutMaxMS  = 5000
	nliSure          = 0.70
	nliSureSupport   = 0.90
	nliSureMin       = 0.34
	nliSureMax       = 0.99
	nliRemovedPrefix = "laya_"
)

// LLMConfig 는 llm.toml 한 벌이다. Key 는 화면·기록·log.md 어디에도 찍지 않는다.
type LLMConfig struct {
	Path            string
	URL             string
	Key             string
	JudgeProfile    string
	GenerateProfile string
	TimeoutMS       int
	// NLIURL 이 비면 NLI 단을 건너뛴다. 답이 지지(A)면 NLISureSupport, 반대·무관(B·C)이면
	// NLISure 아래 확률일 때 다음 단으로 넘긴다.
	NLIURL         string
	NLITimeoutMS   int
	NLISure        float64
	NLISureSupport float64
}

// Enabled 는 바깥 LLM 을 쓸 수 있게 적혀 있는지다. 서버가 닿는지는 모른다.
func (c LLMConfig) Enabled() bool { return c.URL != "" }

// LLMPath 는 읽을 llm.toml 자리다. 집 폴더를 못 찾으면 빈 글이다 (= 꺼짐).
func LLMPath() string {
	if given := strings.TrimSpace(os.Getenv(LLMPathEnv)); given != "" {
		return given
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, llmMachineFolder, LLMFileName)
}

// LoadLLM 은 llm.toml 을 읽는다. 파일이 없으면 꺼진 설정과 문제 0건이다. 읽었는데
// 값이 틀리면 그 칸을 버리고 문제로 알린다 — 틀린 url 은 꺼짐으로 친다.
func LoadLLM(path string) (LLMConfig, []string) {
	settings := defaultLLM(path)
	if path == "" {
		return settings, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return settings, nil
	}
	return ParseLLM(path, strings.TrimPrefix(string(data), "\ufeff"))
}

// ParseLLM 은 llm.toml 글을 읽는다. 칸은 맨 위(절 없음)에 둔다.
func ParseLLM(path, text string) (LLMConfig, []string) {
	settings := defaultLLM(path)
	file, err := parseTOML(text)
	if err != nil {
		return settings, []string{err.Error()}
	}
	problems := []string{}
	address := strings.TrimSpace(file.stringOr("", "url", ""))
	if address != "" && !httpAddress(address) {
		problems = append(problems, i18n.T(i18n.LLMBadURL))
		address = ""
	}
	settings.URL = address
	settings.Key = file.stringOr("", "key", "")
	settings.JudgeProfile = strings.TrimSpace(file.stringOr("", "judge_profile", ""))
	settings.GenerateProfile = strings.TrimSpace(file.stringOr("", "generate_profile", ""))
	timeout := file.intOr("", "timeout_ms", llmTimeoutMS)
	if timeout <= 0 || timeout > llmTimeoutMaxMS {
		problems = append(problems, i18n.T(i18n.LLMBadTimeout, timeout, llmTimeoutMaxMS, llmTimeoutMS))
		timeout = llmTimeoutMS
	}
	settings.TimeoutMS = timeout
	problems = append(problems, parseNLI(file, &settings)...)
	return settings, problems
}

// defaultLLM 은 파일이 없거나 칸이 빠졌을 때의 값이다.
func defaultLLM(path string) LLMConfig {
	return LLMConfig{Path: path, TimeoutMS: llmTimeoutMS, NLITimeoutMS: nliTimeoutMS, NLISure: nliSure,
		NLISureSupport: nliSureSupport}
}

// parseNLI 는 NLI 네 칸을 읽는다. 틀린 칸은 버리고(주소는 꺼짐 · 수는 기본값) 문제로 알린다.
// 옛 laya_* 칸은 별칭으로 받지 않는다 — 옛 값은 다른 모델에 맞춘 문턱이라 그대로 쓰면 안 된다.
func parseNLI(file *tomlFile, settings *LLMConfig) []string {
	problems := []string{}
	if file.hasPrefix("", nliRemovedPrefix) {
		problems = append(problems, i18n.T(i18n.LLMLayaRemoved))
	}
	address := strings.TrimSpace(file.stringOr("", "nli_url", ""))
	if address != "" && !httpAddress(address) {
		problems = append(problems, i18n.T(i18n.LLMBadNLIURL))
		address = ""
	}
	settings.NLIURL = address
	timeout := file.intOr("", "nli_timeout_ms", nliTimeoutMS)
	if timeout <= 0 || timeout > nliTimeoutMaxMS {
		problems = append(problems, i18n.T(i18n.LLMBadNLITimeout, timeout, nliTimeoutMaxMS, nliTimeoutMS))
		timeout = nliTimeoutMS
	}
	settings.NLITimeoutMS = timeout
	var problem string
	settings.NLISure, problem = readSure(file, "nli_sure", nliSure)
	if problem != "" {
		problems = append(problems, problem)
	}
	settings.NLISureSupport, problem = readSure(file, "nli_sure_support", nliSureSupport)
	if problem != "" {
		problems = append(problems, problem)
	}
	// 지지 선이 반대·무관 선보다 낮으면 거짓 지지를 막는 뜻이 뒤집힌다. 막지는 않고 알리기만 한다.
	if settings.NLISureSupport < settings.NLISure {
		problems = append(problems, i18n.T(i18n.LLMNLISureInverted, settings.NLISureSupport, settings.NLISure))
	}
	return problems
}

// readSure 는 문턱 한 칸을 읽는다. 범위 밖이면 기본값과 문제 한 줄이다.
func readSure(file *tomlFile, key string, fallback float64) (float64, string) {
	sure := file.floatOr("", key, fallback)
	// 범위 안을 참으로 묻는다 — nan 은 두 비교가 다 거짓이라 범위 밖 검사를 빠져나간다.
	if !(sure >= nliSureMin && sure <= nliSureMax) {
		return fallback, i18n.T(i18n.LLMBadNLISure, key, sure, nliSureMin, nliSureMax, fallback)
	}
	return sure, ""
}

// httpAddress 는 http(s) 주소인지다. 다른 꼴(file: 등)은 받지 않는다.
func httpAddress(text string) bool {
	parsed, err := url.Parse(text)
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}
