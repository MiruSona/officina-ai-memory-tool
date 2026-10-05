package config

import (
	"errors"
	"strings"

	"github.com/mirusona/officina-ai-memory-tool/internal/i18n"
)

// init 이 줄을 끼운 뒤 거는 대조 장치다. 끼우기 규칙이 파서와 조금만 어긋나도
// 사람이 고친 값이 말없이 기본값으로 덮인다 (코드리뷰 10-05 높음 1). 그래서 끼운
// 글을 다시 읽어 원래 파일에 적혀 있던 키의 값이 그대로인지 본다.

// CheckConfigKept 는 mem.toml 의 before 를 after 로 바꿔도 before 에 적혀 있던
// 키의 읽힌 값이 하나도 안 바뀌는지 본다. 바뀌면 그 키 이름을 담은 오류다.
func CheckConfigKept(before, after string) error {
	was, err := Parse(before)
	if err != nil {
		return err
	}
	now, err := Parse(after)
	if err != nil {
		return err
	}
	return keptOrError(FileName, before, string(Encode(was)), string(Encode(now)))
}

// CheckVocabKept 는 vocab.toml 에 같은 대조를 건다.
func CheckVocabKept(before, after string) error {
	was, err := ParseVocab(before)
	if err != nil {
		return err
	}
	now, err := ParseVocab(after)
	if err != nil {
		return err
	}
	return keptOrError(VocabFileName, before, string(EncodeVocab(was)), string(EncodeVocab(now)))
}

// keptOrError 는 읽힌 값을 다시 써 본 두 글(wasOut·nowOut)을 키마다 견준다.
// 값을 다시 써서 보므로 옛 이름 옮기기처럼 Parse 안에서 일어나는 일도 잡힌다.
// before 에 없던 키는 안 본다 — 빠졌던 키가 새로 생기는 것은 끼우기의 목적이다.
func keptOrError(file, before, wasOut, nowOut string) error {
	was, now := keyLines(wasOut), keyLines(nowOut)
	changed := []string{}
	for _, key := range sortedStrings(tomlKeySet(before)) {
		if was[key].text != now[key].text {
			changed = append(changed, key)
		}
	}
	if len(changed) == 0 {
		return nil
	}
	return errors.New(i18n.T(i18n.InitFillChanged, file, strings.Join(changed, ", ")))
}
