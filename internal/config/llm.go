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

	// Laya 판정 단(자체판정프로그램설계 3절). 주소가 비면 그 단을 건너뛴다.
	layaTimeoutMS    = 1000
	layaTimeoutMaxMS = 5000
	layaSure         = 0.70
	layaSureMin      = 0.34
	layaSureMax      = 0.99
)

// LLMConfig 는 llm.toml 한 벌이다. Key 는 화면·기록·log.md 어디에도 찍지 않는다.
type LLMConfig struct {
	Path            string
	URL             string
	Key             string
	JudgeProfile    string
	GenerateProfile string
	TimeoutMS       int
	// LayaURL 이 비면 Laya 단을 건너뛴다. LayaSure 아래 확률이면 다음 단으로 넘긴다.
	LayaURL       string
	LayaTimeoutMS int
	LayaSure      float64
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
	problems = append(problems, parseLaya(file, &settings)...)
	return settings, problems
}

// defaultLLM 은 파일이 없거나 칸이 빠졌을 때의 값이다.
func defaultLLM(path string) LLMConfig {
	return LLMConfig{Path: path, TimeoutMS: llmTimeoutMS, LayaTimeoutMS: layaTimeoutMS, LayaSure: layaSure}
}

// parseLaya 는 Laya 세 칸을 읽는다. 틀린 칸은 버리고(주소는 꺼짐 · 수는 기본값) 문제로 알린다.
func parseLaya(file *tomlFile, settings *LLMConfig) []string {
	problems := []string{}
	address := strings.TrimSpace(file.stringOr("", "laya_url", ""))
	if address != "" && !httpAddress(address) {
		problems = append(problems, i18n.T(i18n.LLMBadLayaURL))
		address = ""
	}
	settings.LayaURL = address
	timeout := file.intOr("", "laya_timeout_ms", layaTimeoutMS)
	if timeout <= 0 || timeout > layaTimeoutMaxMS {
		problems = append(problems, i18n.T(i18n.LLMBadLayaTimeout, timeout, layaTimeoutMaxMS, layaTimeoutMS))
		timeout = layaTimeoutMS
	}
	settings.LayaTimeoutMS = timeout
	sure := file.floatOr("", "laya_sure", layaSure)
	// 범위 안을 참으로 묻는다 — laya_sure = nan 은 두 비교가 다 거짓이라 범위 밖 검사를 빠져나간다.
	if !(sure >= layaSureMin && sure <= layaSureMax) {
		problems = append(problems, i18n.T(i18n.LLMBadLayaSure, sure, layaSureMin, layaSureMax, layaSure))
		sure = layaSure
	}
	settings.LayaSure = sure
	return problems
}

// httpAddress 는 http(s) 주소인지다. 다른 꼴(file: 등)은 받지 않는다.
func httpAddress(text string) bool {
	parsed, err := url.Parse(text)
	if err != nil || parsed.Host == "" {
		return false
	}
	return parsed.Scheme == "http" || parsed.Scheme == "https"
}
