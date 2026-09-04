package config

import "github.com/mirusona/officina-ai-memory-tool/internal/token"

// CanonTable 은 `[canon]` 을 정규화가 쓸 꼴로 다듬어 준다. 순환·빈 값이면
// 오류다. 표가 비면 오류가 아니라 항등이다 - 아직 안 채운 저장소가 아무 벌도
// 안 받는다 (결정 25 · P12 「배우는 중」).
//
// 색인 쪽(`fts_norm`)과 질의 쪽이 이것 하나를 같이 써야 같은 글자가 된다.
func (c Config) CanonTable() (*token.Canon, error) {
	return token.NewCanon(c.Canon)
}
