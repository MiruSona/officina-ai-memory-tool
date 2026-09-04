package i18n

// 임베딩 설치·상태가 쓰는 문장이다. 다른 표는 안 건드리고 여기서 덧붙인다
// (설계 12-1).

// install 의 임베딩 소단계.
const (
	EmbedStepRuntime Key = "embed-step-runtime"
	EmbedStepModel   Key = "embed-step-model"
	EmbedStateOK     Key = "embed-state-ok"
	EmbedStateNone   Key = "embed-state-none"
	EmbedStateBroken Key = "embed-state-broken"
	EmbedTodoKeep    Key = "embed-todo-keep"
	EmbedTodoBundle  Key = "embed-todo-bundle"
	EmbedTodoFetch   Key = "embed-todo-fetch"
	EmbedTodoSkip    Key = "embed-todo-skip"
	EmbedTodoFailed  Key = "embed-todo-failed"
)

// 안내와 경고.
const (
	EmbedOff          Key = "embed-off"
	EmbedFailed       Key = "embed-failed"
	EmbedNoSource     Key = "embed-no-source"
	EmbedBundleMiss   Key = "embed-bundle-miss"
	EmbedSHABad       Key = "embed-sha-bad"
	EmbedModelWiped   Key = "embed-model-wiped"
	EmbedDone         Key = "embed-done"
	EmbedSizeNote     Key = "embed-size-note"
	EmbedNeedsInstall Key = "embed-needs-install"
)

// status --embed.
const (
	EmbedStatusHead    Key = "embed-status-head"
	EmbedStatusModel   Key = "embed-status-model"
	EmbedStatusRuntime Key = "embed-status-runtime"
	EmbedStatusVectors Key = "embed-status-vectors"
	EmbedStatusNoVec   Key = "embed-status-no-vec"
	EmbedStatusMode    Key = "embed-status-mode"
	EmbedStatusStale   Key = "embed-status-stale"
)

// index 뒤 벡터 만들기.
const (
	EmbedIndexMade    Key = "embed-index-made"
	EmbedIndexSkipped Key = "embed-index-skipped"
	// EmbedIndexFailed 는 토크나이저가 죽어 벡터 없이 지나간 건수다 (H-1).
	EmbedIndexFailed  Key = "embed-index-failed"
	EmbedIndexRebuilt Key = "embed-index-rebuilt"
)

var embedMessages = map[Key]string{
	EmbedStepRuntime: "의미 검색 런타임",
	EmbedStepModel:   "의미 검색 모델",
	EmbedStateOK:     "갖춰짐",
	EmbedStateNone:   "없음",
	EmbedStateBroken: "깨짐(해시 안 맞음)",
	EmbedTodoKeep:    "그대로 둠",
	EmbedTodoBundle:  "꾸러미에서 복사",
	EmbedTodoFetch:   "내려받기",
	EmbedTodoSkip:    "건너뜀 (--no-embed)",
	EmbedTodoFailed:  "**못 놓음**",

	EmbedOff: "의미 검색을 빼고 설치했다. 낱말 검색만 돈다.",
	EmbedFailed: "의미 검색 파일을 못 갖췄다 : %s\n" +
		"  낱말 검색만으로 설치를 끝냈다. 나중에 `mem install --apply` 를 다시 하면 된다.",
	EmbedNoSource:     "내려받을 자리가 아직 없다. `mem install --bundle <폴더> --apply` 로 놓아라.",
	EmbedBundleMiss:   "꾸러미에 %s 가 없다.",
	EmbedSHABad:       "%s 의 해시가 안 맞는다. 지웠다.",
	EmbedModelWiped:   "반쪽짜리 모델은 못 쓴다. 모델 폴더를 통째로 지웠다 : %s",
	EmbedDone:         "의미 검색 준비됨 — 모델 %s.",
	EmbedSizeNote:     "받을 것 : 모델 약 113MB · 토크나이저 약 16MB · 런타임 약 15MB (합 약 145MB).",
	EmbedNeedsInstall: "**의미 검색 꺼짐 — `mem install --apply` 를 다시 해라.**",

	EmbedStatusHead:    "## 의미 검색",
	EmbedStatusModel:   "  모델      %s (%s)",
	EmbedStatusRuntime: "  런타임    %s",
	EmbedStatusVectors: "  벡터      %d건 · %s · 모델 %s@%s",
	EmbedStatusNoVec:   "  벡터      없음 — `mem index` 를 돌리면 만든다",
	EmbedStatusMode:    "  지금 모드 %s",
	EmbedStatusStale:   "  **벡터가 지금 모델과 다르다. `mem index --full` 로 다시 만들어라.**",

	EmbedIndexMade:    "벡터 %d건 새로 · %d건 그대로 · %d건 버림 (%s · %.1f초)",
	EmbedIndexSkipped: "의미 검색 모델이 없어 벡터를 안 만들었다. 낱말 검색만 돈다.",
	EmbedIndexFailed:  "그중 %d건은 글이 이상해 벡터를 못 만들었다. 그 기억은 낱말 검색으로만 잡힌다.",
	EmbedIndexRebuilt: "모델이 바뀌어 벡터를 통째로 다시 만들었다.",
}

func init() {
	for key, text := range embedMessages {
		messages[key] = text
	}
}
