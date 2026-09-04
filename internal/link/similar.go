package link

import "strings"

// Similarity 는 두 기억이 얼마나 같은 주제인지다 (0~1). MMR 의 「이미 고른
// 것과의 닮음」과 묶음 반환이 같이 쓴다.
//
// 본문이나 벡터를 안 쓴다 — 검색 뒤 50건에 도는 자리라 파일을 열면 예산
// 30ms 를 넘긴다. 태그·scope·제목 낱말만으로도 「같은 주제 두 건」은 갈린다.
func Similarity(a, b Doc) float64 {
	tags := jaccard(setOf(a.Tags), setOf(b.Tags))
	words := jaccard(titleWords(a), titleWords(b))
	same := 0.0
	if a.Scope != "" && a.Scope == b.Scope {
		same = 1
	}
	// 태그가 주역이다. scope 는 같은 것이 워낙 많아 몫을 작게 준다.
	return 0.5*tags + 0.3*words + 0.2*same
}

// SameTopic 은 묶음 반환이 한 묶음으로 볼 자다. 태그 2개 겹침 = 자동 링크
// 후보와 같은 선이라 두 장치가 같은 것을 「같은 주제」로 부른다.
func SameTopic(a, b Doc) bool {
	return overlap(setOf(a.Tags), setOf(b.Tags)) >= 2 || Similarity(a, b) >= topicFloor
}

// topicFloor 는 태그가 덜 겹쳐도 같은 묶음으로 볼 닮음이다. 실기억 206건에서
// 0.5 는 묶음이 거의 안 생기고 0.3 은 절반이 한 묶음이 됐다.
const topicFloor = 0.4

// titleWords 는 제목의 낱말이다. scope 는 안 섞는다 — 섞으면 제목이 없는 기억
// 끼리 「제목이 똑같다」로 읽혀 같은 scope 전체가 서로 닮은 것이 된다.
func titleWords(doc Doc) map[string]bool {
	return setOf(strings.Fields(doc.Title))
}

func jaccard(a, b map[string]bool) float64 {
	if len(a) == 0 || len(b) == 0 {
		return 0
	}
	shared := overlap(a, b)
	whole := len(a) + len(b) - shared
	if whole == 0 {
		return 0
	}
	return float64(shared) / float64(whole)
}
