package embed

// 꽂는 자리 (결정 15).
//
// 임베딩을 쓰는 자리는 셋뿐이다 — 검색 재정렬 · 중복 후보 셋째 신호 ·
// review 의 STALE 후보. 셋이 **같은 벡터 하나**를 나눠 쓴다.
//
//   - 중복·STALE 은 `vectors.bin` 만 있으면 된다. **모델을 안 싣는다.**
//   - 검색만 질의 한 줄을 인코딩하느라 모델을 싣는다. 그것도 **정말 쓸 때**
//     싣는다 — 그래서 훅 경로에는 세션이 뜨지 않는다.

import "sync"

// LookupIDs 는 docid 를 기억 id 로 바꿔 주는 자리다. 색인 표를 아는 쪽이
// 넘겨준다 — 여기서 DB 를 알면 패키지가 서로 물린다.
type LookupIDs func(docids []int64) map[int64]string

// Reranker 는 `search.Options.Vectors` 에 꽂는 것이다.
type Reranker struct {
	model   string
	vectors *Vectors
	lookup  LookupIDs
	once    sync.Once
	session *Model
}

// NewReranker 는 재정렬 자리를 만든다. 벡터 파일이 없으면 nil 이다 —
// 문서 벡터가 없으면 질의만 인코딩해 봐야 견줄 것이 없다.
func NewReranker(model string, vectors *Vectors, lookup LookupIDs) *Reranker {
	if vectors == nil || vectors.Len() == 0 || lookup == nil {
		return nil
	}
	return &Reranker{model: model, vectors: vectors, lookup: lookup}
}

// Close 는 열었으면 닫는다. 안 열었으면 아무 일도 안 한다.
func (r *Reranker) Close() {
	if r == nil || r.session == nil {
		return
	}
	r.session.Close()
	r.session = nil
}

// Query 는 질의 한 줄의 단위 벡터다. **여기서 처음으로 모델을 싣는다.**
// 못 실으면 nil 이고, 그러면 검색은 낱말 모드 그대로 답한다.
func (r *Reranker) Query(text string) []float32 {
	if r == nil {
		return nil
	}
	r.once.Do(func() { r.session = OpenModel(r.model) })
	if r.session == nil {
		return nil
	}
	return r.session.Query(text)
}

// Docs 는 후보 문서의 단위 벡터다. 벡터가 없는 문서는 답에서 빠진다.
func (r *Reranker) Docs(docids []int64) map[int64][]float32 {
	if r == nil || len(docids) == 0 {
		return nil
	}
	names := r.lookup(docids)
	out := make(map[int64][]float32, len(names))
	for docid, id := range names {
		if vector := r.vectors.Get(id); vector != nil {
			out[docid] = vector
		}
	}
	return out
}

// OpenVectors 는 저장소의 벡터 파일을 읽는다. 없거나 깨졌으면 nil 이다 —
// 오류가 아니라 「의미 갈래 없음」이다 (결정 16).
func OpenVectors(repoDir string) *Vectors {
	path := VectorPath(repoDir)
	if path == "" {
		return nil
	}
	loaded, err := LoadVectors(path)
	if err != nil {
		return nil
	}
	return loaded
}
