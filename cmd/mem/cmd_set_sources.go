package main

import (
	"errors"

	"github.com/mirusona/officina-ai-memory-tool/internal/config"
	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
	"github.com/mirusona/officina-ai-memory-tool/internal/model"
)

// sourcesOf 는 `set --sources a,b`(별칭 `--source`)를 푼다. 근거는 통째 교체다 —
// review gone 큐가 권하는 다음 명령이 이것이다 (사용 피드백 2026-10-10).
// 안 줬으면 ok 가 false 다. 꼴이 틀리면 add 와 같은 검사 오류(종료 2)를 돌려준다.
func sourcesOf(parsed *options) ([]string, bool, error) {
	if !parsed.has("sources") && !parsed.has("source") {
		return nil, false, nil
	}
	sources := append(parsed.list("sources"), parsed.list("source")...)
	// 빈 목록으로 갈아 끼우면 decision 같은 종류는 승격에서야 bad 로 간다.
	// 큐에 넣기 전에 막아야 AI 가 잘 된 줄 착각하지 않는다.
	if len(sources) == 0 {
		return nil, true, &checkFailError{errors.New(i18n.T(i18n.SetSourcesEmpty))}
	}
	if err := checkList(model.Memory{Spec: model.SpecV2, Sources: sources}); err != nil {
		return nil, true, err
	}
	return sources, true, nil
}

// refuseSecretSources 는 새 근거에 비밀값이 섞였는지 본다. add 의 근거 검사와 같은 자리다.
func refuseSecretSources(repository *config.Repository, set map[string]any) int {
	sources, ok := set["sources"].([]any)
	if !ok {
		return exitOK
	}
	scanner := scannerFor(repository.Config.Secret)
	for _, one := range sources {
		text, _ := one.(string)
		if found := scanner.ScanLine(text); found != nil {
			return refuse(i18n.T(i18n.SecretFoundIn, "sources", found.Rule))
		}
	}
	return exitOK
}
