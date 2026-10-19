package config

import (
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// 생성 서버(Strata) 칸의 기본값과 상한이다 (A2 retain 설계 8절). 판정용 timeout_ms(60초 상한)와
// 따로 둔다 — 한국어 36,000자 토막의 prefill 과 출력 1,500 토큰을 기다려야 한다.
const (
	generateTimeoutMS    = 180000
	generateTimeoutMaxMS = 600000
)

// unusedKeys 는 읽지 않는 옛 칸이다. 남아 있으면 「안 쓰는 칸」 경고 한 줄을 낸다 — 남기면 아직
// 그 값으로 판정한다고 오해한다.
var unusedKeys = []string{"judge_profile", "generate_profile", "timeout_ms"}

// parseGenerate 는 생성 칸 셋을 읽는다. 틀린 칸은 버리고(주소는 꺼짐 · 시간은 기본값) 문제로 알린다.
// 옛 칸(unusedKeys)은 읽지 않고 경고 한 줄만 낸다.
func parseGenerate(file *tomlFile, settings *LLMConfig) []string {
	problems := []string{}
	found := []string{}
	for _, key := range unusedKeys {
		if _, ok := file.value("", key); ok {
			found = append(found, key)
		}
	}
	if len(found) > 0 {
		problems = append(problems, i18n.T(i18n.LLMProfileRemoved, strings.Join(found, " · ")))
	}
	address, problem := generateAddress(file)
	if problem != "" {
		problems = append(problems, problem)
	}
	settings.GenerateURL = address
	settings.GenerateModel = strings.TrimSpace(file.stringOr("", "generate_model", ""))
	timeout := file.intOr("", "generate_timeout_ms", generateTimeoutMS)
	if timeout <= 0 || timeout > generateTimeoutMaxMS {
		problems = append(problems, i18n.T(i18n.LLMBadGenerateTimeout, timeout, generateTimeoutMaxMS, generateTimeoutMS))
		timeout = generateTimeoutMS
	}
	settings.GenerateTimeoutMS = timeout
	return problems
}

// generateAddress 는 생성 주소다. generate_url 이 아예 없을 때만 옛 url 을 대신 읽는다 — 이 기계
// 설정을 안 깨려는 한 판짜리 별칭이고 다음 판에 지운다. 틀린 주소는 꺼짐과 문제 한 줄이다.
func generateAddress(file *tomlFile) (string, string) {
	address := strings.TrimSpace(file.stringOr("", "generate_url", ""))
	if address != "" {
		if !httpAddress(address) {
			return "", i18n.T(i18n.LLMBadGenerateURL)
		}
		return address, ""
	}
	address = strings.TrimSpace(file.stringOr("", "url", ""))
	if address != "" && !httpAddress(address) {
		return "", i18n.T(i18n.LLMBadURL)
	}
	return address, ""
}
